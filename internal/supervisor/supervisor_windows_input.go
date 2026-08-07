//go:build windows

package supervisor

import (
	"fmt"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file implements "reply" on the Windows console backend: injecting text
// into another dispatch's console as if it were typed at the prompt.
//
// The mechanism is AttachConsole(pid) → WriteConsoleInput → FreeConsole.
// WriteConsoleInput does not write to the screen — it appends INPUT_RECORDs to
// the console's *input buffer*, which is exactly where a running program (the
// claude session) reads keystrokes from. So a KEY_EVENT record carrying a
// character is indistinguishable from the user having typed it.
//
// It is best-effort and pending real-Windows validation: a process may attach
// to at most one console at a time, so if the cockpit itself owns a console
// AttachConsole returns access-denied and reply reports a clear error rather
// than a silent success. We deliberately do NOT free the caller's own console
// to force a retry — that would risk leaving the running cockpit TUI without a
// console. A fully robust version would perform the injection from a
// short-lived helper process that starts with no console of its own.

var (
	modkernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole     = modkernel32.NewProc("AttachConsole")
	procFreeConsole       = modkernel32.NewProc("FreeConsole")
	procWriteConsoleInput = modkernel32.NewProc("WriteConsoleInputW")
)

const (
	// keyEventType is INPUT_RECORD.EventType == KEY_EVENT.
	keyEventType = 0x0001
	// vkReturn is VK_RETURN, the virtual-key code for Enter.
	vkReturn = 0x0D
)

// inputRecord mirrors the Win32 INPUT_RECORD holding a KEY_EVENT_RECORD payload.
// The two-byte pad after eventType reproduces the union's DWORD alignment so the
// field offsets match the C struct WriteConsoleInput expects; keep the layout in
// sync with that ABI.
type inputRecord struct {
	eventType       uint16
	_               uint16 // union alignment padding (EventType is WORD, union is DWORD-aligned)
	keyDown         int32  // BOOL bKeyDown
	repeatCount     uint16 // WORD wRepeatCount
	virtualKeyCode  uint16 // WORD wVirtualKeyCode
	virtualScanCode uint16 // WORD wVirtualScanCode
	unicodeChar     uint16 // WCHAR uChar.UnicodeChar
	controlKeyState uint32 // DWORD dwControlKeyState
}

// keyDownRecord builds one key-down INPUT_RECORD carrying UTF-16 code unit ch
// and virtual-key code vk (vk may be 0 for ordinary characters).
func keyDownRecord(ch uint16, vk uint16) inputRecord {
	return inputRecord{
		eventType:      keyEventType,
		keyDown:        1,
		repeatCount:    1,
		virtualKeyCode: vk,
		unicodeChar:    ch,
	}
}

// encodeKeystrokes turns text into a sequence of key-down INPUT_RECORDs — one
// per UTF-16 code unit — followed by a Return keypress, exactly as if the text
// were typed and Enter pressed. Pure (no syscalls) so it is unit-testable.
func encodeKeystrokes(text string) []inputRecord {
	units := utf16.Encode([]rune(text))
	recs := make([]inputRecord, 0, len(units)+1)
	for _, u := range units {
		recs = append(recs, keyDownRecord(u, 0))
	}
	recs = append(recs, keyDownRecord('\r', vkReturn))
	return recs
}

// injectConsoleInput attaches to pid's console, writes recs to its input buffer,
// then detaches. Everything is guarded; it never panics.
func injectConsoleInput(pid int, recs []inputRecord) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	if len(recs) == 0 {
		return nil
	}
	// Attach to the target's console. A process can hold at most one console,
	// so this fails with access-denied when the caller already owns one; we
	// surface that honestly rather than tampering with the caller's console.
	if r1, _, err := procAttachConsole.Call(uintptr(pid)); r1 == 0 {
		return fmt.Errorf("AttachConsole(%d): %w", pid, err)
	}
	defer procFreeConsole.Call()

	// Open the now-attached console's input buffer. CONIN$ resolves to the
	// active console, which after AttachConsole is pid's.
	name, err := windows.UTF16PtrFromString("CONIN$")
	if err != nil {
		return fmt.Errorf("encode CONIN$: %w", err)
	}
	conin, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0,
	)
	if err != nil {
		return fmt.Errorf("open CONIN$: %w", err)
	}
	defer windows.CloseHandle(conin)

	var written uint32
	if r1, _, err := procWriteConsoleInput.Call(
		uintptr(conin),
		uintptr(unsafe.Pointer(&recs[0])),
		uintptr(len(recs)),
		uintptr(unsafe.Pointer(&written)),
	); r1 == 0 {
		return fmt.Errorf("WriteConsoleInput: %w", err)
	}
	return nil
}
