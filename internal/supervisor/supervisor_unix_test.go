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
	if cmd := AttachCmd("x"); cmd == nil {
		t.Error("AttachCmd returned nil")
	}
	// SendKeys/KillSession target a non-existent session; they may error, which
	// is fine — we only require they return without panicking.
	_ = SendKeys("definitely-not-a-real-session-xyz", "hello")
	_ = KillSession("definitely-not-a-real-session-xyz")
}
