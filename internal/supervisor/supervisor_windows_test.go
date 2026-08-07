//go:build windows

package supervisor

import (
	"reflect"
	"testing"
)

func TestBackend(t *testing.T) {
	if got := Backend(); got != "windows-console" {
		t.Errorf("Backend() = %q, want windows-console", got)
	}
	if !Available() {
		t.Error("Available() = false, want true on Windows")
	}
}

func TestUniqueNameFree(t *testing.T) {
	// Isolate the registry so no session is ever "taken".
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if got := UniqueName("disp-fresh"); got != "disp-fresh" {
		t.Errorf("UniqueName of a free name = %q, want disp-fresh", got)
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	want := map[string]int{"disp-a": 111, "disp-b": 222}
	if err := saveRegistry(want); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}
	got := loadRegistry()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadRegistry() = %v, want %v", got, want)
	}
}

func TestLoadRegistryMissingIsEmpty(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if got := loadRegistry(); len(got) != 0 {
		t.Errorf("loadRegistry() on missing file = %v, want empty", got)
	}
}

// TestEncodeKeystrokes checks the pure rune→INPUT_RECORD encoding: one key-down
// record per UTF-16 code unit, then a trailing Return.
func TestEncodeKeystrokes(t *testing.T) {
	recs := encodeKeystrokes("hi")
	// "hi" is two code units + the Return record.
	if len(recs) != 3 {
		t.Fatalf("encodeKeystrokes(\"hi\") produced %d records, want 3", len(recs))
	}
	for i, want := range []uint16{'h', 'i'} {
		r := recs[i]
		if r.eventType != keyEventType {
			t.Errorf("record %d eventType = %#x, want KEY_EVENT %#x", i, r.eventType, keyEventType)
		}
		if r.keyDown != 1 {
			t.Errorf("record %d keyDown = %d, want 1", i, r.keyDown)
		}
		if r.unicodeChar != want {
			t.Errorf("record %d unicodeChar = %#x, want %#x", i, r.unicodeChar, want)
		}
		if r.virtualKeyCode != 0 {
			t.Errorf("record %d virtualKeyCode = %#x, want 0 for a plain char", i, r.virtualKeyCode)
		}
	}
	ret := recs[2]
	if ret.virtualKeyCode != vkReturn || ret.unicodeChar != '\r' {
		t.Errorf("final record = {vk %#x, ch %#x}, want VK_RETURN %#x + '\\r'", ret.virtualKeyCode, ret.unicodeChar, vkReturn)
	}
}

// TestEncodeKeystrokesEmpty: even empty text still submits (Enter only).
func TestEncodeKeystrokesEmpty(t *testing.T) {
	recs := encodeKeystrokes("")
	if len(recs) != 1 || recs[0].virtualKeyCode != vkReturn {
		t.Fatalf("encodeKeystrokes(\"\") = %+v, want a single Return record", recs)
	}
}

// TestEncodeKeystrokesSurrogatePair: an astral-plane rune becomes two UTF-16
// code units, so it yields two char records plus Return.
func TestEncodeKeystrokesSurrogatePair(t *testing.T) {
	recs := encodeKeystrokes("\U0001F680") // rocket emoji
	if len(recs) != 3 {
		t.Fatalf("astral rune produced %d records, want 3 (2 surrogates + Return)", len(recs))
	}
	if recs[0].unicodeChar < 0xD800 || recs[0].unicodeChar > 0xDBFF {
		t.Errorf("first unit %#x is not a high surrogate", recs[0].unicodeChar)
	}
	if recs[1].unicodeChar < 0xDC00 || recs[1].unicodeChar > 0xDFFF {
		t.Errorf("second unit %#x is not a low surrogate", recs[1].unicodeChar)
	}
}

// TestWindowMatches exercises the pure window-matching predicate with fakes:
// PID match wins even on a title mismatch; otherwise a title match; and an empty
// title never matches.
func TestWindowMatches(t *testing.T) {
	cases := []struct {
		name     string
		title    string
		pid      uint32
		wantName string
		wantPID  uint32
		want     bool
	}{
		{"pid match beats title", "some other title", 42, "disp-x", 42, true},
		{"title match when no pid", "disp-x", 100, "disp-x", 0, true},
		{"title match with wrong pid still ok", "disp-x", 100, "disp-x", 999, true},
		{"no match", "disp-y", 100, "disp-x", 999, false},
		{"empty title never matches by title", "", 100, "", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := windowMatches(c.title, c.pid, c.wantName, c.wantPID); got != c.want {
				t.Errorf("windowMatches(%q, %d, %q, %d) = %v, want %v",
					c.title, c.pid, c.wantName, c.wantPID, got, c.want)
			}
		})
	}
}

// TestSendKeysUnknownSession: reply to a name with no registry entry returns a
// clear error, never a silent success or a panic.
func TestSendKeysUnknownSession(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if err := SendKeys("disp-nope", "hello"); err == nil {
		t.Error("SendKeys on an unknown session returned nil, want a clear error")
	}
}

// TestWinFocusUnknownSession: focusing a session with no matching window returns
// an error rather than panicking. (No real window exists under test.)
func TestWinFocusUnknownSession(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if err := WinFocus("disp-nope"); err == nil {
		t.Error("WinFocus on an unknown session returned nil, want an error")
	}
}
