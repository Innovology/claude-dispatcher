package cockpit

import (
	"strings"
	"testing"
)

const nixNotice = "resume failed: tmux new-session: error: Cannot build 'google-chrome-stable_142.0.7444.175-1_amd64.deb.drv' — cannot download it from any mirror"

// A notice too wide to sit beside the key help was dropped whole: spread clips
// the left side and throws the right one away, and the notice is the right
// one. On a laptop-width terminal that was every failure the cockpit reported
// — "resume failed: …", "launch failed: …" — while a wider screen showed them,
// which is how the same cockpit could tell the human what went wrong on one
// machine and nothing at all on another.
//
// Nor may the help give way to it: a notice stands until the next one replaces
// it, so the keys would be gone for as long — and on a row whose launch failed
// the key that reads the error is one of them. The notice takes a line of its
// own instead, and the screen stays exactly as tall as the terminal.
func TestANoticeTooWideToShareTheFooterTakesALineOfItsOwn(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)

	m := newModel()
	m.width, m.height = 120, 40
	m.notice = nixNotice

	foot := stripAnsi(m.footerView())
	lines := strings.Split(foot, "\n")
	if len(lines) != 2 {
		t.Fatalf("the footer is %d lines, want the notice above the help:\n%s", len(lines), foot)
	}
	if !strings.Contains(lines[0], "resume failed: tmux new-session") {
		t.Errorf("the notice is not on its own line: %q", lines[0])
	}
	if !strings.Contains(lines[1], strings.TrimSpace(stripAnsi(m.footerHelp()))) {
		t.Errorf("the help gave way to the notice: %q", lines[1])
	}
	for _, ln := range lines {
		if w := dispWidth(ln); w > m.width {
			t.Errorf("a footer line is %d columns on a %d-column terminal", w, m.width)
		}
	}
	if n := strings.Count(m.View(), "\n") + 1; n != m.height {
		t.Errorf("the screen is %d lines on a %d-line terminal", n, m.height)
	}
}

// A notice wider than the whole line is clipped, and says it was.
func TestANoticeWiderThanTheLineIsClipped(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)

	m := newModel()
	m.width, m.height = 80, 30
	m.notice = nixNotice

	first := strings.Split(stripAnsi(m.footerView()), "\n")[0]
	if dispWidth(first) > m.width || !strings.HasSuffix(strings.TrimSpace(first), "…") {
		t.Errorf("notice line = %q (%d columns)", first, dispWidth(first))
	}
}

// A notice is one line. One carrying a command's output — the resume error
// once held everything nix develop printed — made the footer as tall as the
// output, which pushed the body off the top of the screen on a terminal wide
// enough to show it at all. Its lines are run together, not cut to the first,
// because the first is rarely the one that says what went wrong.
func TestAMultiLineNoticeIsOneLineOfTheFooter(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)

	m := newModel()
	m.width, m.height = 200, 40
	m.notice = "resume failed: tmux new-session: building '/nix/store/x.drv'...\nerror: Cannot build '/nix/store/x.drv'.\n       Reason: builder failed with exit code 1."

	foot := stripAnsi(m.footerView())
	if n := strings.Count(foot, "\n") + 1; n > 2 {
		t.Fatalf("the footer is %d lines:\n%s", n, foot)
	}
	if !strings.Contains(foot, "resume failed") || !strings.Contains(foot, "Reason: builder failed") {
		t.Errorf("the notice is not all on one line: %q", foot)
	}
}

// With room for both, nothing changes: one line, the help on the left, the
// notice on the right.
func TestAFooterWithRoomKeepsTheHelpBesideTheNotice(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)

	m := newModel()
	m.width, m.height = 220, 40
	m.notice = "dispatched \"x\" → app"

	foot := stripAnsi(m.footerView())
	help := stripAnsi(m.footerHelp())
	if strings.Contains(foot, "\n") || !strings.Contains(foot, help) || !strings.Contains(foot, m.notice) {
		t.Errorf("the footer lost a side with room for both:\n%q", foot)
	}
}
