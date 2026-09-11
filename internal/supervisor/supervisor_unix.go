//go:build !windows

package supervisor

import (
	"os/exec"

	"claude-dispatcher/internal/tmux"
)

// The Unix backend is tmux — sessions survive cockpit restarts and "jump in"
// is a full-fidelity attach. Every call delegates to internal/tmux.

func server(s Session) tmux.Server { return tmux.Server{Socket: s.Socket} }

func Available() bool           { return tmux.Available() }
func Backend() string           { return "tmux" }
func HasSession(s Session) bool { return server(s).HasSession(s.Name) }
func Sessions(socket string) []string {
	return tmux.Server{Socket: socket}.ListSessions()
}
func NewSession(s Session, dir, shellCmd, env string) error {
	return server(s).NewSession(s.Name, dir, shellCmd, env)
}
func AttachCmd(s Session, env string) *exec.Cmd { return server(s).AttachCmd(s.Name, env) }
func KillSession(s Session) error               { return server(s).KillSession(s.Name) }
func UniqueName(s Session) string               { return server(s).UniqueName(s.Name) }
func SetStatusHint(s Session)                   { server(s).SetStatusHint(s.Name) }

// SessionIdle reports whether a session is sitting at its shell with nothing
// running — the state a dispatch session is left in once claude exits. known is
// false when the backend cannot tell, which is never the same answer as "idle".
func SessionIdle(s Session) (idle, known bool) { return server(s).SessionIdle(s.Name) }

// EnsureBackKey binds the prefix-free "back to the cockpit" key (Ctrl-\) on
// every server a dispatch may live on. It is bound server-wide, so a server
// this cockpit has never started a session on has never been told about it.
func EnsureBackKey(sockets ...string) {
	for _, sock := range append([]string{""}, sockets...) {
		tmux.Server{Socket: sock}.EnsureDetachKey()
	}
}

// EnsureFocusEvents turns on tmux's focus reporting, which is what tells a
// cockpit hosted in tmux that its pane has come back to the front. It is asked
// of the server the COCKPIT is in — the bare invocation, which $TMUX routes —
// because that is the client whose focus is in question.
func EnsureFocusEvents() { tmux.Default.EnsureFocusEvents() }

// AttachSwitches reports whether AttachCmd moves the human to another client
// instead of taking this terminal over until they detach. True only inside a
// client of the session's OWN server: switch-client cannot cross servers, so a
// dispatch on another socket is a nested attach that exits on the way home.
func AttachSwitches(s Session) bool { return server(s).AttachSwitches() }

// SendKeys types text into the session and presses Enter, as if at the prompt.
func SendKeys(s Session, text string) error {
	args := []string{}
	if s.Socket != "" {
		args = append(args, "-L", s.Socket)
	}
	args = append(args, "send-keys", "-t", "="+s.Name, text, "Enter")
	return exec.Command("tmux", args...).Run()
}
