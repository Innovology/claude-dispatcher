//go:build !windows

package supervisor

import (
	"slices"
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
	if got := UniqueName(Session{Name: name}); got != name {
		t.Errorf("UniqueName(free) = %q, want %q", got, name)
	}
}

// A session that does not exist is not an idle one. Resume reads "unknown" as
// "leave it alone", so answering idle here would have it kill and replace
// sessions it cannot see.
func TestSessionIdleIsUnknownForAMissingSession(t *testing.T) {
	idle, known := SessionIdle(Session{Name: "definitely-not-a-real-session-xyz"})
	if known {
		t.Error("a session that does not exist cannot be reported on")
	}
	if idle {
		t.Error("unknown must never read as idle")
	}
}

func TestNonMutatingCallsAreSafe(t *testing.T) {
	// None of these should panic whether or not tmux is installed.
	bogus := Session{Name: "definitely-not-a-real-session-xyz"}
	onSocket := Session{Name: bogus.Name, Socket: "definitely-not-a-real-socket-xyz"}
	for _, s := range []Session{bogus, onSocket} {
		if HasSession(s) {
			t.Errorf("HasSession reported a bogus session as alive (socket %q)", s.Socket)
		}
		SetStatusHint(s)
		if cmd := AttachCmd(s, ""); cmd == nil {
			t.Error("AttachCmd returned nil")
		}
		// SendKeys/KillSession target a non-existent session; they may error,
		// which is fine — we only require they return without panicking.
		_ = SendKeys(s, "hello")
		_ = KillSession(s)
	}
	// A socket with no server behind it lists nothing; it must not be an error
	// anyone acts on, and it must not start a server to find that out.
	if got := Sessions("definitely-not-a-real-socket-xyz"); len(got) != 0 {
		t.Errorf("Sessions(dead socket) = %v, want none", got)
	}
	EnsureBackKey("definitely-not-a-real-socket-xyz")
	EnsureFocusEvents()
}

// AttachSwitches answers one question — does AttachCmd's exit mean the human is
// back, or that they have just left — and the cockpit's return-trip recheck
// hangs off it. It must agree with the command AttachCmd actually builds, or
// the cockpit waits for a return that already happened (or rechecks one that
// has not).
func TestAttachSwitchesMatchesTheCommandBuilt(t *testing.T) {
	for _, tc := range []struct {
		name, tmux, socket, wantArg string
		wantSwitches                bool
	}{
		{name: "inside tmux", tmux: "/tmp/tmux-501/default,1,0", wantArg: "switch-client", wantSwitches: true},
		{name: "bare terminal", tmux: "", wantArg: "attach-session", wantSwitches: false},
		// A repo names its own server, so the cockpit is routinely in a client
		// of a different one. switch-client cannot cross servers, so this has
		// to be a nested attach — which is also the honest answer for the
		// caller, because that attach runs in the cockpit's own pane and exits
		// when the human detaches.
		{name: "another repo's server", tmux: "/tmp/tmux-501/default,1,0", socket: "player-app", wantArg: "attach-session", wantSwitches: false},
		{name: "this session's own server", tmux: "/tmp/tmux-501/player-app,1,0", socket: "player-app", wantArg: "switch-client", wantSwitches: true},
		{name: "a named server from a bare terminal", tmux: "", socket: "player-app", wantArg: "attach-session", wantSwitches: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMUX", tc.tmux)
			sess := Session{Name: "some-session", Socket: tc.socket}
			if got := AttachSwitches(sess); got != tc.wantSwitches {
				t.Errorf("AttachSwitches() = %v, want %v", got, tc.wantSwitches)
			}
			args := AttachCmd(sess, "").Args
			if !slices.Contains(args, tc.wantArg) {
				t.Errorf("AttachCmd args = %v, want %q — it disagrees with AttachSwitches", args, tc.wantArg)
			}
			if tc.socket != "" && !slices.Contains(args, "-L") {
				t.Errorf("AttachCmd args = %v, want the session's own server named", args)
			}
		})
	}
}
