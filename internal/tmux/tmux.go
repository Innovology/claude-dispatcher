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

// SessionIdle reports whether every pane of a session is sitting at a shell
// with nothing running under it — that is, whether the claude process it was
// launched with has ended.
//
// known is false when we could not answer (no such session, no tmux, no
// readable process table). The distinction matters to the caller: "idle"
// licenses reusing the session — Resume kills it, Launch stops treating the
// record as live — and "unknown" must never be read as either.
//
// `#{pane_current_command}` alone cannot answer this, and reading it as if it
// could is why every session we start looked idle while claude was running in
// it. A session is launched as `<shell> -c "… claude …; exec $SHELL"`, and a
// non-interactive shell has no job control, so claude never gets a process
// group of its own: the pane's foreground group leader stays the shell, and
// tmux dutifully reports "zsh". Measured on a live dispatcher — pane_pid 51122
// reporting "zsh" with claude running as its child, pid 51136.
//
// The child is the answer. A shell-looking pane is idle only if nothing is
// running under it; once claude exits, `exec` replaces that same shell with the
// login shell, which sits there childless. A pane tmux can name a real command
// for is still taken at its word (that is the same question, already answered),
// so nothing is lost where job control does apply.
func SessionIdle(name string) (idle, known bool) {
	out, err := exec.Command("tmux", "list-panes", "-t", "="+name, "-F", "#{pane_pid} #{pane_current_command}").Output()
	if err != nil {
		return false, false
	}
	return panesIdle(parsePanes(string(out)), processParents)
}

// panesIdle is SessionIdle's verdict, over what was read rather than over what
// it took to read it — the seam the tests drive, since a real answer needs a
// tmux server and a process of our own to be the child.
func panesIdle(panes []pane, parentsOf func() map[string]bool) (idle, known bool) {
	if len(panes) == 0 {
		return false, false
	}
	var parents map[string]bool // read at most once, and only if it is needed
	for _, p := range panes {
		if !paneIdle(p.cmd) {
			return false, true
		}
		if parents == nil {
			if parents = parentsOf(); parents == nil {
				// A shell in the foreground and no way to see what is under it
				// is exactly the case this function must not guess at.
				return false, false
			}
		}
		if parents[p.pid] {
			return false, true
		}
	}
	return true, true
}

// pane is one line of the list-panes read: the process tmux started for the
// pane, and what tmux believes is running in it.
type pane struct{ pid, cmd string }

func parsePanes(out string) []pane {
	var ps []pane
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		ps = append(ps, pane{pid: f[0], cmd: f[1]})
	}
	return ps
}

// processParents is the set of pids that have at least one live child, read in
// one pass over the process table. nil when the table could not be read, which
// callers must treat as "unknown" rather than as "no children".
//
// `ps -ax -o pid=,ppid=` is the portable spelling: it is the same on macOS and
// on the Linux distributions this runs under. pgrep -P would be shorter and is
// not dependable — under a sandboxed process table it reports no children for a
// parent that plainly has one, which is the exact wrong answer here.
func processParents() map[string]bool {
	out, err := exec.Command("ps", "-ax", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	parents := make(map[string]bool)
	for _, ln := range strings.Split(string(out), "\n") {
		if f := strings.Fields(ln); len(f) >= 2 {
			parents[f[1]] = true
		}
	}
	if len(parents) == 0 {
		return nil // a process table with no processes in it is a failed read
	}
	return parents
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
