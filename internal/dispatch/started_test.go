package dispatch

import (
	"os"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

func recordOnDisk(t *testing.T, id string) *state.Dispatch {
	t.Helper()
	for _, d := range state.LoadAll() {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("record %s vanished", id)
	return nil
}

// The report: a dispatch sat at "starting session" for good. tmux had accepted
// the session and it died a moment later — the pane's command could not run —
// and a launching record was never swept, because before any hook fires its
// session may simply not exist yet. The supervisor saying yes ends that window,
// so it goes on the record, and from then on a missing session is evidence.
func TestALaunchThatStartedAndDiedIsRetired(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	now := time.Now()
	started := &state.Dispatch{ID: "died", Feature: "died", Status: state.StatusLaunching,
		TmuxSession: "disp-died", SessionStartedAt: &now}
	waiting := &state.Dispatch{ID: "waiting", Feature: "waiting", Status: state.StatusLaunching,
		TmuxSession: "disp-waiting"}
	running := &state.Dispatch{ID: "running", Feature: "running", Status: state.StatusLaunching,
		TmuxSession: "disp-running", SessionStartedAt: &now}
	saveAll(t, started, waiting, running)
	withFakeSessions(t, map[string]bool{"disp-running": true}, true)

	retired, live := ReconcileSessions(state.LoadAll())
	if retired != 1 || live != 1 {
		t.Errorf("ReconcileSessions = (%d retired, %d live), want (1, 1)", retired, live)
	}
	d := recordOnDisk(t, "died")
	if d.Status != state.StatusExited {
		t.Errorf("died: status = %q, want exited — its session existed and is gone", d.Status)
	}
	if !strings.Contains(d.StatusReason, "before claude reported in") {
		t.Errorf("died: reason = %q, want one that says claude never reported in", d.StatusReason)
	}
	if got := statusOnDisk(t, "waiting"); got != state.StatusLaunching {
		t.Errorf("waiting: status = %q, want launching — never started is not evidence", got)
	}
	if got := statusOnDisk(t, "running"); got != state.StatusLaunching {
		t.Errorf("running: status = %q, want launching — its session is there", got)
	}
}

// Launch stamps the record once the supervisor accepts the session, and only
// then; a refused session leaves no stamp to be swept on.
func TestLaunchStampsTheSessionOnlyOnceItStarted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fail  bool
		stamp bool
	}{{"accepted", false, true}, {"refused", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
			repo := initRepo(t)
			prevNew, prevUniq, prevAlive := newSession, uniqueName, sessionAlive
			newSession = func(supervisor.Session, string, string, string) error {
				if tc.fail {
					return os.ErrPermission
				}
				return nil
			}
			uniqueName = func(s supervisor.Session) string { return s.Name }
			sessionAlive = func(supervisor.Session) bool { return false }
			t.Cleanup(func() { newSession, uniqueName, sessionAlive = prevNew, prevUniq, prevAlive })

			d, _ := Launch(repos.Repo{Name: "acme", Path: repo}, "stamp", "go", ModeAuto, DefaultModel, DefaultRoot, false)
			if d == nil {
				t.Fatal("Launch returned no record")
			}
			if got := recordOnDisk(t, d.ID).SessionStartedAt != nil; got != tc.stamp {
				t.Errorf("stamped on disk = %v, want %v", got, tc.stamp)
			}
		})
	}
}

// A hook that lands between new-session returning and the stamp being written
// must not be undone by it: the stamp joins the record on disk.
func TestTheStampKeepsWhatAHookWrote(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	mine := &state.Dispatch{ID: "raced", Feature: "raced", Status: state.StatusLaunching, TmuxSession: "disp-raced"}
	saveAll(t, mine)
	hooked := recordOnDisk(t, "raced")
	hooked.Status, hooked.SessionID = state.StatusWorking, "sid-1"
	saveAll(t, hooked)

	markSessionStarted(mine, time.Now())
	d := recordOnDisk(t, "raced")
	if d.Status != state.StatusWorking || d.SessionID != "sid-1" {
		t.Errorf("after the stamp: status %q, session %q — the hook's write was lost", d.Status, d.SessionID)
	}
	if d.SessionStartedAt == nil {
		t.Error("the stamp was not written")
	}
}
