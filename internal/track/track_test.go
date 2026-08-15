package track

import (
	"testing"

	"claude-dispatcher/internal/state"
)

// The regression this guards: a dispatcher that merges its own PR mid-run was
// flipped to done while its session was still going, and hookcmd would not let
// anything the session said afterwards downgrade that — so a live session
// stuck at a permission prompt disappeared from the cockpit, which then showed
// the empty-fleet dispatch form.
func TestMidWork(t *testing.T) {
	cases := []struct {
		status state.Status
		want   bool
	}{
		{state.StatusWorking, true},
		{state.StatusBlocked, true},
		{state.StatusLaunching, true},
		{state.StatusNeedsInput, false}, // turn over — "done means live" applies
		{state.StatusExited, false},
		{state.StatusDone, false},
	}
	for _, c := range cases {
		if got := midWork(&state.Dispatch{Status: c.status}); got != c.want {
			t.Errorf("midWork(%s) = %v, want %v", c.status, got, c.want)
		}
	}
}
