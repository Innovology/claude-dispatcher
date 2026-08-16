package dispatch

import (
	"testing"

	"claude-dispatcher/internal/state"
)

// withFakeSessions swaps the supervisor seams so the sweep runs against a
// hand-written liveness map instead of a real tmux server.
func withFakeSessions(t *testing.T, alive map[string]bool, ready bool) {
	t.Helper()
	prevAlive, prevReady := sessionAlive, supervisorReady
	t.Cleanup(func() { sessionAlive, supervisorReady = prevAlive, prevReady })
	sessionAlive = func(name string) bool { return alive[name] }
	supervisorReady = func() bool { return ready }
}

func saveAll(t *testing.T, recs ...*state.Dispatch) {
	t.Helper()
	for _, r := range recs {
		if err := state.Save(r); err != nil {
			t.Fatalf("state.Save(%s): %v", r.ID, err)
		}
	}
}

// statusOnDisk re-reads a record so the test asserts what was persisted rather
// than what was mutated in memory.
func statusOnDisk(t *testing.T, id string) state.Status {
	t.Helper()
	for _, d := range state.LoadAll() {
		if d.ID == id {
			return d.Status
		}
	}
	t.Fatalf("record %s vanished", id)
	return ""
}

// A record only reaches working, needs-input or blocked through a hook fired
// from inside its session, so the session provably existed — and its absence
// now is proof it ended, however it ended. Everything else is left alone.
func TestReconcileSessionsRetiresOnlyProvableStrays(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())

	rec := func(id string, st state.Status, session string) *state.Dispatch {
		return &state.Dispatch{ID: id, Feature: id, Status: st, TmuxSession: session}
	}
	live := rec("live", state.StatusWorking, "disp-live")
	gone := rec("gone", state.StatusWorking, "disp-gone")
	needs := rec("needs", state.StatusNeedsInput, "disp-needs")
	blocked := rec("blocked", state.StatusBlocked, "disp-blocked")
	launching := rec("launching", state.StatusLaunching, "disp-launching")
	done := rec("done", state.StatusDone, "disp-done")
	exited := rec("exited", state.StatusExited, "disp-exited")
	unnamed := rec("unnamed", state.StatusWorking, "")
	saveAll(t, live, gone, needs, blocked, launching, done, exited, unnamed)

	withFakeSessions(t, map[string]bool{"disp-live": true}, true)

	if n := ReconcileSessions(state.LoadAll()); n != 3 {
		t.Errorf("ReconcileSessions retired %d records, want 3 (gone, needs, blocked)", n)
	}

	for _, id := range []string{"gone", "needs", "blocked"} {
		if got := statusOnDisk(t, id); got != state.StatusExited {
			t.Errorf("%s: status = %q, want exited — its session is gone", id, got)
		}
	}
	if got := statusOnDisk(t, "live"); got != state.StatusWorking {
		t.Errorf("live: status = %q, want working — its session is still there", got)
	}
	// Nothing has fired a hook for a launching record, so there is a window in
	// which its session does not exist yet and never did. Absence proves
	// nothing there, and the sweep must not claim otherwise.
	if got := statusOnDisk(t, "launching"); got != state.StatusLaunching {
		t.Errorf("launching: status = %q, want launching — the launch window is not evidence", got)
	}
	if got := statusOnDisk(t, "done"); got != state.StatusDone {
		t.Errorf("done: status = %q, want done — done means live", got)
	}
	if got := statusOnDisk(t, "exited"); got != state.StatusExited {
		t.Errorf("exited: status = %q, want exited", got)
	}
	if got := statusOnDisk(t, "unnamed"); got != state.StatusWorking {
		t.Errorf("unnamed: status = %q, want working — there is no session to look for", got)
	}
}

// The reason has to say what was actually observed, since it is what the
// cockpit shows in place of the status the hook never got to write.
func TestReconcileSessionsExplainsItself(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	saveAll(t, &state.Dispatch{
		ID: "stray", Feature: "stray", Status: state.StatusWorking,
		TmuxSession: "disp-stray", StatusReason: "processing your prompt",
	})
	withFakeSessions(t, nil, true)

	ReconcileSessions(state.LoadAll())
	for _, d := range state.LoadAll() {
		if d.ID != "stray" {
			continue
		}
		if d.StatusReason == "processing your prompt" {
			t.Error("a retired record kept the reason from the status it no longer has")
		}
		if d.StatusReason == "" {
			t.Error("a retired record must say why")
		}
	}
}

// A supervisor we cannot reach is not evidence that every session is gone. If
// it were swept anyway, one missing tmux would retire the whole fleet at once.
func TestReconcileSessionsSweepsNothingWithoutASupervisor(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	saveAll(t, &state.Dispatch{
		ID: "orphan", Feature: "orphan", Status: state.StatusWorking,
		TmuxSession: "disp-orphan",
	})
	withFakeSessions(t, nil, false)

	if n := ReconcileSessions(state.LoadAll()); n != 0 {
		t.Errorf("ReconcileSessions retired %d records with no supervisor, want 0", n)
	}
	if got := statusOnDisk(t, "orphan"); got != state.StatusWorking {
		t.Errorf("orphan: status = %q, want working — nothing observed it", got)
	}
}

// The sweep must be idempotent: a second pass over records it already retired
// changes nothing and reports nothing, so a cockpit polling it does not churn
// the state dir (every save wakes the fsnotify watcher).
func TestReconcileSessionsIsIdempotent(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	saveAll(t, &state.Dispatch{
		ID: "twice", Feature: "twice", Status: state.StatusBlocked,
		TmuxSession: "disp-twice",
	})
	withFakeSessions(t, nil, true)

	if n := ReconcileSessions(state.LoadAll()); n != 1 {
		t.Fatalf("first pass retired %d, want 1", n)
	}
	if n := ReconcileSessions(state.LoadAll()); n != 0 {
		t.Errorf("second pass retired %d, want 0", n)
	}
}
