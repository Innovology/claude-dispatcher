//go:build !windows

package supervisor

import (
	"os/exec"

	"claude-dispatcher/internal/tmux"
)

// The Unix backend is tmux — sessions survive cockpit restarts and "jump in"
// is a full-fidelity attach. Every call delegates to internal/tmux.

func Available() bool                             { return tmux.Available() }
func Backend() string                             { return "tmux" }
func HasSession(name string) bool                 { return tmux.HasSession(name) }
func NewSession(name, dir, shellCmd string) error { return tmux.NewSession(name, dir, shellCmd) }
func AttachCmd(name string) *exec.Cmd             { return tmux.AttachCmd(name) }
func KillSession(name string) error               { return tmux.KillSession(name) }
func UniqueName(base string) string               { return tmux.UniqueName(base) }
func SetStatusHint(name string)                   { tmux.SetStatusHint(name) }

// EnsureBackKey binds the prefix-free "back to the cockpit" key (Ctrl-\).
func EnsureBackKey() { tmux.EnsureDetachKey() }

// EnsureFocusEvents turns on tmux's focus reporting, which is what tells a
// cockpit hosted in tmux that its pane has come back to the front.
func EnsureFocusEvents() { tmux.EnsureFocusEvents() }

// AttachSwitches is true inside tmux, where the handover is a switch-client
// that exits as the human leaves rather than when they come back.
func AttachSwitches() bool { return tmux.AttachSwitches() }

// SendKeys types text into the session and presses Enter, as if at the prompt.
func SendKeys(name, text string) error {
	return exec.Command("tmux", "send-keys", "-t", "="+name, text, "Enter").Run()
}
