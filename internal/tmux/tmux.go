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

// NewSession starts a detached session running shellCommand in dir.
func NewSession(name, dir, shellCommand string) error {
	out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, shellCommand).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func KillSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", "="+name).Run()
}

// AttachCmd returns the command that hands the terminal over to a session.
// Inside an existing tmux client we switch rather than nest.
func AttachCmd(name string) *exec.Cmd {
	if os.Getenv("TMUX") != "" {
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
