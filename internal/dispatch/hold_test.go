package dispatch

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// withConversations swaps the question hold.go asks the machine: which
// conversation each session's claude is running, by session name.
func withConversations(t *testing.T, running map[string]string) *int {
	t.Helper()
	asked := 0
	prev := sessionRunsConversation
	t.Cleanup(func() { sessionRunsConversation = prev })
	sessionRunsConversation = func(sess supervisor.Session, sid string) bool {
		asked++
		return sid != "" && running[sess.Name] == sid
	}
	return &asked
}

// withListing stands a server up for the sweep: each session by name, with when
// it was made, and every name the listing gives answering the probe.
func withListing(t *testing.T, born map[string]time.Time) {
	t.Helper()
	prevAlive, prevList, prevReady := sessionAlive, sessionList, supervisorReady
	t.Cleanup(func() { sessionAlive, sessionList, supervisorReady = prevAlive, prevList, prevReady })
	sessionList = func(socket string) []supervisor.SessionInfo {
		var out []supervisor.SessionInfo
		for name, at := range born {
			out = append(out, supervisor.SessionInfo{Session: supervisor.Session{Name: name, Socket: socket}, Created: at})
		}
		return out
	}
	sessionAlive = func(sess supervisor.Session) bool { _, ok := born[sess.Name]; return ok }
	supervisorReady = func() bool { return true }
}

// The incident (7b388dcc30f3): the record held 14:32:21, the session at its
// address was made at 14:35:00 by its own relaunch, and a stale copy had put the
// old hold back. By the hold, the session was a stranger's, and the sweep
// retired a dispatcher that was working. Its own conversation is running there,
// so it is its own session: the sweep keeps it and moves the hold to the
// session's birth, so the next load needs no asking.
func TestASweepKeepsAYoungerSessionRunningItsOwnConversation(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	held := time.Date(2026, 11, 26, 14, 32, 21, 411916704, time.Local)
	born := time.Date(2026, 11, 26, 14, 35, 0, 0, time.Local)
	saveAll(t,
		&state.Dispatch{ID: "ds", Feature: "DS adoption", Status: state.StatusWorking,
			TmuxSession: "disp-ds", TmuxSocket: "player-app", SessionID: "sess-ds", SessionStartedAt: &held},
		&state.Dispatch{ID: "other", Feature: "other", Status: state.StatusWorking,
			TmuxSession: "disp-other", TmuxSocket: "player-app", SessionID: "sess-other", SessionStartedAt: &held},
	)
	withListing(t, map[string]time.Time{"disp-ds": born, "disp-other": born})
	// disp-other is running somebody else's conversation under the old name.
	asked := withConversations(t, map[string]string{"disp-ds": "sess-ds", "disp-other": "sess-someone-else"})

	ds := state.LoadAll()
	retired, live := ReconcileSessions(ds)
	if retired != 1 || live != 1 {
		t.Errorf("ReconcileSessions = retired:%d live:%d, want its own kept and the stranger's retired", retired, live)
	}
	got := onDisk(t, "ds")
	if got.Status != state.StatusWorking {
		t.Errorf("ds: status = %s (%q), want it still working", got.Status, got.StatusReason)
	}
	if got.SessionStartedAt == nil || !got.SessionStartedAt.Equal(born) {
		t.Errorf("ds: hold = %v, want it moved to the session's birth %v", got.SessionStartedAt, born)
	}
	for _, d := range ds {
		if d.ID == "ds" && (d.SessionStartedAt == nil || !d.SessionStartedAt.Equal(born)) {
			t.Error("the load's own copy still holds the old session; the collectors would read it")
		}
	}
	if other := onDisk(t, "other"); other.Status != state.StatusExited || !strings.Contains(other.StatusReason, "newer session") {
		t.Errorf("other: %s %q, want it retired — a session running another conversation is not its own", other.Status, other.StatusReason)
	}

	// The next load finds the hold right and asks nothing.
	*asked = 0
	if _, live := ReconcileSessions(state.LoadAll()); live != 1 || *asked != 0 {
		t.Errorf("second sweep: live %d, asked the machine %d times, want 1 and 0", live, *asked)
	}
}

// What the sweep cannot ask about, it still retires: a record whose
// conversation is not known has nothing to find, and the machine is not asked.
func TestASweepRetiresAYoungerSessionWhenThereIsNoConversationToFind(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	held := time.Date(2026, 11, 26, 14, 32, 21, 0, time.Local)
	saveAll(t, &state.Dispatch{ID: "r", Feature: "r", Status: state.StatusNeedsInput,
		TmuxSession: "player-app__main", TmuxSocket: "player-app", SessionStartedAt: &held})
	withListing(t, map[string]time.Time{"player-app__main": held.Add(48 * time.Hour)})
	asked := withConversations(t, map[string]string{"player-app__main": "sess-theirs"})

	if retired, _ := ReconcileSessions(state.LoadAll()); retired != 1 {
		t.Errorf("retired %d, want the record retired", retired)
	}
	if *asked != 0 {
		t.Errorf("asked the machine %d times about a record with no conversation", *asked)
	}
}

// A sweep decides on the load's copy, and a load is slow. When the record has
// moved on before the sweep writes — a resume gave it a new session, a hook a
// new status — the verdict was about a record that no longer exists, and
// writing it would put the copy's old session back over the new one. It is
// left for the next load to judge.
func TestASweepDoesNotRetireARecordThatMovedOnSinceTheLoad(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	held := time.Date(2026, 11, 26, 14, 32, 21, 0, time.Local)
	saveAll(t, &state.Dispatch{ID: "r", Feature: "r", Status: state.StatusWorking,
		TmuxSession: "disp-r", TmuxSocket: "s", SessionID: "sess-r", SessionStartedAt: &held})
	ds := state.LoadAll() // the load's copy

	// The resume, landing after the load read the records.
	resumed := held.Add(3 * time.Minute)
	now := *ds[0]
	now.Status, now.StatusReason, now.TmuxSession, now.SessionStartedAt = state.StatusLaunching, "resuming its session", "disp-r-2", &resumed
	saveAll(t, &now)

	withListing(t, map[string]time.Time{}) // disp-r is gone, disp-r-2 not yet listed
	withConversations(t, nil)
	if retired, _ := ReconcileSessions(ds); retired != 0 {
		t.Errorf("retired %d, want 0 — the record the verdict was about has been resumed", retired)
	}
	got := onDisk(t, "r")
	if got.Status != state.StatusLaunching || got.TmuxSession != "disp-r-2" || !got.SessionStartedAt.Equal(resumed) {
		t.Errorf("on disk: %s %q hold %v — the load's copy was written over the resume", got.Status, got.TmuxSession, got.SessionStartedAt)
	}
	if ds[0].TmuxSession != "disp-r-2" {
		t.Errorf("the load's copy names %q, want the record as it now is", ds[0].TmuxSession)
	}
}
