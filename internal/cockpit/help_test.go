package cockpit

// help_test.go pins the one thing the key sheet has to do: print what a key
// does. Reported from a 200-column terminal, where it was printing halves —
// "upgrade to the published build — be…" — with most of the screen empty,
// because the sheet capped itself at 100 columns and then truncated.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"claude-dispatcher/internal/keymap"
)

// stripAnsi removes styling so the assertions are about text. The tests run
// without a terminal, where lipgloss emits none — this keeps them honest if
// they ever run somewhere it does.
func stripAnsi(s string) string { return ansi.Strip(s) }

// helpText is the sheet as drawn, with the colour stripped and the line breaks
// removed, so a wrapped description reads as one string again.
func helpText(t *testing.T, w, h int) string {
	t.Helper()
	m := keymapFixture(t, nil)
	m.width, m.height = w, h
	flat := strings.ReplaceAll(stripAnsi(m.viewHelp(w, h)), "\n", " ")
	return strings.Join(strings.Fields(flat), " ")
}

// Every description reaches the screen whole. Checked in single-column mode,
// where flattening the sheet reassembles a wrapped sentence — at two columns
// the neighbouring block sits between a line and its continuation, so the same
// check would be reading across the gutter rather than down a column.
//
// A key sheet that cuts its own explanations is the one screen where losing
// text costs the most: it is what you open BECAUSE you do not know.
func TestHelpNeverTruncatesADescription(t *testing.T) {
	const narrow = 70 // below the two-column threshold
	body := helpText(t, narrow, 400)
	for _, b := range keymap.Defaults {
		if b.Help == "" {
			continue
		}
		want := strings.Join(strings.Fields(b.Help), " ")
		if !strings.Contains(body, want) {
			t.Errorf("%q is not printed in full:\n%s", b.Action, body)
		}
	}
	for _, sec := range helpLegend {
		for _, r := range sec.keys {
			want := strings.Join(strings.Fields(r.d), " ")
			if want != "" && !strings.Contains(body, want) {
				t.Errorf("the legend line %q is cut", r.d)
			}
		}
	}
}

// The ellipsis is the tell, and this is the check that would have caught the
// reported screenshot: at 200 columns it was full of them.
//
// It looks for an ellipsis that ENDS a run of text — which is what a cut leaves
// behind — rather than any ellipsis at all. "1…6" is a key label with a digit
// after it, and the one legitimate ellipsis the sheet prints.
func TestHelpHasNoElidedText(t *testing.T) {
	for _, w := range []int{80, 110, 140, 170, 200, 260} {
		m := keymapFixture(t, nil)
		m.width, m.height = w, 200
		for _, ln := range strings.Split(stripAnsi(m.viewHelp(w, 200)), "\n") {
			runes := []rune(ln)
			for i, r := range runes {
				if r != '…' {
					continue
				}
				if i == len(runes)-1 || runes[i+1] == ' ' {
					t.Errorf("at %d cols text is cut: %q", w, strings.TrimRight(ln, " "))
					break
				}
			}
		}
	}
}

// And it still fits the terminal it is drawn in — wrapping must not push the
// sheet off the right-hand edge, which is the other way to lose text.
func TestHelpStaysInsideItsWidth(t *testing.T) {
	for _, w := range []int{80, 110, 170, 200} {
		m := keymapFixture(t, nil)
		m.width, m.height = w, 200
		for _, ln := range strings.Split(stripAnsi(m.viewHelp(w, 200)), "\n") {
			if d := dispWidth(ln); d > w {
				t.Errorf("at %d cols a line is %d wide: %q", w, d, ln)
			}
		}
	}
}
