package cockpit

// cq_derive_test.go covers the triage lens's derivation helpers — the pure
// functions that decide what each row claims and in what order the table puts
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

// The table's order is the whole point of the lens: what wants you sorts above
// what does not, then a permission ask outranks everything, then severity, then
// whoever has waited longest.
func TestFleetSortOrder(t *testing.T) {
	now := time.Now()
	q := func(feature, ask, tone string, waited time.Time) fleetRow {
		return fleetRow{
			id: feature, kind: "queue", ask: ask, tone: tone, waited: waited,
			rank: fleetRank("queue", tone, false),
		}
	}
	rows := []fleetRow{
		{id: "clean", kind: "run", rank: fleetRank("run", "normal", false), moved: now},
		q("normal-new", "needs", "normal", now.Add(-time.Minute)),
		{id: "stalled", kind: "run", rank: fleetRank("run", "amber", true), moved: now.Add(-time.Hour)},
		q("red-old", "needs", "red", now.Add(-2*time.Hour)),
		q("permission", "permission", "normal", now),
		q("normal-old", "needs", "normal", now.Add(-3*time.Hour)),
		q("amber", "needs", "amber", now.Add(-time.Minute)),
	}
	fleetSort(rows)

	var order []string
	for _, r := range rows {
		order = append(order, r.id)
	}
	want := []string{"red-old", "permission", "amber", "normal-old", "normal-new", "stalled", "clean"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("fleetSort order = %v, want %v", order, want)
		}
	}
}

// Two identical polls must produce the same table. The cursor is a position in
// this slice, so a pair of rows that swapped between them would move the
// selection under the reader's hands — straight onto a row `x` would kill.
func TestFleetSortIsTotal(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	rows := []fleetRow{
		{id: "c", kind: "queue", ask: "needs", tone: "normal", rank: 1, waited: at},
		{id: "a", kind: "queue", ask: "needs", tone: "normal", rank: 1, waited: at},
		{id: "b", kind: "run", rank: 3, moved: at},
		{id: "d", kind: "run", rank: 3, moved: at},
	}
	rev := []fleetRow{rows[3], rows[2], rows[1], rows[0]}
	fleetSort(rows)
	fleetSort(rev)
	for i := range rows {
		if rows[i].id != rev[i].id {
			t.Fatalf("two orderings of the same rows disagree: %v vs %v", rows, rev)
		}
	}
	if rows[0].id != "a" || rows[1].id != "c" {
		t.Errorf("an exact tie should break on id, got %v", rows)
	}
}

// The glyph legend in the help sheet is written against these four ranks.
func TestFleetRankNamesTheFourStates(t *testing.T) {
	cases := []struct {
		name, kind, tone string
		stalled          bool
		want             int
	}{
		{"blocking", "queue", "red", false, 0},
		{"waiting", "queue", "amber", false, 1},
		{"green and unmerged", "run", "amber", true, 2},
		{"running clean", "run", "normal", false, 3},
	}
	for _, c := range cases {
		if got := fleetRank(c.kind, c.tone, c.stalled); got != c.want {
			t.Errorf("%s: fleetRank = %d, want %d", c.name, got, c.want)
		}
	}
	// The glyph and its colour are read off the rank, never off the record's
	// status, so the two can never disagree with the legend.
	if fleetGlyph(0) != "●" || fleetGlyph(1) != "○" || fleetGlyph(2) != "◆" || fleetGlyph(3) != "·" {
		t.Error("the rank glyphs do not match the help sheet's legend")
	}
	if fleetRankColor(0) != cRed || fleetRankColor(1) != cRed ||
		fleetRankColor(2) != cAmber || fleetRankColor(3) != cFaint {
		t.Error("rank colours: tone picks the glyph, rank picks the colour")
	}
}

func TestCQToneRankAndUrgency(t *testing.T) {
	if cqToneRank("red") >= cqToneRank("amber") || cqToneRank("amber") >= cqToneRank("normal") {
		t.Errorf("tone rank must order red < amber < normal")
	}
	if cqToneRank("nonsense") != cqToneRank("normal") {
		t.Errorf("an unknown tone should rank as normal")
	}
	if cqUrgency(fleetRow{ask: "permission"}) >= cqUrgency(fleetRow{ask: "needs"}) {
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
	got := cqCtxLine(fleetRow{ctxKnown: true, ctxTokens: 118_400, model: "opus-5"})
	if got != "118k context · opus-5" {
		t.Errorf("ctx line = %q", got)
	}
	if strings.Contains(got, "%") {
		t.Error("a percentage claims a denominator nobody measured")
	}
	if got := cqCtxLine(fleetRow{ctxTokens: 5000}); got != "" {
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

// A dispatcher we have read nothing from says nothing: the panel's status tail
// drops each clause whole when its source is unavailable, rather than printing
// a zero turn count or a context window nobody measured.
func TestFleetMetaSaysNothingItCannotSource(t *testing.T) {
	if got := fleetMeta(fleetRow{}); got != "" {
		t.Errorf("bare meta = %q, want every clause omitted", got)
	}
	full := fleetMeta(fleetRow{pass: 2, ctxKnown: true, ctxTokens: 9000, model: "opus-5"})
	if full != "turn 2 · 9k context · opus-5" {
		t.Errorf("meta = %q", full)
	}
	for _, never := range []string{"%", "of 200k", "auto"} {
		if strings.Contains(full, never) {
			t.Errorf("meta %q claims %q, which nothing measures", full, never)
		}
	}
}

// The chain is our inference from the last tool used, so it must be able to say
// "we do not know" — an empty phase lights no segment at all.
func TestCQChainLightsOnlyWhatIsKnown(t *testing.T) {
	lit := func(phase string) int {
		n := 0
		for _, sg := range cqChainSegs(phase) {
			if sg.hex == cWhite {
				n++
			}
		}
		return n
	}
	if lit("") != 0 {
		t.Error("an unknown phase must light nothing")
	}
	if lit("act") != 1 {
		t.Error("a known phase lights exactly its own segment")
	}
	if got := cqPhaseWord(""); got != "—" {
		t.Errorf("the STAGE cell for an unknown phase = %q, want a dash", got)
	}
}

// The repo cell drops a product prefix the PRODUCT column two cells to the left
// already said — but only when there is something left afterwards.
func TestFleetRepoStemsOnlyTheProductPrefix(t *testing.T) {
	cases := []struct{ repo, product, want string }{
		{"cortiva-api", "cortiva", "api"},
		{"CORTIVA-api", "cortiva", "api"},
		{"cortiva", "cortiva", "cortiva"},
		{"shop-api", "cortiva", "shop-api"},
		{"api", "", "api"},
	}
	for _, c := range cases {
		if got := fleetRepo(c.repo, c.product); got != c.want {
			t.Errorf("fleetRepo(%q,%q) = %q, want %q", c.repo, c.product, got, c.want)
		}
	}
}
