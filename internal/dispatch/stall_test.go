package dispatch

import (
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

// A session the supervisor started and that has not reported in by the
// threshold is waiting on a startup prompt; one still inside it, one a hook has
// bound, and one the supervisor never accepted are not.
func TestLaunchStalled(t *testing.T) {
	now := time.Now()
	at := func(ago time.Duration) *time.Time { t := now.Add(-ago); return &t }
	cases := []struct {
		name string
		d    state.Dispatch
		want bool
	}{
		{"silent past the threshold", state.Dispatch{Status: state.StatusLaunching, SessionStartedAt: at(LaunchStallAfter + time.Second)}, true},
		{"still within it", state.Dispatch{Status: state.StatusLaunching, SessionStartedAt: at(5 * time.Second)}, false},
		{"a hook bound it", state.Dispatch{Status: state.StatusLaunching, SessionID: "s", SessionStartedAt: at(time.Minute)}, false},
		{"never accepted by the supervisor", state.Dispatch{Status: state.StatusLaunching}, false},
		{"working", state.Dispatch{Status: state.StatusWorking, SessionStartedAt: at(time.Minute)}, false},
	}
	for _, c := range cases {
		if got := LaunchStalled(&c.d, now); got != c.want {
			t.Errorf("%s: LaunchStalled = %v, want %v", c.name, got, c.want)
		}
	}
}

// A launch whose worktree could not be marked trusted says so on its record,
// so a stall can name the trust prompt rather than guess at it.
func TestLaunchReasonNamesAnUntrustedWorktree(t *testing.T) {
	if launchReason(true) != "" || launchReason(false) != state.ReasonUntrustedLaunch {
		t.Errorf("launchReason(true) = %q, launchReason(false) = %q", launchReason(true), launchReason(false))
	}
}
