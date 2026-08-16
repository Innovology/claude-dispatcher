package cockpit

// dispatchx_test.go covers the split of the dx form's old WHAT into TITLE (the
// name, and the only field that reaches the branch) and WHAT (the brief, which
// wraps).

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

// dxFormModel is the dx form open over a repo list that actually has a repo in
// it, so submit gets past "nothing matches" to the field checks.
func dxFormModel(t *testing.T) model {
	t.Helper()
	m := newModel()
	m.cfg = &config.Config{Roots: []string{seedRepoRoot(t, "shop-api")}}
	m.width, m.height = 130, 40
	return m.dxOpen("")
}

// The branch comes from TITLE and nothing else. WHAT is a paragraph; before the
// split it was also the branch, which is how a five-word branch ended up being
// the only place a name could be typed.
func TestDXBranchComesFromTitleOnly(t *testing.T) {
	m := newModel()
	m.dxWhat = "rework the whole checkout so declined cards retry on a backoff"
	if got := m.dxBranch(); got != "feature/untitled" {
		t.Errorf("with no title the branch = %q, want feature/untitled", got)
	}

	m.dxTitle = "payment retries"
	if got := m.dxBranch(); got != "feature/payment-retries" {
		t.Errorf("branch = %q, want feature/payment-retries", got)
	}
	if !strings.Contains(m.dxTitleHint(), "feature/payment-retries") {
		t.Errorf("the branch preview belongs under TITLE, got %q", m.dxTitleHint())
	}
}

// The feature name is the title as typed — the record, and every screen that
// reads it, keeps the human's words. Only the slug is abbreviated.
func TestDXFeatureNameKeepsTheTitle(t *testing.T) {
	if got := dxFeatureName("  Payment Retries  "); got != "Payment Retries" {
		t.Errorf("dxFeatureName = %q, want %q", got, "Payment Retries")
	}
	// A title that cannot become a branch is no title at all, and submit says so
	// rather than letting Launch fail out of sight.
	for _, s := range []string{"", "   ", "!!! ---"} {
		if got := dxFeatureName(s); got != "" {
			t.Errorf("dxFeatureName(%q) = %q, want empty", s, got)
		}
	}
}

// Submit names the field it is waiting on, and keeps everything already typed.
func TestDXSubmitAsksForTitleThenWhat(t *testing.T) {
	m := dxFormModel(t)
	m.dxWhat = "retry declined cards"

	m, cmd := m.dxSubmit()
	if cmd != nil {
		t.Fatal("nothing should launch without a title")
	}
	if m.dxField != dxTitleF || !strings.Contains(m.notice, "branch") {
		t.Errorf("field = %d, notice = %q — submit should point at TITLE", m.dxField, m.notice)
	}
	if !m.cqDispatch || m.dxWhat != "retry declined cards" {
		t.Error("a refused submit must keep the form and what was typed")
	}

	// A title of nothing but punctuation reaches the same refusal: it slugs to
	// nothing, and Launch would have been the one to find that out.
	m.dxTitle = "!!!"
	m2, cmd := m.dxSubmit()
	if cmd != nil || m2.dxField != dxTitleF {
		t.Errorf("an unslugabble title should be refused on TITLE, field = %d", m2.dxField)
	}

	// With a title but no brief, the form asks for the brief.
	m.dxTitle, m.dxWhat = "payment retries", ""
	m3, cmd := m.dxSubmit()
	if cmd != nil {
		t.Fatal("nothing should launch without a brief")
	}
	if m3.dxField != dxWhatF || m3.notice == "" {
		t.Errorf("field = %d, notice = %q — submit should point at WHAT", m3.dxField, m3.notice)
	}
}

// The prompt leads with the title, then the brief, then the two sentences that
// are the whole of DONE WHEN and AUTO.
func TestDXPromptLeadsWithTheTitle(t *testing.T) {
	got := dxPrompt("payment retries", "retry declined cards on a backoff", "ci is green", true)
	want := strings.Join([]string{
		"payment retries",
		"",
		"retry declined cards on a backoff",
		"",
		"done when: ci is green",
		"Keep working until that is true.",
		"",
		"Commit as you go, open the PR, and fix your own CI failures without stopping to ask.",
	}, "\n")
	if got != want {
		t.Errorf("prompt =\n%s\n\nwant\n%s", got, want)
	}

	// With no brief the title stands alone rather than leaving a hole where the
	// body would be.
	if got := dxPrompt("payment retries", "", "", false); !strings.HasPrefix(got, "payment retries\n\nDo one pass") {
		t.Errorf("prompt with no WHAT =\n%s", got)
	}
}

// WHAT wraps down the value column instead of scrolling left, so a brief stays
// readable while it is being written.
func TestDXWrapValueWraps(t *testing.T) {
	lines := dxWrapValue("retry declined cards on a backoff", false, 12, 6)
	if len(lines) < 3 {
		t.Fatalf("32 characters in a 12-column field should wrap, got %d lines: %q", len(lines), lines)
	}
	for _, ln := range lines {
		if w := dispWidth(ln); w > 12 {
			t.Errorf("line %q is %d columns wide, want at most 12", ln, w)
		}
	}
	if !strings.Contains(strings.Join(lines, " "), "backoff") {
		t.Errorf("the end of the text went missing: %q", lines)
	}
}

// Past its cap the box keeps the end: the caret is where the typing is, and a
// field that scrolled the other way would hide the words being written.
func TestDXWrapValueKeepsTheCaretEnd(t *testing.T) {
	lines := dxWrapValue("alpha bravo charlie delta echo foxtrot golf hotel", true, 12, 2)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want the 2 it was capped to", len(lines))
	}
	if !strings.Contains(lines[1], "hotel") {
		t.Errorf("the last line should hold the end of the text, got %q", lines[1])
	}
	if strings.Contains(strings.Join(lines, " "), "alpha") {
		t.Errorf("the capped box should have dropped the top, got %q", lines)
	}
}

// A space is a keystroke like any other: strings.Fields would eat a trailing
// one and the caret would sit still while the human typed.
func TestDXWrapValueKeepsTrailingSpace(t *testing.T) {
	one := dxWrapValue("go", true, 20, 4)
	two := dxWrapValue("go ", true, 20, 4)
	if dispWidth(two[len(two)-1]) != dispWidth(one[len(one)-1])+1 {
		t.Errorf("the caret did not move for a trailing space: %q vs %q", one, two)
	}
}

// A word longer than the column is broken rather than left to overhang into
// whatever pane is beside it.
func TestDXWrapBreaksAnOverlongWord(t *testing.T) {
	lines := dxWrap("supercalifragilistic", 8)
	if len(lines) != 3 {
		t.Fatalf("got %d lines: %q", len(lines), lines)
	}
	for _, ln := range lines {
		if dispWidth(ln) > 8 {
			t.Errorf("line %q overruns 8 columns", ln)
		}
	}
	if strings.Join(lines, "") != "supercalifragilistic" {
		t.Errorf("breaking the word lost some of it: %q", lines)
	}
}

// The form still fits, and still shows the repo list, with a brief long enough
// to fill WHAT's box — the list is the one block here that cannot be typed back
// into view.
func TestDXViewKeepsTheRepoListUnderALongWhat(t *testing.T) {
	m := dxFormModel(t)
	m.dxTitle = "payment retries"
	m.dxWhat = strings.Repeat("retry declined cards on a backoff, ", 12)
	m.dxField = dxWhatF

	for _, h := range []int{40, 30, 24, 18} {
		out := m.dxView(120, h)
		if n := len(strings.Split(out, "\n")); n > h {
			t.Errorf("h=%d rendered %d lines", h, n)
		}
		if !strings.Contains(out, "shop-api") {
			t.Errorf("h=%d: WHAT squeezed the repo list off the screen:\n%s", h, out)
		}
		if !strings.Contains(out, "DONE WHEN") {
			t.Errorf("h=%d: the fields below WHAT went missing:\n%s", h, out)
		}
	}
}
