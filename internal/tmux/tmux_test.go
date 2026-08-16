package tmux

import "testing"

// SessionIdle decides whether a dispatch session's claude process has ended and
// its name can be taken back, so what counts as "a shell" is the whole of it. A
// login shell reports with a leading dash, and anything that is not a shell is
// something running.
func TestShellCommandsCoverALoginShell(t *testing.T) {
	idle := paneIdle
	for _, sh := range []string{"zsh", "-zsh", "bash", "-bash", "sh", "fish"} {
		if !idle(sh) {
			t.Errorf("%q should read as an idle shell", sh)
		}
	}
	for _, busy := range []string{"claude", "node", "vim", "git", "python3"} {
		if idle(busy) {
			t.Errorf("%q should read as something running", busy)
		}
	}
}

// A session tmux cannot answer for is unknown, never idle: the caller reclaims
// an idle session and leaves an unknown one alone.
func TestSessionIdleIsUnknownForAMissingSession(t *testing.T) {
	idle, known := SessionIdle("definitely-not-a-real-tmux-session-xyz")
	if known || idle {
		t.Errorf("SessionIdle(missing) = idle:%v known:%v", idle, known)
	}
}
