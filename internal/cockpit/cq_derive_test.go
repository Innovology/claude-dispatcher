package cockpit

// cq_derive_test.go covers the command queue's derivation helpers — the pure
// functions that decide what each row claims and in what order the queue puts
// it. They are the difference between an honest screen and a confident wrong
// one, so every branch is pinned here rather than reached incidentally through
// a render test.

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
)

func TestCQWantCoversEveryKind(t *testing.T) {
	cases := map[string]string{
		"permission": "approve a permission",
		"review":     "approve a merge",
		"turn-done":  "it finished a turn",
		"idle":       "it is waiting on you",
		"needs":      "it stopped",
		"":           "it stopped",
	}
	for kind, want := range cases {
		if got := cqWant(kind); got != want {
			t.Errorf("cqWant(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestCQKindFromRecord(t *testing.T) {
	cases := []struct {
		name, st, reason, want string
	}{
		{"blocked is a permission ask", "blocked", "", "permission"},
		{"an open PR is a merge ask", "review", "turn complete — waiting on you", "review"},
		{"finished turn", "needs", "turn complete — waiting on you", "turn-done"},
		{"idle at the prompt", "needs", "waiting for your next prompt", "idle"},
		{"anything else stopped", "needs", "something odd", "needs"},
	}
	for _, c := range cases {
		if got := cqKind(&state.Dispatch{StatusReason: c.reason}, c.st); got != c.want {
			t.Errorf("%s: cqKind = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCQToneOfPrioritisesFailure(t *testing.T) {
	clash := &cqClash{path: "billing/seats.go", by: "other feature"}
	cases := []struct {
		name  string
		st    string
		ck    gh.Checks
		rv    gh.Review
		clash *cqClash
		want  string
	}{
		{"failing checks", "review", gh.Checks{Failing: 1}, gh.Review{}, nil, "red"},
		{"changes requested", "review", gh.Checks{}, gh.Review{ChangesRequested: 1}, nil, "red"},
		{"a collision", "needs", gh.Checks{}, gh.Review{}, clash, "red"},
		{"blocked", "blocked", gh.Checks{}, gh.Review{}, nil, "amber"},
		{"ordinary ask", "needs", gh.Checks{Passed: 4}, gh.Review{}, nil, "normal"},
	}
	for _, c := range cases {
		if got := cqToneOf(c.st, c.ck, c.rv, c.clash); got != c.want {
			t.Errorf("%s: cqToneOf = %q, want %q", c.name, got, c.want)
		}
	}
}

// The queue's order is the whole point of the lens: a permission ask outranks
// everything, then severity, then whoever has waited longest.
func TestCQSortOrder(t *testing.T) {
	now := time.Now()
	items := []cqItem{
		{title: "normal-new", kind: "needs", tone: "normal", waited: now.Add(-time.Minute)},
		{title: "red-old", kind: "needs", tone: "red", waited: now.Add(-2 * time.Hour)},
		{title: "permission", kind: "permission", tone: "normal", waited: now},
		{title: "normal-old", kind: "needs", tone: "normal", waited: now.Add(-3 * time.Hour)},
		{title: "amber", kind: "needs", tone: "amber", waited: now.Add(-time.Minute)},
	}
	cqSort(items)

	var order []string
	for _, it := range items {
		order = append(order, it.title)
	}
	want := []string{"permission", "red-old", "amber", "normal-old", "normal-new"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("cqSort order = %v, want %v", order, want)
		}
	}
}

func TestCQToneRankAndUrgency(t *testing.T) {
	if cqToneRank("red") >= cqToneRank("amber") || cqToneRank("amber") >= cqToneRank("normal") {
		t.Errorf("tone rank must order red < amber < normal")
	}
	if cqToneRank("nonsense") != cqToneRank("normal") {
		t.Errorf("an unknown tone should rank as normal")
	}
	if cqUrgency(cqItem{kind: "permission"}) >= cqUrgency(cqItem{kind: "needs"}) {
		t.Errorf("a permission ask must outrank everything else")
	}
}

func TestCQAgeUnits(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in   time.Time
		want string
	}{
		{time.Time{}, ""},
		{now.Add(-6 * time.Second), "6s"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{now.Add(-2 * 24 * time.Hour), "2d"},
		{now.Add(time.Minute), "0s"}, // a clock skew into the future clamps
	}
	for _, c := range cases {
		if got := cqAge(c.in); got != c.want {
			t.Errorf("cqAge(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCQCommitsReadsHonestly(t *testing.T) {
	cases := map[int]string{0: "no commits yet", 1: "1 commit", 4: "4 commits"}
	for n, want := range cases {
		if got := cqCommits(n); got != want {
			t.Errorf("cqCommits(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestCQLeadTextFallsBackHonestly(t *testing.T) {
	if got := cqLeadText(cqItem{lead: "It wants to merge."}); got != "It wants to merge." {
		t.Errorf("a real lead should win: %q", got)
	}
	if got := cqLeadText(cqItem{want: "approve a merge"}); got != "approve a merge" {
		t.Errorf("want is the first fallback: %q", got)
	}
	if got := cqLeadText(cqItem{}); got != "It stopped and is waiting on you." {
		t.Errorf("empty item should still say something true: %q", got)
	}
}

func TestCQLeadColorByTone(t *testing.T) {
	cases := map[string]string{"red": cRed, "amber": cAmber, "normal": cDim, "": cDim}
	for tone, want := range cases {
		if got := cqLeadColor(tone); got != want {
			t.Errorf("cqLeadColor(%q) = %q, want %q", tone, got, want)
		}
	}
}

func TestCQLabelUppercasesAndNamesTheUnmapped(t *testing.T) {
	if got := cqLabel("shop"); got != "SHOP" {
		t.Errorf("cqLabel = %q", got)
	}
	if got := cqLabel(""); got != "OTHER" {
		t.Errorf("an unmapped product should read OTHER, got %q", got)
	}
}

// "—" rather than "0s ago": a dispatcher whose transcript we cannot read has an
// unknown last-output time, which is not the same as a silent one.
func TestCQOutCellDistinguishesUnknownFromSilent(t *testing.T) {
	text, hex := cqOutCell("")
	if text != "—" || hex != cFaint {
		t.Errorf("unknown output = (%q,%q), want (—, faint)", text, hex)
	}
	if text, _ = cqOutCell("6s"); text != "6s ago" {
		t.Errorf("seconds = %q", text)
	}
	if _, hex = cqOutCell("4m"); hex != cMid {
		t.Errorf("a minutes-old write should not read as fresh")
	}
}

// ---- v3: the chain, the goal, the status strip and the evidence -------------

// The goal row must never present a prompt as a completion criterion: nothing
// records a goal yet, so the label says what the text actually is.
func TestCQGoalLabelsThePromptAsAPrompt(t *testing.T) {
	text, label := cqGoal(&state.Dispatch{Prompt: "Fix the retry window.\nThen add a test."})
	if label != "prompt" {
		t.Errorf("label = %q, want prompt — a brief is not a definition of done", label)
	}
	if text != "Fix the retry window." {
		t.Errorf("goal text = %q, want the prompt's first sentence", text)
	}
	if text, label = cqGoal(&state.Dispatch{}); text != "" || label != "goal" {
		t.Errorf("no prompt = (%q,%q), want an empty goal", text, label)
	}
}

// The turn count is dropped whole — separator included — when the event log has
// nothing attributed to the dispatcher.
func TestCQPassLineOmitsAnUncountedTurn(t *testing.T) {
	if got := cqPassLine(0); got != "" {
		t.Errorf("an uncounted turn = %q, want it omitted", got)
	}
	if got := cqPassLine(3); got != "turn 3" {
		t.Errorf("cqPassLine(3) = %q — it counts prompts, not repair rounds", got)
	}
}

// Context occupancy is real; the window it fills is not knowable from a model
// id, so no percentage is ever printed and an unread transcript says nothing.
func TestCQCtxLineStatesTheCountAndNeverAPercentage(t *testing.T) {
	got := cqCtxLine(cqItem{ctxKnown: true, ctxTokens: 118_400, model: "opus-5"})
	if got != "118k context · opus-5" {
		t.Errorf("ctx line = %q", got)
	}
	if strings.Contains(got, "%") {
		t.Error("a percentage claims a denominator nobody measured")
	}
	if got := cqCtxLine(cqItem{ctxTokens: 5000}); got != "" {
		t.Errorf("an unread transcript = %q, want it omitted", got)
	}
}

func TestCQShortModelTrimsTheDateStamp(t *testing.T) {
	if got := cqShortModel("claude-opus-5-20260401"); got != "opus-5" {
		t.Errorf("cqShortModel = %q", got)
	}
	if got := cqShortModel(""); got != "" {
		t.Errorf("an unknown model should stay empty, got %q", got)
	}
}

// A dispatcher we have read nothing from lights no segment at all, and the
// strip is then only the chain — every other clause omits itself.
func TestCQStatusStripSaysNothingItCannotSource(t *testing.T) {
	bare := cqStatusStrip(cqItem{}, 80)
	if dispWidth(bare) != cqChainWidth() {
		t.Errorf("bare strip is %d wide, want just the chain (%d)", dispWidth(bare), cqChainWidth())
	}
	full := cqStatusStrip(cqItem{phase: "act", pass: 2, ctxKnown: true, ctxTokens: 9000}, 80)
	for _, want := range []string{"turn 2", "9k context"} {
		if !strings.Contains(full, want) {
			t.Errorf("strip %q is missing %q", full, want)
		}
	}
}

// The evidence pane takes no more rows than it has lines: in a terminal the
// surplus is black, and the queue behind the ask is a better use for it.
func TestCQSplitPanesDonatesUnusedEvidenceRows(t *testing.T) {
	if ev, q := cqSplitPanes(20, 40); ev != 15 || q != 5 {
		t.Errorf("long evidence split = (%d,%d), want the design's 3:1", ev, q)
	}
	if ev, q := cqSplitPanes(20, 3); ev != 3 || q != 17 {
		t.Errorf("short evidence split = (%d,%d), want the surplus donated", ev, q)
	}
	if ev, q := cqSplitPanes(0, 10); ev != 0 || q != 0 {
		t.Errorf("no budget = (%d,%d)", ev, q)
	}
}

// The excerpt is a real `git diff` over the dispatch's own provenance range,
// with the headers nobody reads dropped and the signs kept for colouring.
func TestCQDiffLinesReadsTheRealDiff(t *testing.T) {
	repo := newTestGitRepo(t, "evidence")
	writeAndCommit(t, repo, "retry.go", "package retry\n\nconst window = 7\n", "base")
	base := gitOutput(t, repo, "rev-parse", "HEAD")
	if out, err := gitCmd(t, repo, "checkout", "-q", "-b", "feature/retries").CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}
	writeAndCommit(t, repo, "retry.go", "package retry\n\nconst window = 14\n", "widen")

	rec := &state.Dispatch{RepoPath: repo, BaseSHA: base, Branch: "feature/retries"}
	lines := cqDiffLines(rec, "retry.go")
	if len(lines) == 0 {
		t.Fatal("expected hunk lines from a real diff")
	}
	var added, removed bool
	for _, l := range lines {
		if strings.HasPrefix(l.text, "diff --git") || strings.HasPrefix(l.text, "index ") {
			t.Errorf("header line survived: %q", l.text)
		}
		if l.sign == "+" && strings.Contains(l.text, "window = 14") {
			added = true
		}
		if l.sign == "-" && strings.Contains(l.text, "window = 7") {
			removed = true
		}
	}
	if !added || !removed {
		t.Errorf("diff lines = %+v, want the change both ways round", lines)
	}
	// A record that never branched has no range to diff, and says so by
	// producing nothing rather than by diffing the working tree.
	if got := cqDiffLines(&state.Dispatch{RepoPath: repo}, "retry.go"); got != nil {
		t.Errorf("no base sha should yield no evidence, got %+v", got)
	}
}

// Tool markers are not something a dispatcher told you, so they never reach the
// pane as its "last output".
func TestCQSaidLinesDropsToolMarkers(t *testing.T) {
	got := cqSaidLines([]string{"⚙ Bash", "Ran the suite.", "  ", "⚙ Edit", "Two tests fail."})
	if len(got) != 2 || got[0].text != "Ran the suite." || got[1].text != "Two tests fail." {
		t.Errorf("said lines = %+v", got)
	}
	for _, l := range got {
		if l.sign != " " {
			t.Errorf("a spoken line has no diff sign, got %q", l.sign)
		}
	}
}
