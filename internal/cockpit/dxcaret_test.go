package cockpit

// dxcaret_test.go covers the caret in the dx form's one-line fields: the
// arrows move it, and every edit happens where it stands.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// press feeds key messages through the model's own Update, as a terminal does.
func feed(t *testing.T, m model, msgs ...tea.KeyMsg) model {
	t.Helper()
	for _, msg := range msgs {
		mm, _ := m.Update(msg)
		m = mm.(model)
	}
	return m
}

func key(kt tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: kt} }
func alt(kt tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: kt, Alt: true} }
func text(s string) tea.KeyMsg      { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// titleModel is the form on TITLE with s already in it, caret at the end.
func titleModel(t *testing.T, s string) model {
	t.Helper()
	m := dxFormModel(t)
	m.dxField = dxTitleF
	return feed(t, m, text(s))
}

// The case it was asked for: a typo three words back is fixed where it is,
// without deleting the words after it.
func TestDXArrowsEditMidTitle(t *testing.T) {
	m := titleModel(t, "payment retyr flow")
	// Five lefts: back over " flow", to just after "retyr".
	m = feed(t, m, key(tea.KeyLeft), key(tea.KeyLeft), key(tea.KeyLeft), key(tea.KeyLeft), key(tea.KeyLeft))
	m = feed(t, m, key(tea.KeyBackspace), key(tea.KeyBackspace), text("ry"))
	if m.dxTitle != "payment retry flow" {
		t.Errorf("title = %q, want the typo fixed in place", m.dxTitle)
	}
	// Typing carries on at the caret, not at the end.
	m = feed(t, m, text("!"))
	if m.dxTitle != "payment retry! flow" {
		t.Errorf("title = %q, want the next letter at the caret", m.dxTitle)
	}
}

func TestDXHomeEndAndWords(t *testing.T) {
	m := titleModel(t, "one two three")

	m = feed(t, m, key(tea.KeyHome), text(">"))
	if m.dxTitle != ">one two three" {
		t.Errorf("home: %q", m.dxTitle)
	}
	m = feed(t, m, key(tea.KeyEnd), text("<"))
	if m.dxTitle != ">one two three<" {
		t.Errorf("end: %q", m.dxTitle)
	}
	m = feed(t, m, key(tea.KeyCtrlA), alt(tea.KeyRight), text("|"))
	if m.dxTitle != ">one| two three<" {
		t.Errorf("ctrl+a then alt+right: %q", m.dxTitle)
	}
	m = feed(t, m, key(tea.KeyCtrlE), alt(tea.KeyLeft), text("|"))
	if m.dxTitle != ">one| two |three<" {
		t.Errorf("ctrl+e then alt+left: %q", m.dxTitle)
	}
	// ctrl+arrows and alt+b/alt+f, what other terminals send for the same move.
	m = feed(t, m, key(tea.KeyCtrlLeft), key(tea.KeyCtrlLeft), text("^"))
	if m.dxTitle != ">one| ^two |three<" {
		t.Errorf("ctrl+left twice: %q", m.dxTitle)
	}
	m = feed(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true}, text("~"))
	if m.dxTitle != ">one| ~^two |three<" {
		t.Errorf("alt+b: %q", m.dxTitle)
	}
	m = feed(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f"), Alt: true}, text("$"))
	if m.dxTitle != ">one| ~^two$ |three<" {
		t.Errorf("alt+f: %q", m.dxTitle)
	}
}

// delete takes the character under the caret, ctrl+u everything before it and
// ctrl+k everything after it; ctrl+w the word before it.
func TestDXEditsActAtTheCaret(t *testing.T) {
	m := titleModel(t, "payment retry flow")
	m = feed(t, m, alt(tea.KeyLeft), alt(tea.KeyLeft)) // before "retry"

	if got := feed(t, m, key(tea.KeyDelete)).dxTitle; got != "payment etry flow" {
		t.Errorf("delete: %q", got)
	}
	if got := feed(t, m, key(tea.KeyCtrlU)).dxTitle; got != "retry flow" {
		t.Errorf("ctrl+u: %q", got)
	}
	if got := feed(t, m, key(tea.KeyCtrlK)).dxTitle; got != "payment " {
		t.Errorf("ctrl+k: %q", got)
	}
	if got := feed(t, m, key(tea.KeyCtrlW)).dxTitle; got != "retry flow" {
		t.Errorf("ctrl+w: %q", got)
	}
	// A paste lands at the caret too, folded onto the line.
	if got := feed(t, m, pasteMsg("fast\n")).dxTitle; got != "payment fast retry flow" {
		t.Errorf("paste: %q", got)
	}
	// Moving the caret is not an edit, so a held move keeps the caret where it
	// stops: delete at the end deletes nothing.
	end := feed(t, m, key(tea.KeyEnd), key(tea.KeyRight), key(tea.KeyDelete))
	if end.dxTitle != "payment retry flow" {
		t.Errorf("right past the end, then delete: %q", end.dxTitle)
	}
}

// A character the screen draws as one is stepped over and deleted as one: an
// accent or a skin tone is never left on its own.
func TestDXCaretStepsByCharacterAsDrawn(t *testing.T) {
	m := titleModel(t, "ok \U0001F44D\U0001F3FD e\u0301")
	m = feed(t, m, key(tea.KeyLeft), key(tea.KeyLeft), key(tea.KeyLeft), text("|"))
	if m.dxTitle != "ok |\U0001F44D\U0001F3FD e\u0301" {
		t.Errorf("left over an accented letter and a toned emoji: %q", m.dxTitle)
	}
	m = feed(t, m, key(tea.KeyDelete))
	if m.dxTitle != "ok | e\u0301" {
		t.Errorf("delete takes the whole emoji: %q", m.dxTitle)
	}
	m = feed(t, m, key(tea.KeyEnd), key(tea.KeyBackspace))
	if m.dxTitle != "ok | " {
		t.Errorf("backspace takes the whole accented letter: %q", m.dxTitle)
	}
}

// Leaving a field and coming back puts the caret where typing resumes.
func TestDXCaretGoesHomeWhenTheFieldIsLeft(t *testing.T) {
	m := titleModel(t, "payment retries")
	m = feed(t, m, key(tea.KeyHome), key(tea.KeyTab), key(tea.KeyShiftTab), text("!"))
	if m.dxTitle != "payment retries!" {
		t.Errorf("title = %q — back on TITLE the caret should be at the end", m.dxTitle)
	}
}

// WHAT wraps, so its caret stays at the end: the arrows do nothing there and
// typing still appends. up/down still walk the fields.
func TestDXWhatCaretStaysAtTheEnd(t *testing.T) {
	m := dxFormModel(t)
	m.dxField = dxWhatF
	m = feed(t, m, text("retry cards"), key(tea.KeyLeft), key(tea.KeyHome), text("!"))
	if m.dxWhat != "retry cards!" {
		t.Errorf("WHAT = %q", m.dxWhat)
	}
	if m = feed(t, m, key(tea.KeyDown)); m.dxField != dxGoalF {
		t.Errorf("down from WHAT went to field %d", m.dxField)
	}
}

// The switches keep left/right; WHERE's arrows move the caret in the filter
// and leave the repo cursor alone, while typing still sends it home.
func TestDXArrowsElsewhereOnTheForm(t *testing.T) {
	m := dxFormModel(t)
	m.dxField = dxModeF
	before := m.dxMode
	if m = feed(t, m, key(tea.KeyRight)); m.dxMode == before {
		t.Error("right on MODE should still cycle it")
	}

	m = dxFormModel(t)
	m = feed(t, m, text("shp"), key(tea.KeyLeft), text("o"))
	if m.dxFilter != "shop" {
		t.Errorf("filter = %q", m.dxFilter)
	}
}

// The caret is drawn where it stands: the character under it in reverse, and
// on a title far longer than its row, the text around it rather than the end.
func TestDXCaretIsDrawnWhereItStands(t *testing.T) {
	m := titleModel(t, "payment retries")
	m = feed(t, m, key(tea.KeyHome))
	row := m.dxFieldRow(80, "TITLE", dxTitleF, "")
	if !strings.Contains(row, paint(cSurface, cFg, "p")) {
		t.Errorf("the caret should stand on the first letter: %q", row)
	}
	if !strings.Contains(ansi.Strip(row), "payment retries") {
		t.Errorf("the whole title fits and should all show: %q", ansi.Strip(row))
	}

	long := titleModel(t, "START "+strings.Repeat("middle ", 60)+"END")
	atEnd := ansi.Strip(long.dxFieldRow(80, "TITLE", dxTitleF, ""))
	if !strings.Contains(atEnd, "END") || strings.Contains(atEnd, "START") {
		t.Errorf("caret at the end shows the end: %q", atEnd)
	}
	long = feed(t, long, key(tea.KeyHome))
	atHome := ansi.Strip(long.dxFieldRow(80, "TITLE", dxTitleF, ""))
	if !strings.Contains(atHome, "START") || strings.Contains(atHome, "END") {
		t.Errorf("caret at home shows the start: %q", atHome)
	}
	// The row is its gutter (pad) and 80 columns, wherever the caret stands.
	if w := dispWidth(long.dxFieldRow(80, "TITLE", dxTitleF, "")); w > 80+pad {
		t.Errorf("row is %d columns in 80+%d", w, pad)
	}
}

// The edges of the caret: a field too narrow for the character under it, a
// caret count that lands inside a character, a word move with nothing but
// spaces ahead, and a key that is neither text nor a caret key on WHAT.
func TestDXCaretEdges(t *testing.T) {
	if got := dxValue("", "\u6f22", true, 1); got != paint(cSurface, cFg, " ") {
		t.Errorf("a wide character in a one-column field should draw the caret alone: %q", got)
	}
	if got := dxValue("a", "", true, 0); got != "" {
		t.Errorf("a field with no room draws nothing: %q", got)
	}

	m := titleModel(t, "naïve")
	m.dxCaret = dxCaret{field: dxTitleF, back: 3} // between the two bytes of ï
	if h, tl := m.dxSplit(dxTitleF); h != "na" || tl != "ïve" {
		t.Errorf("split inside a character = %q|%q, want it moved to the character's start", h, tl)
	}
	if got := m.dxText(dxModeF); got != "" {
		t.Errorf("a switch has no text, got %q", got)
	}

	m = titleModel(t, "word   ")
	m = feed(t, m, key(tea.KeyHome), alt(tea.KeyRight), alt(tea.KeyRight), text("|"))
	if m.dxTitle != "word   |" {
		t.Errorf("alt+right with only spaces ahead goes to the end: %q", m.dxTitle)
	}
	m = titleModel(t, "one two")
	m = feed(t, m, key(tea.KeyHome), alt(tea.KeyRight), alt(tea.KeyRight), text("|"))
	if m.dxTitle != "one two|" {
		t.Errorf("alt+right onto the last word goes to its end: %q", m.dxTitle)
	}

	m = dxFormModel(t)
	m.dxField = dxWhatF
	if m = feed(t, m, text("brief"), key(tea.KeyF5)); m.dxWhat != "brief" {
		t.Errorf("a function key typed into WHAT: %q", m.dxWhat)
	}
}
