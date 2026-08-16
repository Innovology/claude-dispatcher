//go:build !windows

package supervisor

import (
	"strings"
	"testing"
)

// These exercise the Unix surface. They are best-effort like the code itself:
// tmux may be absent on the runner, so calls must degrade rather than fail.

func TestBackendName(t *testing.T) {
	if Backend() != "tmux" {
		t.Errorf("Backend() = %q, want tmux", Backend())
	}
	// Available() is whatever the host reports; just ensure it does not panic.
	_ = Available()
}

func TestUniqueNameFallsBackWhenFree(t *testing.T) {
	// A session name this specific will not exist, so UniqueName returns it as-is.
	name := "disp-supervisor-test-" + strings.Repeat("z", 8)
	if got := UniqueName(name); got != name {
		t.Errorf("UniqueName(free) = %q, want %q", got, name)
	}
}

func TestNonMutatingCallsAreSafe(t *testing.T) {
	// None of these should panic whether or not tmux is installed.
	if HasSession("definitely-not-a-real-session-xyz") {
		t.Error("HasSession reported a bogus session as alive")
	}
	SetStatusHint("definitely-not-a-real-session-xyz")
	EnsureBackKey()
	EnsureFocusEvents()
	if cmd := AttachCmd("x"); cmd == nil {
		t.Error("AttachCmd returned nil")
	}
	// SendKeys/KillSession target a non-existent session; they may error, which
	// is fine — we only require they return without panicking.
	_ = SendKeys("definitely-not-a-real-session-xyz", "hello")
	_ = KillSession("definitely-not-a-real-session-xyz")
}

// AttachSwitches answers one question — does AttachCmd's exit mean the human is
// back, or that they have just left — and the cockpit's return-trip recheck
// hangs off it. It must agree with the command AttachCmd actually builds, or
// the cockpit waits for a return that already happened (or rechecks one that
// has not).
func TestAttachSwitchesMatchesTheCommandBuilt(t *testing.T) {
	for _, tc := range []struct {
		name, tmux, wantArg string
		wantSwitches        bool
	}{
		{name: "inside tmux", tmux: "/tmp/tmux-501/default,1,0", wantArg: "switch-client", wantSwitches: true},
		{name: "bare terminal", tmux: "", wantArg: "attach-session", wantSwitches: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMUX", tc.tmux)
			if got := AttachSwitches(); got != tc.wantSwitches {
				t.Errorf("AttachSwitches() = %v, want %v", got, tc.wantSwitches)
			}
			args := AttachCmd("some-session").Args
			if len(args) < 2 || args[1] != tc.wantArg {
				t.Errorf("AttachCmd args = %v, want %q — it disagrees with AttachSwitches", args, tc.wantArg)
			}
		})
	}
}
