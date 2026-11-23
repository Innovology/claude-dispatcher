package cockpit

// dxcaret.go is the caret in the dx form's text fields.
//
// The caret used to sit at the end of a field and stay there: typing appended,
// backspace took the last character, and fixing a typo three words back meant
// deleting the three words. Asked for as "enable arrow keys navigation in the
// title". In the one-line fields — WHERE, TITLE, DONE WHEN, ROOT and PREFIX —
// left and right step a character, alt or ctrl with them (and alt+b/alt+f, what
// a Mac terminal sends for option-arrow) a word, home and end (ctrl+a, ctrl+e)
// go to the ends, and delete takes the character under the caret. Typing,
// backspace, ctrl+w, ctrl+u and ctrl+k all act at the caret. These are the keys
// bubbles' textinput answers to on the + overlay, less ctrl+d, which dispatches
// here.
//
// WHAT keeps its caret at the end. It wraps, and the wrap collapses runs of
// spaces (strings.Fields), so a position in its text is not a position on the
// screen without a layout that keeps one; a brief long enough to need moving
// around in is what ctrl+g is for (dxeditor.go).

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// dxCaret is where the caret stands: the bytes of text AFTER it, in the field
// it was put in.
//
// Counting from the end is what keeps it simple. Every edit is made at the
// caret, so what is after it does not change when something is typed, deleted
// or cleared before it; and the zero value is the end, which is where every
// field starts. Naming the field means a caret is never read against another
// field's text: dxKey sends it home whenever a key arrives in a field it was
// not put in, so a field left and come back to has its caret at the end again.
type dxCaret struct {
	field dxFieldID
	back  int
}

// dxText is a field's text as it reads.
func (m model) dxText(f dxFieldID) string {
	switch f {
	case dxWhereF:
		return m.dxFilter
	case dxTitleF:
		return m.dxTitle
	case dxWhatF:
		return m.dxWhat
	case dxGoalF:
		return m.dxGoal
	case dxRootF:
		return m.dxRoot
	case dxPrefixF:
		return m.dxPrefixText()
	}
	return ""
}

// dxSplit is f's text either side of the caret. A caret put in another field
// is the end of this one; one that no longer lands inside the text (or lands
// inside a character) is moved to the nearest place that does.
func (m model) dxSplit(f dxFieldID) (head, tail string) {
	s := m.dxText(f)
	back := 0
	if m.dxCaret.field == f {
		back = m.dxCaret.back
	}
	p := len(s) - mini(maxi(back, 0), len(s))
	for p > 0 && p < len(s) && !utf8.RuneStart(s[p]) {
		p--
	}
	return s[:p], s[p:]
}

// dxEdit applies an edit at the caret of field f and leaves the caret after
// whatever the edit leaves before it. WHERE sends its repo cursor home, because
// a different filter is a different list. PREFIX records the edit as this
// dispatch's override, so clearing it means "no prefix", which is an answer of
// its own (see dxPrefixText).
func (m model) dxEdit(f dxFieldID, edit func(head, tail string) (string, string)) model {
	head, tail := m.dxSplit(f)
	head, tail = edit(head, tail)
	s := head + tail
	switch f {
	case dxWhereF:
		m.dxFilter, m.dxRepo = s, 0
	case dxTitleF:
		m.dxTitle = s
	case dxWhatF:
		m.dxWhat = s
	case dxGoalF:
		m.dxGoal = s
	case dxRootF:
		m.dxRoot = s
	case dxPrefixF:
		m = m.dxSetPrefix(s)
	}
	m.dxCaret = dxCaret{field: f, back: len(tail)}
	return m
}

// dxMove moves the caret within field f and changes no text.
func (m model) dxMove(f dxFieldID, move func(head, tail string) (string, string)) model {
	head, tail := m.dxSplit(f)
	_, tail = move(head, tail)
	m.dxCaret = dxCaret{field: f, back: len(tail)}
	return m
}

// dxCaretKey is a key that edits or moves at the caret, and what it does. ok is
// false for a key that is neither, which the caller then reads as text.
//
// Moves are refused on WHAT (see the file comment): the caret there is the end,
// and a move would put it somewhere the wrapped box cannot draw.
func (m model) dxCaretKey(f dxFieldID, k string) (model, bool) {
	switch k {
	case "backspace", "ctrl+h":
		return m.dxEdit(f, func(h, t string) (string, string) { return dropLastCluster(h), t }), true
	case "delete":
		return m.dxEdit(f, func(h, t string) (string, string) { return h, t[len(firstCluster(t)):] }), true
	case "ctrl+w", "alt+backspace":
		return m.dxEdit(f, func(h, t string) (string, string) { return dxDropWord(h), t }), true
	case "ctrl+u":
		return m.dxEdit(f, func(_, t string) (string, string) { return "", t }), true
	case "ctrl+k":
		return m.dxEdit(f, func(h, _ string) (string, string) { return h, "" }), true
	}

	var move func(h, t string) (string, string)
	switch k {
	case "left", "ctrl+b":
		move = func(h, t string) (string, string) {
			c := lastCluster(h)
			return h[:len(h)-len(c)], c + t
		}
	case "right", "ctrl+f":
		move = func(h, t string) (string, string) {
			c := firstCluster(t)
			return h + c, t[len(c):]
		}
	case "alt+left", "ctrl+left", "alt+b":
		move = func(h, t string) (string, string) {
			kept := dxDropWord(h)
			return kept, h[len(kept):] + t
		}
	case "alt+right", "ctrl+right", "alt+f":
		move = func(h, t string) (string, string) {
			n := wordEnd(t)
			return h + t[:n], t[n:]
		}
	case "home", "ctrl+a":
		move = func(h, t string) (string, string) { return "", h + t }
	case "end", "ctrl+e":
		move = func(h, t string) (string, string) { return h + t, "" }
	default:
		return m, false
	}
	if f == dxWhatF {
		return m, true
	}
	return m.dxMove(f, move), true
}

// dxDropWord is s without its last word, keeping the space before it, so the
// next word typed is a new one. A word ends at any space, including a brief's
// line breaks. It is ctrl+w, and where alt+left lands.
func dxDropWord(s string) string {
	q := strings.TrimRightFunc(s, unicode.IsSpace)
	i := strings.LastIndexFunc(q, unicode.IsSpace)
	if i < 0 {
		return ""
	}
	_, n := utf8.DecodeRuneInString(q[i:])
	return q[:i+n]
}

// wordEnd is how far alt+right moves into s: past any spaces, then to the end
// of the word after them.
func wordEnd(s string) int {
	i := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsSpace(r) })
	if i < 0 {
		return len(s)
	}
	j := strings.IndexFunc(s[i:], unicode.IsSpace)
	if j < 0 {
		return len(s)
	}
	return i + j
}

// firstCluster is the first character of s as the screen draws it — a
// grapheme, so an accent or a skin tone is never left on its own.
func firstCluster(s string) string {
	if s == "" {
		return ""
	}
	c, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
	return c
}

// lastCluster is the last character of s as the screen draws it.
func lastCluster(s string) string {
	last := ""
	for i := 0; i < len(s); {
		c := firstCluster(s[i:])
		last = c
		i += max(len(c), 1)
	}
	return last
}

// dropLastCluster is s without its last character: backspace.
func dropLastCluster(s string) string {
	return s[:len(s)-len(lastCluster(s))]
}
