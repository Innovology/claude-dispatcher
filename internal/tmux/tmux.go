// Package tmux wraps the tmux CLI. tmux is the session substrate: every
// dispatcher runs as an interactive claude process inside its own tmux
// session, so sessions survive cockpit restarts and "jump in" is a plain
// attach at full fidelity.
package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func HasSession(name string) bool {
	return exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil
}

// ListSessions names every live session on the server, in one round trip.
// HasSession answers the same question for one name, but asking it per record
// costs a subprocess per dispatch; the opening screen wants the whole picture
// at once. No server running is not an error — it is zero sessions, which is
// exactly what a machine with nothing dispatched looks like.
func ListSessions() []string {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, ln := range strings.Split(string(out), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			names = append(names, ln)
		}
	}
	return names
}

// NewSession starts a detached session running shellCommand in dir.
func NewSession(name, dir, shellCommand string) error {
	out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, shellCommand).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session: %s", strings.TrimSpace(string(out)))
	}
	EnsureDetachKey()
	SetStatusHint(name)
	return nil
}

// SetStatusHint puts the way home in the session's status line. Note the
// trailing colon: set-option rejects the "=name" exact-match form that
// attach/kill accept.
func SetStatusHint(name string) {
	_ = exec.Command("tmux", "set-option", "-t", name+":", "status-right",
		` Ctrl-\ → back to dispatch `).Run()
}

// EnsureDetachKey binds Ctrl-\ (prefix-free, server-wide) to detach. The
// default Ctrl-b d is a timed sequence that trips people up — holding Ctrl
// for the whole chord gets silently swallowed by tmux. One chord instead.
func EnsureDetachKey() {
	_ = exec.Command("tmux", "bind-key", "-n", `C-\`, "detach-client").Run()
}

// EnsureFocusEvents asks tmux to pass focus in and out through to the programs
// it hosts. tmux ships with it off, and without it a cockpit running inside
// tmux is never told when its own pane comes back to the front — which is the
// only notice it gets that the human has returned from a session it switched
// them to, because switch-client exits at the moment they leave rather than the
// moment they come back (see AttachSwitches). Set server-wide, like the detach
// key, so it covers the cockpit's client and not just the sessions we start.
func EnsureFocusEvents() {
	_ = exec.Command("tmux", "set-option", "-g", "focus-events", "on").Run()
}

// AttachSwitches reports whether AttachCmd moves the human to another client
// instead of taking this terminal over until they detach — which decides
// whether that command's exit means "they are back" or "they have just left".
func AttachSwitches() bool { return os.Getenv("TMUX") != "" }

func KillSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", "="+name).Run()
}

// shellCommands are the pane commands that mean "nothing is running here": the
// login shell a dispatch session drops to once claude exits (see
// launchCommand). A login shell reports as "-bash", hence the leading dash trim
// in paneIdle.
var shellCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true,
	"dash": true, "ksh": true, "csh": true, "tcsh": true, "login": true,
}

// paneIdle reports whether a pane's current command is a shell waiting for
// input rather than a process doing something.
func paneIdle(cmd string) bool { return shellCommands[strings.TrimPrefix(cmd, "-")] }

// SessionIdle reports whether every pane of a session is sitting at a shell —
// that is, whether the claude process it was launched with has ended.
//
// known is false when tmux could not answer (no such session, no tmux). The
// distinction matters to the caller: "idle" licenses reusing the session, and
// "unknown" must never be read as either — a session we cannot see into is one
// we leave alone.
func SessionIdle(name string) (idle, known bool) {
	out, err := exec.Command("tmux", "list-panes", "-t", "="+name, "-F", "#{pane_current_command}").Output()
	if err != nil {
		return false, false
	}
	lines := strings.Fields(string(out))
	if len(lines) == 0 {
		return false, false
	}
	for _, cmd := range lines {
		if !paneIdle(cmd) {
			return false, true
		}
	}
	return true, true
}

// AttachCmd returns the command that hands the terminal over to a session.
// Inside an existing tmux client we switch rather than nest.
func AttachCmd(name string) *exec.Cmd {
	if AttachSwitches() {
		return exec.Command("tmux", "switch-client", "-t", "="+name)
	}
	return exec.Command("tmux", "attach-session", "-t", "="+name)
}

// UniqueName returns base, or base-2, base-3, … if a session already exists.
func UniqueName(base string) string {
	name := base
	for i := 2; HasSession(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}
