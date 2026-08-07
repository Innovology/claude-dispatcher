//go:build windows

// The Windows backend is a first implementation. tmux has no native Windows
// build, so instead of one multiplexer hosting many sessions, each dispatch
// runs its claude process in its OWN detached console window, tracked by PID in
// a small JSON registry under state.Dir(). This makes the supervisor surface
// real on Windows without WSL2.
//
// Known gaps versus the tmux backend, honest by design:
//   - In-place attach ("jump in") is not possible — the session already owns
//     its own console window; AttachCmd only prints where to find it.
//   - Reply / SendKeys into another console is not reliably possible, so it
//     returns a clear error rather than pretending.
//
// This file is validated to compile and vet under GOOS=windows; its runtime
// behaviour is pending a real Windows run.
package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"

	"claude-dispatcher/internal/state"
)

// stillActive is the exit code GetExitCodeProcess reports for a running process
// (Windows STILL_ACTIVE / STATUS_PENDING).
const stillActive = 259

func Available() bool { return true }
func Backend() string { return "windows-console" }

// registryPath is the JSON file mapping session name → child PID.
func registryPath() string { return filepath.Join(state.Dir(), "win-sessions.json") }

// loadRegistry reads the name→PID map, returning an empty map on any error so
// callers never have to nil-check. All registry IO is best-effort.
func loadRegistry() map[string]int {
	reg := map[string]int{}
	b, err := os.ReadFile(registryPath())
	if err != nil {
		return reg
	}
	_ = json.Unmarshal(b, &reg)
	if reg == nil {
		reg = map[string]int{}
	}
	return reg
}

// saveRegistry atomically writes the name→PID map (tmp file + rename).
func saveRegistry(reg map[string]int) error {
	if err := state.EnsureDirs(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	final := registryPath()
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// processAlive reports whether pid names a live process, via OpenProcess +
// GetExitCodeProcess. A process that has exited reports its real exit code;
// only a running one reports STILL_ACTIVE.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// NewSession launches shellCmd in a NEW detached console window rooted at dir,
// running it through cmd.exe, and records name→PID in the registry. The window
// is titled name so the user can find it (see AttachCmd). Start (not Run) so
// the process detaches and outlives this call.
func NewSession(name, dir, shellCmd string) error {
	// Title the window after the session so the user can locate it, then run
	// the launch command.
	full := "title " + name + " && " + shellCmd
	cmd := exec.Command("cmd.exe", "/c", full)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_CONSOLE,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start console session %q: %w", name, err)
	}
	reg := loadRegistry()
	reg[name] = cmd.Process.Pid
	return saveRegistry(reg)
}

// HasSession reports whether name's tracked process is still alive, pruning the
// registry entry when it has exited.
func HasSession(name string) bool {
	reg := loadRegistry()
	pid, ok := reg[name]
	if !ok {
		return false
	}
	if processAlive(pid) {
		return true
	}
	delete(reg, name)
	_ = saveRegistry(reg)
	return false
}

// KillSession terminates name's tracked process tree and drops it from the
// registry. taskkill /T kills the console's child claude process too.
func KillSession(name string) error {
	reg := loadRegistry()
	pid, ok := reg[name]
	if ok {
		delete(reg, name)
		_ = saveRegistry(reg)
	}
	if !ok {
		return nil
	}
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

// SendKeys is not supported: injecting input into another process's console is
// not reliably possible. Return an honest error rather than a silent no-op.
func SendKeys(name, text string) error {
	return errors.New("reply is not supported on the Windows console backend yet — attach to the window and type")
}

// AttachCmd cannot hand the terminal over — the session runs in its own console
// window. Return a command that prints where to find it, so a tea.ExecProcess
// attach degrades to a clear line rather than a broken handoff.
func AttachCmd(name string) *exec.Cmd {
	return exec.Command("cmd", "/c", "echo",
		fmt.Sprintf("claude-dispatcher: session %s runs in its own console window titled %q -- switch to that window to attach.", name, name))
}

// UniqueName returns base, or base-2, base-3, … if a session already exists.
func UniqueName(base string) string {
	name := base
	for i := 2; HasSession(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

// SetStatusHint and EnsureBackKey are tmux status-line / key-binding concerns
// with no console-window analogue; they are no-ops on Windows.
func SetStatusHint(name string) {}
func EnsureBackKey()            {}
