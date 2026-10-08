package cockpit

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/gh"
)

// A repo that names no gate must read exactly as it always has. The gate is an
// opt-in, and an opt-in that changes the default is not one: every repo in a
// portfolio keeps the old cell until its own name appears under [gates].
func TestRepoCellWithoutAGateIsUnchanged(t *testing.T) {
	cases := []struct {
		name   string
		checks map[int]gh.Checks
		text   string
		color  string
	}{
		{"anything red anywhere", map[int]gh.Checks{1: {Total: 4, Passed: 3, Failing: 1}, 2: {Total: 2, Passed: 2}}, "✗ failing", cRed},
		{"nothing red, something running", map[int]gh.Checks{1: {Total: 3, Passed: 2, Running: 1}}, "● deploying", cBlue},
		{"all green", map[int]gh.Checks{1: {Total: 2, Passed: 2}}, "✓ green", cGreen},
		{"nothing to go on", map[int]gh.Checks{}, "—", cDim},
	}
	for _, tc := range cases {
		text, color := prodCICell(tc.checks)
		if text != tc.text || color != tc.color {
			t.Errorf("%s: cell = %q/%s, want %q/%s", tc.name, text, color, tc.text, tc.color)
		}
	}
}

// The defect, as reported: player-app's row said "✗ failing" while its trunk
// was green and a pull request sat ready to merge. Its gate's own numbers,
// measured 2026-10-08 over 7 open pull requests: 1 SUCCESS, 2 FAILURE, 4 with
// no gate entry at all. What the row owes the human is the one that is ready.
func TestGateCellLeadsWithWhatIsReady(t *testing.T) {
	playerApp := []string{"SUCCESS", "FAILURE", "FAILURE", "", "", "", ""}
	text, tail, color := prodGateCell(playerApp)
	if text != "1 ready" || tail != " · 2 red" || color != cAmber {
		t.Errorf("cell = %q + %q / %s, want \"1 ready\" + \" · 2 red\" / amber", text, tail, color)
	}

	// A pull request whose rollup carries no gate result is counted in nothing:
	// the umbrella skips its aggregator on a targeted or dispatch run, and the
	// four here are the majority of the repo's open pull requests. A rule that
	// read them as anything would have this cell swing on how the run was
	// started.
	if text, tail, _ := prodGateCell([]string{"SUCCESS", "", "", ""}); text != "1 ready" || tail != "" {
		t.Errorf("no-result PRs leaked into the counts: %q + %q", text, tail)
	}
	// PENDING is not a verdict either. A gate still running has said nothing.
	if text, tail, _ := prodGateCell([]string{"PENDING", "PENDING"}); text != "—" || tail != "" {
		t.Errorf("a running gate was read as a verdict: %q + %q", text, tail)
	}
	// Red with nothing ready leads on its own: amber is a claim about the
	// human, and "0 ready" is a claim about nobody.
	if text, tail, color := prodGateCell([]string{"FAILURE", "FAILURE", ""}); text != "2 red" || tail != "" || color != cRed {
		t.Errorf("red-only cell = %q + %q / %s, want \"2 red\" / red", text, tail, color)
	}
}

// A gate configured and no pull request carrying it is the absence this cockpit
// spells "—": we could not see. Not green (we have no evidence anything passed)
// and not red (we have none that anything failed) — the same rule the checks
// column, the forge columns and the effort figures all keep.
func TestGateCellWithNoGateResultSaysNothing(t *testing.T) {
	for _, states := range [][]string{nil, {}, {"", "", ""}, {"CANCELLED", "SKIPPED"}} {
		text, tail, color := prodGateCell(states)
		if text != "—" || tail != "" || color != cDim {
			t.Errorf("states %v: cell = %q + %q / %s, want \"—\"/dim", states, text, tail, color)
		}
	}
}

// The verdict cell is a fixed-width column with the repo name flexing beside
// it, so the two-colour version has to occupy the width the one-colour version
// did — otherwise naming a gate shifts every cell to its left. The narrow
// breakpoint is where it would show: 21 columns in the product panel.
func TestGateCellKeepsItsColumnWidth(t *testing.T) {
	for _, w := range []int{21, 22} {
		solid := row(40, "", flexc("player-app", cFg), cr("✗ failing", w, cRed))
		gated := row(40, "", append([]seg{flexc("player-app", cFg)},
			crTail("1 ready", " · 2 red", w, cAmber, cDim)...)...)
		if dispWidth(solid) != dispWidth(gated) {
			t.Errorf("w=%d: gated row is %d columns, solid is %d", w, dispWidth(gated), dispWidth(solid))
		}
		if !strings.Contains(plain(gated), "1 ready · 2 red") {
			t.Errorf("w=%d: the clause was clipped: %q", w, plain(gated))
		}
	}
	// Three figures each still fit the narrower of the two columns, which is
	// the most a repo can plausibly carry: 50 is gh's own page size here.
	if got := dispWidth("999 ready · 999 red"); got > 21 {
		t.Errorf("the widest cell is %d columns, the column is 21", got)
	}
}
