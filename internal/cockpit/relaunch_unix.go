//go:build !windows

package cockpit

// relaunch_unix.go turns "upgraded" into "running the upgrade" without the
// human doing anything.

import (
	"os"
	"os/exec"
	"syscall"
)

// relaunch replaces this process with the build that was just installed.
//
// syscall.Exec, not a child process: the cockpit is usually the foreground job
// of a terminal the human is sitting in, and often a tmux pane. Spawning a
// child would leave the old binary alive as its parent, holding the pane's
// process group, and the human would be looking at a program inside a program.
// Exec keeps the pid, the terminal and the place in the window; the only thing
// that changes is the code.
//
// It must run after Bubble Tea has returned — the alt screen has to be left and
// raw mode restored before the new process starts, or it inherits a terminal
// it never configured and cannot reason about.
//
// The path is looked up on PATH first, deliberately. A cask upgrade repoints
// /opt/homebrew/bin/claude-dispatcher at a new Caskroom directory and may
// remove the old one, so os.Executable's already-resolved path can name a
// binary that no longer exists. PATH resolves to whatever was just installed.
func relaunch() error {
	exe, err := exec.LookPath(binaryName)
	if err != nil {
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	// Returns only on failure.
	return syscall.Exec(exe, append([]string{exe}, os.Args[1:]...), os.Environ())
}
