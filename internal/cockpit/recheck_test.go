package cockpit

// recheck_test.go covers the jump-in round trip: coming back from a session has
// to re-establish what is true rather than redraw what was true before the
// human went in.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// A handover that exits on the way out (switch-client inside tmux, a raised
// console window on Windows) has not brought anyone back yet, so the recheck
// waits for the focus the cockpit's pane regains on the return trip. A handover
// that exits on the way home is itself the return.
func TestAttachRoundTripWaitsForTheActualReturn(t *testing.T) {
	base := newModel()
	base.cfg = &config.Config{}

	// Away: the command exited as the human left. Still marked away, so the
	// focus event is still owed a recheck.
	away := base
	away.away = true
	got, cmd := away.Update(attachReturnedMsg{})
	if !got.(model).away {
		t.Error("a handover that exits on the way out must leave the cockpit marked away")
	}
	if cmd == nil {
		t.Error("the cockpit should still reload when the handover command exits")
	}

	// Not away: this message IS the return, so it is not deferred to focus.
	got, cmd = base.Update(attachReturnedMsg{})
	if got.(model).away {
		t.Error("a blocking attach returns the human with it — nothing to wait for")
	}
	if cmd == nil {
		t.Error("returning from an attach should recheck")
	}

	// A handover that never happened sent nobody anywhere.
	got, _ = away.Update(attachReturnedMsg{err: errFake{}})
	if got.(model).away {
		t.Error("a failed attach must not leave the cockpit waiting for a return")
	}
}

// Focus is not on its own a reason to re-read the world: a full forge refetch
// every time the human alt-tabs back to the terminal is how the gh quota gets
// burned, and a cockpit that never left has nothing to catch up on.
func TestFocusOnlyRechecksAfterAJumpIn(t *testing.T) {
	base := newModel()
	base.cfg = &config.Config{}

	if _, cmd := base.Update(tea.FocusMsg{}); cmd != nil {
		t.Error("focus without a jump-in should cost nothing")
	}

	away := base
	away.away = true
	got, cmd := away.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Error("coming back from a jump-in should recheck")
	}
	if got.(model).away {
		t.Error("the return trip is owed one recheck, not one per focus")
	}
	// And the debt is settled: a second focus is an ordinary one again.
	if _, cmd := got.(model).Update(tea.FocusMsg{}); cmd != nil {
		t.Error("the recheck should not repeat on every later focus")
	}
}

// On demo data there is nothing to recheck against, and track.Refresh would be
// handed a nil config.
func TestFocusWithoutConfigRechecksNothing(t *testing.T) {
	m := newModel()
	m.away = true
	got, cmd := m.Update(tea.FocusMsg{})
	if cmd != nil {
		t.Error("a config-less cockpit has no live data to recheck")
	}
	if got.(model).away {
		t.Error("away should still be cleared — the human is back either way")
	}
}

// The end-to-end claim: after a jump-in, a record whose session went away while
// the cockpit was not looking comes back as exited rather than as the status
// the last hook managed to write. The whole environment is real here — repos,
// records and all — so this exercises recheckCmd's actual sequence.
func TestRecheckRetiresSessionsThatWentAway(t *testing.T) {
	if !supervisor.Available() {
		t.Skip("no session supervisor on this host — the liveness sweep cannot observe anything")
	}
	env := buildEnvScenario(t)
	saved := captureVars()
	defer restoreVars(saved)

	// The fixture's blocked and needs-input records name tmux sessions that do
	// not exist — a session killed, or a tmux server that went down, while the
	// human was inside another one.
	before := map[string]state.Status{}
	for _, d := range state.LoadAll() {
		before[d.ID] = d.Status
	}
	if before["rec-blocked"] != state.StatusBlocked || before["rec-needs"] != state.StatusNeedsInput {
		t.Fatalf("fixture changed: blocked=%q needs=%q", before["rec-blocked"], before["rec-needs"])
	}

	msg := recheckCmd(env.cfg)()
	if _, ok := msg.(snapshotMsg); !ok {
		t.Fatalf("recheckCmd returned %T, want a snapshot", msg)
	}

	after := map[string]state.Status{}
	for _, d := range state.LoadAll() {
		after[d.ID] = d.Status
	}
	for _, id := range []string{"rec-blocked", "rec-needs"} {
		if after[id] != state.StatusExited {
			t.Errorf("%s: status = %q after the recheck, want exited — its session is gone", id, after[id])
		}
	}
	// rec-working names no session at all, so there is nothing to look for and
	// nothing to conclude.
	if after["rec-working"] != state.StatusWorking {
		t.Errorf("rec-working: status = %q, want working — it names no session to check", after["rec-working"])
	}
	if after["rec-done"] != state.StatusDone {
		t.Errorf("rec-done: status = %q, want done", after["rec-done"])
	}
}
