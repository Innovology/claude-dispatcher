//go:build windows

// The Windows backend is a first implementation. tmux has no native Windows
// build, so instead of one multiplexer hosting many sessions, each dispatch
// runs its claude process in its OWN detached console window, tracked by PID in
// a small JSON registry under state.Dir(). This makes the supervisor surface
// real on Windows without WSL2.
//
// Parity with the tmux backend, best-effort and pending real-Windows validation:
//   - "Jump in" (attach) raises the session's own console window to the
//     foreground. There is no in-terminal handoff like tmux; foregrounding the
//     session's window is the analogue. AttachCmd shells out to the hidden
//     `win-focus` subcommand (see supervisor_windows_focus.go), which the
//     cockpit runs via tea.ExecProcess. Windows foreground-lock rules may limit
//     the raise — it is a preview.
//   - Reply (SendKeys) injects the text as console input into the session's
//     process via AttachConsole + WriteConsoleInput (see
//     supervisor_windows_input.go), so the running claude reads it as typed
//     input. It fails with a clear error rather than a silent success when the
//     caller's console prevents attaching.
//
// This file is validated to compile and vet under GOOS=windows; its runtime
// behaviour is pending a real Windows run.
package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// Sessions names every tracked process still alive, pruning the ones that have
// exited in the same pass. It is the whole-registry form of HasSession.
func Sessions() []string {
	reg := loadRegistry()
	var live []string
	changed := false
	for name, pid := range reg {
		if processAlive(pid) {
			live = append(live, name)
			continue
		}
		delete(reg, name)
		changed = true
	}
	if changed {
		_ = saveRegistry(reg)
	}
	sort.Strings(live)
	return live
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

// SendKeys injects text (plus Enter) into the session's console as if typed at
// the prompt: it looks up the session's PID, then attaches to that console and
// writes the keystrokes to its input buffer (see supervisor_windows_input.go).
// Best-effort — a clear error, never a silent success, if the injection fails.
func SendKeys(name, text string) error {
	reg := loadRegistry()
	pid, ok := reg[name]
	if !ok {
		return fmt.Errorf("reply: no tracked session %q", name)
	}
	if !processAlive(pid) {
		return fmt.Errorf("reply: session %q is not running", name)
	}
	return injectConsoleInput(pid, encodeKeystrokes(text))
}

// AttachCmd raises the session's console window to the foreground by re-invoking
// this binary's hidden `win-focus <name>` subcommand. The cockpit runs it via
// tea.ExecProcess; on Windows there is no in-terminal handoff like tmux, so
// foregrounding the session's own window is the analogue (a preview — see the
// package doc and supervisor_windows_focus.go). If we cannot resolve our own
// path, fall back to a command that prints where to find the window.
func AttachCmd(name string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return exec.Command("cmd", "/c", "echo",
			fmt.Sprintf("claude-dispatcher: session %s runs in its own console window titled %q -- switch to that window to attach.", name, name))
	}
	return exec.Command(exe, "win-focus", name)
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
// with no console-window analogue; they are no-ops on Windows. Focus reporting
// is the console's own business rather than something we can switch on for it,
// so EnsureFocusEvents is a no-op too.
func SetStatusHint(name string) {}
func EnsureBackKey()            {}
func EnsureFocusEvents()        {}

// AttachSwitches is true here for the same reason it is true inside tmux: the
// Windows handover raises the session's own console window and returns at once,
// so the human is over there and this command's exit is not their return. The
// cockpit therefore waits for focus rather than rechecking on that exit — see
// the attachReturnedMsg case in internal/cockpit.
func AttachSwitches() bool { return true }
