//go:build windows

package supervisor

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file implements "jump in" on the Windows console backend. tmux hands the
// current terminal to the session; Windows has no such in-terminal handoff
// because each dispatch already owns a separate console window. The analogue is
// to raise that window to the foreground. This runs as the hidden
// `claude-dispatcher win-focus <name>` subcommand, which the cockpit invokes via
// tea.ExecProcess(AttachCmd(...)).
//
// Best-effort and a preview: Windows foreground-lock rules can refuse a raise
// requested by a process that does not own the current foreground window, in
// which case SetForegroundWindow may only flash the taskbar entry. Pending
// real-Windows validation.

var (
	moduser32               = windows.NewLazySystemDLL("user32.dll")
	procGetWindowTextW      = moduser32.NewProc("GetWindowTextW")
	procShowWindow          = moduser32.NewProc("ShowWindow")
	procSetForegroundWindow = moduser32.NewProc("SetForegroundWindow")
)

// windowMatches decides whether a top-level window with the given title and
// owning PID is the console window for session name (whose tracked PID is
// wantPID). We accept either a PID match (robust) or a title match — NewSession
// titles each console after its session name. Pure: unit-testable with fakes.
func windowMatches(title string, pid uint32, name string, wantPID uint32) bool {
	if wantPID != 0 && pid == wantPID {
		return true
	}
	return title != "" && title == name
}

// windowTitle returns hwnd's title text via GetWindowTextW.
func windowTitle(hwnd windows.HWND) string {
	buf := make([]uint16, 256)
	r1, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	n := int(r1)
	if n <= 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// WinFocus raises the session's console window to the foreground. It enumerates
// top-level windows, picks the first matching name/PID (see windowMatches), then
// ShowWindow(SW_RESTORE) + SetForegroundWindow. Best-effort; never panics.
func WinFocus(name string) error {
	reg := loadRegistry()
	wantPID := uint32(0)
	if pid, ok := reg[name]; ok {
		wantPID = uint32(pid)
	}

	var found windows.HWND
	cb := windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var pid uint32
		windows.GetWindowThreadProcessId(hwnd, &pid)
		if windowMatches(windowTitle(hwnd), pid, name, wantPID) {
			found = hwnd
			return 0 // stop enumeration
		}
		return 1 // continue enumeration
	})
	_ = windows.EnumWindows(cb, nil)

	if found == 0 {
		return fmt.Errorf("no console window found for session %q", name)
	}
	procShowWindow.Call(uintptr(found), uintptr(windows.SW_RESTORE))
	if r1, _, err := procSetForegroundWindow.Call(uintptr(found)); r1 == 0 {
		return fmt.Errorf("SetForegroundWindow for %q failed (foreground lock?): %w", name, err)
	}
	return nil
}
