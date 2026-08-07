//go:build windows

package supervisor

import (
	"errors"
	"os/exec"
)

// The Windows backend is not implemented yet. tmux has no native Windows build,
// so a ConPTY-based session manager will replace it here. Until then the cockpit
// still builds and runs on Windows as a read-only viewer — it renders the status
// that lifecycle hooks write — but cannot launch, attach to, or kill sessions.
//
// Available() reporting false is the single signal the rest of the app checks;
// the mutating calls return errNotImpl so callers surface an honest notice.

var errNotImpl = errors.New("session supervisor not implemented on Windows yet — run under WSL2 for now")

func Available() bool             { return false }
func Backend() string             { return "none (Windows)" }
func HasSession(name string) bool { return false }

func NewSession(name, dir, shellCmd string) error { return errNotImpl }
func KillSession(name string) error               { return errNotImpl }
func SendKeys(name, text string) error            { return errNotImpl }

func UniqueName(base string) string { return base }
func SetStatusHint(name string)     {}
func EnsureBackKey()                {}

// AttachCmd returns a command that prints the not-supported message, so a
// tea.ExecProcess attach degrades to a clear line rather than a broken handoff.
func AttachCmd(name string) *exec.Cmd {
	return exec.Command("cmd", "/c", "echo", "claude-dispatcher: attaching is not supported on Windows yet (run under WSL2).")
}
