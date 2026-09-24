//go:build !windows

package supervisor

import (
	"errors"
	"os/exec"
	"strings"

	"claude-dispatcher/internal/tmux"
)

// The Unix backend is tmux — sessions survive cockpit restarts and "jump in"
// is a full-fidelity attach. Every call delegates to internal/tmux.

func Available() bool                             { return tmux.Available() }
func Backend() string                             { return "tmux" }
func HasSession(name string) bool                 { return tmux.HasSession(name) }
func Sessions() []string                          { return tmux.ListSessions() }
func NewSession(name, dir, shellCmd string) error { return tmux.NewSession(name, dir, shellCmd) }
func AttachCmd(name string) *exec.Cmd             { return tmux.AttachCmd(name) }
func KillSession(name string) error               { return tmux.KillSession(name) }
func UniqueName(base string) string               { return tmux.UniqueName(base) }
func SetStatusHint(name string)                   { tmux.SetStatusHint(name) }

// SessionIdle reports whether a session is sitting at its shell with nothing
// running — the state a dispatch session is left in once claude exits. known is
// false when the backend cannot tell, which is never the same answer as "idle".
func SessionIdle(name string) (idle, known bool) { return tmux.SessionIdle(name) }

// EnsureBackKey binds the prefix-free "back to the cockpit" key (Ctrl-\).
func EnsureBackKey() { tmux.EnsureDetachKey() }

// EnsureFocusEvents turns on tmux's focus reporting, which is what tells a
// cockpit hosted in tmux that its pane has come back to the front.
func EnsureFocusEvents() { tmux.EnsureFocusEvents() }

// AttachSwitches is true inside tmux, where the handover is a switch-client
// that exits as the human leaves rather than when they come back.
func AttachSwitches() bool { return tmux.AttachSwitches() }

// SendKeys types text into the session and presses Enter, as if at the prompt.
//
// The target is "=name:" — the session's current pane — not the "=name" every
// session-level command here takes: send-keys wants a pane, and tmux (3.7b,
// measured) answers "=name" with "can't find pane", so the plain form typed
// nothing, ever, and the reply that used it said it had. The text goes with -l
// and after --, so it is typed as written: without them tmux looks each
// argument up as a key name first, and a reply of "Enter", "Up" or "C-c" would
// be pressed rather than typed, and one starting with "-" read as a flag.
// Enter is its own call because it is the one argument that must be a key.
func SendKeys(name, text string) error {
	target := "=" + name + ":"
	if out, err := exec.Command("tmux", "send-keys", "-t", target, "-l", "--", text).CombinedOutput(); err != nil {
		return sendErr(out, err)
	}
	if out, err := exec.Command("tmux", "send-keys", "-t", target, "Enter").CombinedOutput(); err != nil {
		return sendErr(out, err)
	}
	return nil
}

// sendErr carries tmux's own words when it gave any.
func sendErr(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New(msg)
	}
	return err
}
