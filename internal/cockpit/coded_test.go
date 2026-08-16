package cockpit

// coded_test.go covers the hand-coding equivalent — the one figure in the
// cockpit that is a model rather than a reading (internal/effort).
//
// Two things are tested here that are not tested of any other figure. First,
// that it never appears without saying it is an estimate: the "≈" on triage and
// the rate-and-coverage note on velocity are load-bearing, not decoration.
// Second, that it distinguishes "we could not read the diff" from "the diff is
// empty" everywhere it is totalled — a floor presented as a total is the way
// this feature would lie.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/effort"
	"claude-dispatcher/internal/state"
)

var codedAnsi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain strips the colour so a test can assert on what a reader sees.
func plain(s string) string { return codedAnsi.ReplaceAllString(s, "") }

// ---- the row clause ---------------------------------------------------------

func TestCQCodedLineSaysNothingItCannotSource(t *testing.T) {
	// The diff could not be read at all.
	if got := cqCodedLine(fleetRow{coded: 4 * time.Hour}); got != "" {
		t.Errorf("unread diff = %q, want the clause dropped whole", got)
	}
	// The diff was read and the branch has written nothing. True, and no use
	// to anyone.
	if got := cqCodedLine(fleetRow{codedKnown: true}); got != "" {
		t.Errorf("empty diff = %q, want the clause dropped whole", got)
	}
	got := cqCodedLine(fleetRow{codedKnown: true, coded: 3*time.Hour + 40*time.Minute})
	if got != "≈3h 40m to hand-code" {
		t.Errorf("coded clause = %q", got)
	}
	// The "≈" is the whole difference between an estimate and a measurement,
	// and this is the only place on the row that says which this is.
	if !strings.HasPrefix(got, "≈") {
		t.Errorf("%q states an estimate as a reading", got)
	}
}

// The status tail is readings first and the one estimate last, so it does not
// sit among figures that were counted. Asserted with every clause populated,
// because the ordering is the only thing keeping the distinction visible.
func TestFleetMetaPutsTheEstimateLast(t *testing.T) {
	full := fleetMeta(fleetRow{
		pass: 2, ctxKnown: true, ctxTokens: 9000, model: "opus-5", mode: "auto",
		codedKnown: true, coded: 90 * time.Minute,
	})
	if full != "turn 2 · 9k context · opus-5 · auto · ≈1h 30m to hand-code" {
		t.Errorf("meta = %q", full)
	}
	// And with the readings absent it is still the last thing, not the first.
	if got := fleetMeta(fleetRow{codedKnown: true, coded: time.Hour}); got != "≈1h 00m to hand-code" {
		t.Errorf("estimate alone = %q", got)
	}
}

// ---- the fleet total --------------------------------------------------------

func TestFleetCodedCountsOnlyWhatItRead(t *testing.T) {
	rows := []fleetRow{
		{codedKnown: true, coded: 2 * time.Hour},
		{codedKnown: true, coded: 30 * time.Minute},
		// Branch deleted, or no BaseSHA recorded: its hours are unknown, not
		// zero, and adding a zero would quietly claim it wrote nothing.
		{coded: 99 * time.Hour},
	}
	if got := fleetCoded(rows); got != 2*time.Hour+30*time.Minute {
		t.Errorf("fleet total = %s, want 2h 30m", effort.Human(got))
	}
	if fleetCoded(nil) != 0 {
		t.Error("an empty table should total nothing")
	}
}

// On a terminal too narrow for the whole clause the clause goes, rather than
// being truncated into "≈10h t…" over the counts it is there to qualify.
func TestFleetHeadlineDropsTheEstimateRatherThanCutIt(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)

	rows := []fleetRow{{
		id: "a", kind: "queue", rank: 1, feature: "webhook retries",
		codedKnown: true, coded: 10 * time.Hour,
	}}
	fleet = rows

	m := newModel()
	wide := plain(m.fleetHeadline(140, rows))
	if !strings.Contains(wide, "≈10h to hand-code") {
		t.Errorf("wide headline lost the estimate: %q", wide)
	}
	narrow := plain(m.fleetHeadline(58, rows))
	if strings.Contains(narrow, "≈") || strings.Contains(narrow, "hand-code") {
		t.Errorf("narrow headline kept a clause that does not fit: %q", narrow)
	}
	if !strings.Contains(narrow, "want you") {
		t.Errorf("narrow headline lost the counts it exists for: %q", narrow)
	}
	// The estimate must never be what costs the reader the hint pinned right —
	// it is the only place `h history` is advertised.
	mid := plain(m.fleetHeadline(106, rows))
	if strings.Contains(mid, "hand-code") && !strings.Contains(mid, "h history") {
		t.Errorf("the estimate pushed the key hint off the line: %q", mid)
	}
}

// History is the one table where every row is finished, so the total is the
// whole of what the work turned out to be rather than a running count.
func TestFleetHistoryHeadlineCarriesTheTotal(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)

	rows := []fleetRow{
		{id: "a", kind: "past", rank: fleetPastRank, feature: "csv export",
			signal: "deployed", codedKnown: true, coded: 9 * time.Hour},
		// Ended without shipping, so it was never on the floor and has no diff
		// behind it. It counts as a row and contributes no hours.
		{id: "b", kind: "past", rank: fleetPastRank, feature: "spike", signal: "stopped"},
	}
	m := newModel()
	got := plain(m.fleetHistoryHeadline(140, rows))
	if !strings.Contains(got, "≈9h 00m to hand-code") {
		t.Errorf("history headline lost the total: %q", got)
	}
	if !strings.Contains(got, "2 finished") || !strings.Contains(got, "1 shipped") {
		t.Errorf("history headline lost its counts: %q", got)
	}
	// The way back must survive: the estimate is appended only when both it and
	// the right-hand hint still fit.
	if !strings.Contains(got, "h back to the fleet") {
		t.Errorf("history headline lost the way back: %q", got)
	}
	if narrow := plain(m.fleetHistoryHeadline(56, rows)); strings.Contains(narrow, "hand-code") {
		t.Errorf("narrow history headline kept a clause that does not fit: %q", narrow)
	}
}

// A finished dispatcher's row must stay the cheapest row there is: a machine
// accumulates them forever, and a diff per row on every five-second poll would
// cost the whole history. The estimate is a lookup into what collectFloor
// already read, never a diff of its own.
func TestPastRowsTakeTheEstimateWithoutReadingADiff(t *testing.T) {
	rec := &state.Dispatch{
		ID: "zzz", Feature: "csv export", RepoName: "shop-hq",
		// A path that does not exist: any git call here would fail, and a row
		// that quietly ran one would still come back with codedKnown false.
		RepoPath: filepath.Join(t.TempDir(), "not-a-repo"),
		Branch:   "feature/csv-export", BaseSHA: "deadbeef",
		Status: state.StatusDone, PRState: "MERGED",
	}
	s := &snapshot{effortBy: map[string]effort.Estimate{
		"csv export": {Dur: 4 * time.Hour, Files: 3, Lines: 200},
	}}
	got := fleetPastRow(&collectCtx{}, s, nil, rec)
	if !got.codedKnown || got.coded != 4*time.Hour {
		t.Errorf("past row = %s (known %v), want the floor's own figure",
			effort.Human(got.coded), got.codedKnown)
	}

	// And a dispatcher that ended without shipping was never on the floor, so
	// it has no entry and says nothing rather than claiming zero.
	rec.Feature = "abandoned spike"
	if got := fleetPastRow(&collectCtx{}, s, nil, rec); got.codedKnown {
		t.Error("a record with no floor entry should carry no estimate")
	}
}

// ---- the velocity lines -----------------------------------------------------

func TestVelCodedLinesAlwaysStateTheirModel(t *testing.T) {
	// Nothing shipped: the headline above already says so, and a dashed line
	// under it would add a shape and no fact.
	if fig, note := velCodedLines(0, 0, 6, 0, 0); fig != "" || note != "" {
		t.Errorf("nothing live = %q / %q, want both empty", fig, note)
	}
	// Features shipped but not one diff could be read. The honest answer is
	// silence, not a total over zero of them.
	if fig, note := velCodedLines(0, 0, 6, 0, 4); fig != "" || note != "" {
		t.Errorf("nothing measurable = %q / %q, want both empty", fig, note)
	}

	fig, note := velCodedLines(6*time.Hour, 26*time.Hour, 6, 3, 3)
	if fig != "≈6h 00m to hand-code this week · ≈26h over 6 weeks" {
		t.Errorf("figure = %q", fig)
	}
	if note != "senior developer at 50 lines/hour · from 3 live features" {
		t.Errorf("note = %q", note)
	}
	// The rate is printed from the constant, never from a copy of it that
	// could drift.
	if !strings.Contains(note, itoa(int(effort.LinesPerHour))+" lines/hour") {
		t.Errorf("note %q does not quote effort.LinesPerHour", note)
	}

	// A partial read is a floor and says so, rather than presenting itself as
	// the whole window.
	_, partial := velCodedLines(time.Hour, 8*time.Hour, 6, 2, 5)
	if !strings.Contains(partial, "2 of 5 live features measurable") {
		t.Errorf("partial coverage note = %q", partial)
	}

	// A quiet week inside a busy window states the window alone rather than
	// leading with a dash.
	quiet, _ := velCodedLines(0, 26*time.Hour, 6, 3, 3)
	if quiet != "≈26h to hand-code over 6 weeks" {
		t.Errorf("quiet week figure = %q", quiet)
	}
}

// HAND-CODED outranks the three by-week columns that have no source behind them
// and render "—" on every row, so it survives to narrower terminals than they
// do.
func TestVelColumnsKeepHandCodedOverTheEmptyOnes(t *testing.T) {
	keys := func(cols []velCol) map[string]bool {
		out := map[string]bool{}
		for _, c := range cols {
			out[c.key] = true
		}
		return out
	}
	got := keys(velColumns(48, 4))
	if !got["coded"] {
		t.Error("hand-coded should survive a narrow by-week table")
	}
	for _, empty := range []string{"fail", "restore", "first"} {
		if got[empty] {
			t.Errorf("%q outranked hand-coded, and it has no data behind it", empty)
		}
	}
	// Display order still follows velColsAll, not rank.
	cols := velColumns(200, 8)
	var order []string
	for _, c := range cols {
		order = append(order, c.key)
	}
	if len(order) < 2 || order[0] != "deploys" || order[1] != "lead" {
		t.Errorf("column order = %v, want the declared order", order)
	}
}

// Every cell width has to leave a gap, or two headers run together. "WAIT ON
// YOU" is exactly eleven characters and used to collide with the header before
// it.
func TestVelCellWidthLeavesAGapAfterTheLongestHeader(t *testing.T) {
	for _, col := range velColsAll {
		if dispWidth(col.label) >= velCellW {
			t.Errorf("header %q is %d wide in a %d cell — it will touch its neighbour",
				col.label, dispWidth(col.label), velCellW)
		}
	}
}

// ---- end to end -------------------------------------------------------------

// The whole chain, from a real branch to both lenses: numstat → effort →
// snapshot → the triage headline and the velocity output pane. It is one test
// because the point is that both lenses read ONE measurement — two figures for
// the same branch on two screens is the failure this guards.
func TestHandCodingTimeReachesTriageAndVelocity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)

	now := time.Now()
	inflight, inflightBase := codedTestRepo(t, dir, "shop-api", "feature/webhook-retries", map[string]int{
		"internal/webhook/retry.go":      300,
		"internal/webhook/retry_test.go": 150,
		// Emitted, not written: this must not show up as three days of work.
		"go.sum": 8000,
	})
	live, liveBase := codedTestRepo(t, dir, "shop-billing", "feature/seat-limits", map[string]int{
		"internal/billing/seats.go": 600,
	})

	recs := []*state.Dispatch{
		{ID: "aaa111", Feature: "webhook retries", Slug: "webhook-retries",
			RepoName: "shop-api", RepoPath: inflight, Product: "shop",
			Branch: "feature/webhook-retries", BaseSHA: inflightBase,
			Prompt: "Retry webhooks idempotently.", Status: state.StatusNeedsInput,
			StatusReason: "turn complete — waiting on you", Commits: []string{"a"},
			CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-4 * time.Minute)},
		{ID: "bbb222", Feature: "seat limits", Slug: "seat-limits",
			RepoName: "shop-billing", RepoPath: live, Product: "shop",
			Branch: "feature/seat-limits", BaseSHA: liveBase,
			Prompt: "Enforce seat limits per plan.", Status: state.StatusDone,
			StatusReason: "deployed — live", PRNumber: 9, PRState: "MERGED",
			DeployedAt: &now, PRMergedAt: &now, Commits: []string{"d"},
			CreatedAt: now.Add(-30 * time.Hour), UpdatedAt: now.Add(-time.Hour)},
	}
	for _, r := range recs {
		if err := state.Save(r); err != nil {
			t.Fatal(err)
		}
	}

	saved := captureVars()
	defer restoreVars(saved)

	cfg := &config.Config{Products: map[string][]string{"shop": {"shop-api", "shop-billing"}}}
	snap := loadSnapshot(cfg)

	// The estimate exists for both records, and the generated lockfile did not
	// dominate the in-flight one: 8000 emitted lines cost one command.
	inflightEst, ok := snap.effortBy["webhook retries"]
	if !ok {
		t.Fatal("no estimate for a branch with a readable diff")
	}
	if inflightEst.Dur > 20*time.Hour {
		t.Errorf("in-flight estimate %s — the lockfile was charged as hand-written",
			effort.Human(inflightEst.Dur))
	}
	if inflightEst.Dur < time.Hour {
		t.Errorf("in-flight estimate %s for 450 lines of source", effort.Human(inflightEst.Dur))
	}

	applySnapshot(snap)

	m := newModel()
	m.width, m.height = 190, 44
	triage := plain(press(m, "1").View())
	if !strings.Contains(triage, "to hand-code") {
		t.Errorf("triage never mentions the hand-coding time:\n%s", triage)
	}
	if !strings.Contains(triage, "≈"+effort.Human(inflightEst.Dur)+" to hand-code") {
		t.Errorf("triage does not carry the selected row's own figure (%s):\n%s",
			effort.Human(inflightEst.Dur), triage)
	}

	velocity := plain(press(m, "6").View())
	liveEst := snap.effortBy["seat limits"]
	if !strings.Contains(velocity, "≈"+effort.Human(liveEst.Dur)+" to hand-code this week") {
		t.Errorf("velocity does not price this week's live feature (%s):\n%s",
			effort.Human(liveEst.Dur), velocity)
	}
	// Velocity prices what SHIPPED. The in-flight dispatcher is bigger than the
	// live one and must not be in the figure.
	if strings.Contains(velocity, "≈"+effort.Human(inflightEst.Dur)+" to hand-code this week") {
		t.Error("velocity priced work that has not reached production")
	}
	// And the figure is never shown without the model behind it.
	if !strings.Contains(velocity, "lines/hour") {
		t.Errorf("velocity states an estimate without its model:\n%s", velocity)
	}
	if !strings.Contains(velocity, "HAND-CODED") {
		t.Errorf("the by-week table lost its hand-coded column:\n%s", velocity)
	}
}

// codedTestRepo builds a repo with a feature branch carrying `lines` lines in
// each named file, and returns the repo path and the branch's base SHA.
func codedTestRepo(t *testing.T, root, name, branch string, files map[string]int) (path, base string) {
	t.Helper()
	repo := filepath.Join(root, name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := gitCmd(t, repo, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "coded@example.com")
	run("config", "user.name", "coded")
	writeAndCommit(t, repo, "README.md", "hello\n", "initial commit")
	base = gitOutput(t, repo, "rev-parse", "HEAD")

	run("checkout", "-q", "-b", branch)
	for f, n := range files {
		full := filepath.Join(repo, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteString("// line ")
			b.WriteString(itoa(i))
			b.WriteString("\n")
		}
		if err := os.WriteFile(full, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-q", "-m", "the work")
	return repo, base
}
