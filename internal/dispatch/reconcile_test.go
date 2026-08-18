package dispatch

import (
	"testing"

	"claude-dispatcher/internal/state"
)

// withFakeSessions swaps the supervisor seams so the sweep runs against a
// hand-written liveness map instead of a real tmux server. The listing and the
// per-name probe answer from the same map, which is what a working supervisor
// does; withSessionSeams is how a test pulls the two apart.
func withFakeSessions(t *testing.T, alive map[string]bool, ready bool) {
	t.Helper()
	names := make([]string, 0, len(alive))
	for name, ok := range alive {
		if ok {
			names = append(names, name)
		}
	}
	withSessionSeams(t, func() []string { return names }, func(name string) bool { return alive[name] }, ready)
}

// withSessionSeams is withFakeSessions with the listing and the probe given
// separately, for the cases where they do not agree.
func withSessionSeams(t *testing.T, list func() []string, probe func(string) bool, ready bool) {
	t.Helper()
	prevAlive, prevNames, prevReady := sessionAlive, sessionNames, supervisorReady
	t.Cleanup(func() { sessionAlive, sessionNames, supervisorReady = prevAlive, prevNames, prevReady })
	sessionNames = list
	sessionAlive = probe
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

	retired, stillRunning := ReconcileSessions(state.LoadAll())
	if retired != 3 {
		t.Errorf("ReconcileSessions retired %d records, want 3 (gone, needs, blocked)", retired)
	}
	// The second figure is the one the opening screen prints, and it counts
	// records with a session rather than statuses: only "live" still has one.
	if stillRunning != 1 {
		t.Errorf("ReconcileSessions counted %d live sessions, want 1", stillRunning)
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

	if n, _ := ReconcileSessions(state.LoadAll()); n != 0 {
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

	if n, _ := ReconcileSessions(state.LoadAll()); n != 1 {
		t.Fatalf("first pass retired %d, want 1", n)
	}
	if n, _ := ReconcileSessions(state.LoadAll()); n != 0 {
		t.Errorf("second pass retired %d, want 0", n)
	}
}

// The listing screens; it never convicts on its own. A listing that came back
// empty because tmux failed — rather than because nothing is running — would
// otherwise retire every working dispatcher on the machine in one pass, which
// is the same mass-retirement an unreachable supervisor is guarded against
// above. The direct probe is what has to agree before anything is written.
func TestReconcileSessionsConfirmsAnEmptyListingBeforeRetiring(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	saveAll(t, &state.Dispatch{
		ID: "busy", Feature: "busy", Status: state.StatusWorking,
		TmuxSession: "disp-busy",
	})
	probes := 0
	withSessionSeams(t,
		func() []string { return nil }, // the listing failed and said nothing
		func(name string) bool { probes++; return name == "disp-busy" },
		true)

	retired, live := ReconcileSessions(state.LoadAll())
	if retired != 0 {
		t.Errorf("retired %d records the listing missed, want 0 — the probe says it is there", retired)
	}
	if live != 1 {
		t.Errorf("counted %d live, want 1 — the probe found the session the listing did not list", live)
	}
	if probes != 1 {
		t.Errorf("probed %d times, want 1 — only a record about to be retired is worth a subprocess", probes)
	}
	if got := statusOnDisk(t, "busy"); got != state.StatusWorking {
		t.Errorf("busy: status = %q, want working", got)
	}
}

// A record the listing already accounts for is never probed. The sweep runs on
// every cockpit load now, so a probe per record would be a subprocess per
// record on every poll and every state-file change.
func TestReconcileSessionsProbesOnlyWhatTheListingMissed(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	saveAll(t,
		&state.Dispatch{ID: "a", Feature: "a", Status: state.StatusWorking, TmuxSession: "disp-a"},
		&state.Dispatch{ID: "b", Feature: "b", Status: state.StatusNeedsInput, TmuxSession: "disp-b"},
		&state.Dispatch{ID: "old", Feature: "old", Status: state.StatusDone, TmuxSession: "disp-old"},
	)
	probes := 0
	withSessionSeams(t,
		func() []string { return []string{"disp-a", "disp-b"} },
		func(string) bool { probes++; return false },
		true)

	if _, live := ReconcileSessions(state.LoadAll()); live != 2 {
		t.Errorf("counted %d live, want 2", live)
	}
	if probes != 0 {
		t.Errorf("probed %d times, want 0 — the listing already answered for every record", probes)
	}
}
