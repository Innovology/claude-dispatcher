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

// KnowShell tells the backend about a shell a pane may be sitting at, so that
// a session parked at it reads as idle rather than busy. The configured shell
// is passed at startup, because a pane the cockpit did not launch in this run
// is judged before anything else could teach it the name.
func KnowShell(shell string)      { tmux.KnowShell(shell) }
func KillSession(s Session) error { return server(s).KillSession(s.Name) }
func UniqueName(s Session) string { return server(s).UniqueName(s.Name) }
func SetStatusHint(s Session)     { server(s).SetStatusHint(s.Name) }

// Servers is every server with a socket on this machine, by socket name. It
// finds servers this cockpit never started — the per-project ones a human keeps
// themselves — because it looks where tmux puts sockets rather than at what we
// happen to have recorded.
func Servers() []string {
	var out []string
	for _, s := range tmux.Servers() {
		out = append(out, s.Socket)
	}
	return out
}

// SessionList is every session on one server, with the directory each runs in.
// A socket whose server has gone lists nothing and starts nothing.
func SessionList(socket string) []SessionInfo {
	var out []SessionInfo
	for _, s := range (tmux.Server{Socket: socket}).SessionList() {
		out = append(out, SessionInfo{
			Session: Session{Name: s.Name, Socket: socket},
			Path:    s.Path,
		})
	}
	return out
}

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
//
// The target is "=name:" — the session's current pane — not the "=name" every
// session-level command here takes: send-keys wants a pane, and tmux (3.7b,
// measured) answers "=name" with "can't find pane", so the plain form typed
// nothing, ever, and the reply that used it said it had. The text goes with -l
// and after --, so it is typed as written: without them tmux looks each
// argument up as a key name first, and a reply of "Enter", "Up" or "C-c" would
// be pressed rather than typed, and one starting with "-" read as a flag.
// Enter is its own call because it is the one argument that must be a key.
//
// It takes the whole address rather than a name: a repo's sessions live on
// that repo's own server (ADR 0014), and a reply typed at the default server
// is typed at a session that is not there.
func SendKeys(s Session, text string) error {
	target := "=" + s.Name + ":"
	if out, err := sendKeysCmd(s, "-t", target, "-l", "--", text).CombinedOutput(); err != nil {
		return sendErr(out, err)
	}
	if out, err := sendKeysCmd(s, "-t", target, "Enter").CombinedOutput(); err != nil {
		return sendErr(out, err)
	}
	return nil
}

// sendKeysCmd is one send-keys invocation against the session's own server.
func sendKeysCmd(s Session, args ...string) *exec.Cmd {
	argv := []string{}
	if s.Socket != "" {
		argv = append(argv, "-L", s.Socket)
	}
	argv = append(argv, "send-keys")
	return exec.Command("tmux", append(argv, args...)...)
}

// sendErr carries tmux's own words when it gave any.
func sendErr(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New(msg)
	}
	return err
}
