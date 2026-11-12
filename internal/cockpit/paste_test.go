package cockpit

// paste_test.go covers what a paste does to a field. A bracketed paste arrives
// as ONE key message carrying every rune, line breaks included, and the dx
// form's one-line fields used to take them as they came: a pasted email in
// TITLE drew as ninety rows where the form had budgeted one, pushing WHAT, the
// switches and the caret off the screen.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// pasteMsg is a bracketed paste as bubbletea delivers it.
func pasteMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

// paste feeds one bracketed paste through the model's own Update, the path a
// terminal's paste takes.
func paste(t *testing.T, m model, s string) model {
	t.Helper()
	mm, _ := m.Update(pasteMsg(s))
	return mm.(model)
}

// pastedEmail is the shape of the paste that broke the form: a quoted reply
// chain, line after line, some of them blank.
const pastedEmail = "So my son has access to his team. What else should we do??\r\n" +
	"Sent from my iPhone\r\n\r\n" +
	"> On Oct 23, 2026, at 1:52 PM, Someone wrote:\n" +
	">\n" +
	"> That's correct! Should be accepted shortly.\n" +
	">> Sincerely,\n>> Someone\n"

func TestOneLineFoldsEveryBreak(t *testing.T) {
	cases := map[string]string{
		"plain":                      "plain",
		"a\nb":                       "a b",
		"a\r\nb":                     "a b", // one break, one space
		"a\rb":                       "a b",
		"a\tb":                       "a b",
		"a\u2028b\u0085c":            "a b c",
		"red \x1b[31mtext\x1b[0m":    "red [31mtext[0m", // the escape byte goes, not the text
		"keeps  its   own  spaces\n": "keeps  its   own  spaces ",
		"naïve café":                 "naïve café",
	}
	for in, want := range cases {
		if got := oneLine(in); got != want {
			t.Errorf("oneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlockTextKeepsBreaks(t *testing.T) {
	cases := map[string]string{
		"a\r\nb":        "a\nb",
		"a\rb":          "a\nb",
		"a\n\nb":        "a\n\nb",
		"col1\tcol2":    "col1\tcol2",
		"bell\x07 gone": "bell gone",
	}
	for in, want := range cases {
		if got := blockText(in); got != want {
			t.Errorf("blockText(%q) = %q, want %q", in, got, want)
		}
	}
}

// A paste of nothing but control characters types nothing — and says so, so a
// field that resets its cursor on typing does not.
func TestTypedTextOfOnlyControlIsNothing(t *testing.T) {
	msg := pasteMsg("\x1b\x07")
	if s, ok := typedTextFor(msg, msg.String()); ok || s != "" {
		t.Errorf("typedTextFor(control only) = %q, %v; want nothing", s, ok)
	}
}

// TITLE is one line, so a paste lands on one line, and the form keeps every one
// of its rows on screen.
func TestDXPasteIntoTitleStaysOneLine(t *testing.T) {
	m := dxFormModel(t)
	m.dxField = dxTitleF
	m = paste(t, m, pastedEmail)

	if strings.ContainsAny(m.dxTitle, "\r\n") {
		t.Fatalf("TITLE took the paste's line breaks: %q", m.dxTitle)
	}
	if !strings.HasPrefix(m.dxTitle, "So my son has access") || !strings.Contains(m.dxTitle, "Someone") {
		t.Errorf("the fold lost text: %q", m.dxTitle)
	}

	const h = 40
	out := m.dxView(120, h)
	if n := len(strings.Split(out, "\n")); n > h {
		t.Errorf("rendered %d lines into %d rows:\n%s", n, h, out)
	}
	for _, field := range []string{"WHAT", "DONE WHEN", "ROOT", "MODE", "FAN OUT"} {
		if !strings.Contains(out, field) {
			t.Errorf("%s was pushed off the form by a pasted title:\n%s", field, out)
		}
	}
}

// WHAT is the brief, and a pasted brief keeps its structure — as \n, whatever
// the terminal sent — while the form still draws it inside its box.
func TestDXPasteIntoWhatKeepsItsLines(t *testing.T) {
	m := dxFormModel(t)
	m.dxField = dxWhatF
	m = paste(t, m, "fix login\r\n- retry once\r\n- then give up\r\n")

	if m.dxWhat != "fix login\n- retry once\n- then give up\n" {
		t.Errorf("WHAT = %q, want the paste's lines as \\n", m.dxWhat)
	}
	const h = 40
	if n := len(strings.Split(m.dxView(120, h), "\n")); n > h {
		t.Errorf("a multi-line brief rendered %d lines into %d rows", n, h)
	}
}

// Every other one-line field folds the same way: WHERE, DONE WHEN, ROOT and
// PREFIX share TITLE's rule.
func TestDXPasteFoldsInEveryOneLineField(t *testing.T) {
	for _, f := range []dxFieldID{dxWhereF, dxGoalF, dxRootF, dxPrefixF} {
		m := dxFormModel(t)
		m.dxField = f
		m = paste(t, m, "one\ntwo")
		got := map[dxFieldID]string{
			dxWhereF: m.dxFilter, dxGoalF: m.dxGoal, dxRootF: m.dxRoot, dxPrefixF: m.dxPrefixText(),
		}[f]
		if strings.Contains(got, "\n") || !strings.HasSuffix(got, "one two") {
			t.Errorf("field %d took %q", f, got)
		}
	}
}

// The shared inline editor — palette, reply, park, resume, adopt — is one line
// too. A pasted reply with a newline in it would otherwise be drawn as several
// rows and typed into the session as several submissions.
func TestApplyEditFoldsAPaste(t *testing.T) {
	msg := pasteMsg("first line\nsecond line")
	if next, _, _ := applyEdit("", msg.String(), msg); next != "first line second line" {
		t.Errorf("applyEdit(paste) = %q", next)
	}
}

// A title of any length draws in time linear in its length. The scroll that
// keeps the caret in view used to be quadratic: a 9,000-character title cost
// half a second a frame, every key paid it again, and esc and ctrl+c waited
// behind the backspaces — the cockpit read as frozen. At 200,000 characters the
// old loop ran for minutes; this budget is a thousand times what the linear one
// takes, so it fails on a regression and not on a slow machine.
func TestDXLongTitleDrawsQuickly(t *testing.T) {
	m := dxFormModel(t)
	m.dxField = dxTitleF
	m.dxTitle = strings.Repeat("a pasted brief that is not a title ", 200_000/35)

	start := time.Now()
	out := m.dxView(120, 40)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("one frame with a %d-character title took %s", len(m.dxTitle), took)
	}
	// And what it draws is the end, where the caret is.
	if !strings.Contains(out, "not a title") {
		t.Errorf("TITLE should show the end of its text:\n%s", out)
	}
}

// ctrl+u empties the field that has the keyboard and nothing else: the repo
// pick, the other fields and the switches stay. That is the escape from a paste
// in the wrong field that esc is not — esc closes the form with all of it.
func TestDXCtrlUClearsOnlyTheField(t *testing.T) {
	m := dxFormModel(t)
	m.dxField = dxTitleF
	m = paste(t, m, pastedEmail)
	m.dxWhat, m.dxGoal, m.dxRoot = "the brief", "ci green", "main"

	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = mm.(model)

	if m.dxTitle != "" {
		t.Errorf("ctrl+u left TITLE = %q", m.dxTitle)
	}
	if !m.cqDispatch || m.dxField != dxTitleF {
		t.Error("ctrl+u must leave the form open on the field it cleared")
	}
	if m.dxWhat != "the brief" || m.dxGoal != "ci green" || m.dxRoot != "main" {
		t.Errorf("ctrl+u reached past its field: what=%q goal=%q root=%q", m.dxWhat, m.dxGoal, m.dxRoot)
	}
	if !strings.Contains(m.dxFooterHelp(), "ctrl+u clears") {
		t.Errorf("the footer should name the key: %q", m.dxFooterHelp())
	}
}

// ctrl+w drops the last word, on every text field, and a brief's line break
// ends a word like a space does.
func TestDXCtrlWDropsTheLastWord(t *testing.T) {
	cases := map[string]string{
		"payment retries":       "payment ",
		"payment retries  ":     "payment ",
		"one":                   "",
		"":                      "",
		"fix login\n- retry":    "fix login\n- ",
		"naïve café":            "naïve ",
		"word\u00a0nbsp-joined": "word\u00a0",
	}
	for in, want := range cases {
		if got := dxDropWord(in); got != want {
			t.Errorf("dxDropWord(%q) = %q, want %q", in, got, want)
		}
	}

	m := dxFormModel(t)
	m.dxField, m.dxWhat = dxWhatF, "retry declined cards"
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := mm.(model).dxWhat; got != "retry declined " {
		t.Errorf("ctrl+w on WHAT = %q", got)
	}
}

// Clearing WHERE is a different list, so the cursor goes home; clearing PREFIX
// is "no prefix", an answer of its own, not the default coming back.
func TestDXCtrlUOnWhereAndPrefix(t *testing.T) {
	m := dxFormModel(t)
	m.dxFilter, m.dxRepo = "shop", 1
	m = m.dxEdit(dxWhereF, func(string) string { return "" })
	if m.dxFilter != "" || m.dxRepo != 0 {
		t.Errorf("WHERE cleared to %q with cursor %d", m.dxFilter, m.dxRepo)
	}

	m.dxField = dxPrefixF
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = mm.(model)
	if m.dxPrefix == nil || *m.dxPrefix != "" {
		t.Errorf("ctrl+u on PREFIX should record an empty override, got %v", m.dxPrefix)
	}
	if got := m.dxBranch(); got != "untitled" {
		t.Errorf("a cleared prefix cuts a bare branch, got %q", got)
	}
}

// A title past a name's length is refused at submit, on TITLE, before the form
// asks for WHAT — it is almost always the brief in the wrong field, and the
// refusal names both the field it belongs in and the key that clears this one.
// Nothing typed is lost.
func TestDXSubmitRefusesATitleThatIsABrief(t *testing.T) {
	m := dxFormModel(t)
	m.dxField = dxTitleF
	m = paste(t, m, pastedEmail)
	title := m.dxTitle

	m.dxField = dxGoalF
	m, cmd := m.dxSubmit()
	if cmd != nil {
		t.Fatal("a brief-length title must not launch")
	}
	if m.dxField != dxTitleF {
		t.Errorf("field = %d, want TITLE — not WHAT, which is empty because the brief is here", m.dxField)
	}
	for _, want := range []string{"characters", "WHAT", "ctrl+u"} {
		if !strings.Contains(m.notice, want) {
			t.Errorf("notice %q should mention %q", m.notice, want)
		}
	}
	if !m.cqDispatch || m.dxTitle != title {
		t.Error("a refused submit keeps the form and the title")
	}
	// The hint says so as the title grows, before any submit.
	if h := m.dxTitleHint(); !strings.Contains(h, "longer than a title") || strings.Contains(h, "branch ") {
		t.Errorf("TITLE's hint = %q", h)
	}
}

// The ceiling is the + overlay's, and a title at it still launches.
func TestDXTitleAtTheCeilingLaunches(t *testing.T) {
	got := captureLaunches(t)

	m := dxFormModel(t)
	m.dxTitle = strings.Repeat("x", featureNameMax)
	m.dxWhat = "the brief"
	m, _ = m.dxSubmit()
	if got.n != 1 || got.feature != strings.Repeat("x", featureNameMax) {
		t.Errorf("an %d-character title should launch, got %d launches of %q (notice %q)", featureNameMax, got.n, got.feature, m.notice)
	}
	if newDispatchForm(nil).feature.CharLimit != featureNameMax {
		t.Error("the two forms must share one ceiling")
	}
}
