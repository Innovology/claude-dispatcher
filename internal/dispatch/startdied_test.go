//go:build !windows

package dispatch

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// incidentScreen is the pane of the player-app handoff on 2026-11-26, as
// claude 2.1.332 and the exit shell left it: claude's one line and its exit,
// then nu, standing in a directory that had been deleted, complaining at its
// prompt.
const incidentScreen = "The current directory no longer exists (it was deleted or moved). Start Claude Code from an existing directory.\n" +
	"\n" + ExitMark + "1\n" +
	"Error: nu::shell::directory_not_found\n" +
	"  × Directory not found\n" +
	"~/player-app/adopt-ds-3-21-2> \n\n\n"

// withPanes stands in for what the supervisor says about a session's panes:
// whether they are idle, and what they show.
func withPanes(t *testing.T, idle, known bool, screen string, err error) {
	t.Helper()
	prevIdle, prevCapture := sessionIdle, capturePane
	t.Cleanup(func() { sessionIdle, capturePane = prevIdle, prevCapture })
	sessionIdle = func(supervisor.Session) (bool, bool) { return idle, known }
	capturePane = func(supervisor.Session) (string, error) { return screen, err }
}

// The report: a handed-off dispatch's claude refused to start, the launch
// line's exit shell took the pane, and the session lived on with nothing in it
// but a shell — so the sweep, which retired a launch only when its session had
// gone, left the row at "starting session" for good. A launch whose pane has
// fallen to its shell is a start that failed, and it says so in claude's words.
func TestALaunchWhoseClaudeDiedIsRetiredWithItsWords(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	started := time.Now().Add(-10 * time.Second)
	saveAll(t, &state.Dispatch{ID: "dead", Feature: "adopt DS 3.21.2 on main", Status: state.StatusLaunching,
		TmuxSession: "disp-adopt-ds-3-21-2", TmuxSocket: "player-app", SessionStartedAt: &started})
	withFakeSessions(t, map[string]bool{"disp-adopt-ds-3-21-2": true}, true)
	withPanes(t, true, true, incidentScreen, nil)

	recs := state.LoadAll()
	retired, live := ReconcileSessions(recs)
	if retired != 1 || live != 0 {
		t.Errorf("ReconcileSessions = (%d retired, %d live), want (1, 0)", retired, live)
	}
	d := recordOnDisk(t, "dead")
	if d.Status != state.StatusExited {
		t.Fatalf("status = %q, want exited — its claude is gone and never reported in", d.Status)
	}
	want := "claude exited before it reported in (status 1): The current directory no longer exists (it was deleted or moved)."
	if !strings.HasPrefix(d.StatusReason, want) {
		t.Errorf("reason = %q\nwant it to start %q", d.StatusReason, want)
	}
	if strings.Contains(d.StatusReason, "nu::shell") {
		t.Errorf("the exit shell's complaint was read as claude's: %q", d.StatusReason)
	}
	// Nobody watched it end, so it holds its row (ADR 0012), where ⏎ starts it
	// again and l opens the whole screen.
	if !d.Held() {
		t.Error("the retired launch is not held on the table")
	}
	if !NeverStarted(d) {
		t.Error("a launch that never reported in reads as a conversation to resume")
	}
	kept, err := os.ReadFile(d.StartOutput)
	if err != nil {
		t.Fatalf("the screen was not kept where l opens it: %v", err)
	}
	if !strings.HasPrefix(string(kept), "error: "+d.StatusReason) || !strings.Contains(string(kept), "nu::shell::directory_not_found") {
		t.Errorf("the kept output is not the reason over the whole screen:\n%s", kept)
	}
	// The caller's slice is what the cockpit goes on to draw from.
	if recs[0].Status != state.StatusExited {
		t.Error("the record passed in still says launching")
	}
}

// Only an idle shell is evidence. A busy pane is claude starting, or stopped on
// a startup prompt (an eleven-minute one, the same afternoon); an unknown one is
// unknown; a pane read in the moment before the launch line has forked anything
// is a shell with nothing under it and proves nothing; and a record a hook has
// moved on is not a launch at all.
func TestOnlyAnIdleShellAfterTheSettleRetiresALaunch(t *testing.T) {
	long, just := time.Now().Add(-time.Minute), time.Now()
	for _, tc := range []struct {
		name        string
		status      state.Status
		started     *time.Time
		idle, known bool
	}{
		{"busy: starting or on a startup prompt", state.StatusLaunching, &long, false, true},
		{"unknown", state.StatusLaunching, &long, false, false},
		{"idle before the settle", state.StatusLaunching, &just, true, true},
		{"never accepted by the supervisor", state.StatusLaunching, nil, true, true},
		{"reported in, then ended", state.StatusWorking, &long, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
			saveAll(t, &state.Dispatch{ID: "r", Feature: "r", Status: tc.status,
				TmuxSession: "disp-r", SessionStartedAt: tc.started})
			withFakeSessions(t, map[string]bool{"disp-r": true}, true)
			withPanes(t, tc.idle, tc.known, incidentScreen, nil)

			if retired, live := ReconcileSessions(state.LoadAll()); retired != 0 || live != 1 {
				t.Errorf("ReconcileSessions = (%d retired, %d live), want (0, 1)", retired, live)
			}
			if got := statusOnDisk(t, "r"); got != tc.status {
				t.Errorf("status = %q, want %q", got, tc.status)
			}
		})
	}
}

// The judgement is made off the record as the load read it, and the pane is
// read before the write. A SessionStart landing in between is a claude that
// started after all; the retirement writes against a fresh read under the hook
// lock, and leaves it working.
func TestASessionStartBetweenTheLookAndTheWriteWins(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	started := time.Now().Add(-time.Minute)
	saveAll(t, &state.Dispatch{ID: "late", Feature: "late", Status: state.StatusLaunching,
		TmuxSession: "disp-late", SessionStartedAt: &started})
	withFakeSessions(t, map[string]bool{"disp-late": true}, true)
	withPanes(t, true, true, "", nil)
	capturePane = func(supervisor.Session) (string, error) {
		d := recordOnDisk(t, "late")
		d.Status, d.SessionID = state.StatusWorking, "sid"
		saveAll(t, d)
		return incidentScreen, nil
	}

	if retired, live := ReconcileSessions(state.LoadAll()); retired != 0 || live != 1 {
		t.Errorf("ReconcileSessions = (%d retired, %d live), want (0, 1)", retired, live)
	}
	if d := recordOnDisk(t, "late"); d.Status != state.StatusWorking || d.SessionID != "sid" {
		t.Errorf("after the sweep: status %q, session %q — the hook's write was lost", d.Status, d.SessionID)
	}
}

// What the row says is read the way any failed command's output is, from what
// claude printed: the borders of a box it drew are not words, a pane with no
// mark is quoted as the pane's rather than as claude's, and a pane that cannot
// be read says that rather than nothing.
func TestTheReasonIsWhatClaudeSaid(t *testing.T) {
	boxed := "╭──────────────────╮\n│ Invalid API key · Please run /login │\n╰──────────────────╯\n" + ExitMark + "1\n$ "
	if got := diedReason(boxed, nil); got != diedLead+" (status 1): │ Invalid API key · Please run /login │" {
		t.Errorf("boxed: %q", got)
	}
	if got := diedReason("something it said\n$ ", nil); got != diedLead+" — its pane last read: something it said" {
		t.Errorf("no mark: %q", got)
	}
	if got := diedReason("", errors.New("can't find pane: =disp-x:")); !strings.Contains(got, "could not be read: can't find pane") {
		t.Errorf("unreadable: %q", got)
	}
	if got := diedReason("\n"+ExitMark+"0\n", nil); got != diedLead+" (status 0) and printed nothing" {
		t.Errorf("silent: %q", got)
	}
}
