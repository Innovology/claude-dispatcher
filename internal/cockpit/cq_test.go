package cockpit

// cq_test.go covers the triage lens's key state machine and its two render
// modes. Like fixtures_test.go it installs its own data and restores it, so
// nothing here depends on what another test left in the package vars.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// installFleetFixture puts two asks and one running dispatcher on the triage
// lens, already in the collector's rank order.
func installFleetFixture(t *testing.T) {
	t.Helper()
	prevFleet, prevOut := fleet, cqLastOutput
	t.Cleanup(func() { fleet, cqLastOutput = prevFleet, prevOut })

	now := time.Now()
	fleet = []fleetRow{
		{
			id: "id-one", kind: "queue", rank: 1, ask: "permission", product: "alpha",
			feature: "one", repo: "alpha-api", ref: "#1", stage: "act", pass: 2,
			signal: "approve a permission", tone: "amber",
			why: "It is blocked and waiting on you.",
			acts: []cqAct{
				{k: "⏎", d: "attach", ok: "attaching to alpha-api session…", keep: true},
				{k: "x", d: "kill", ok: "killed \"one\""},
				{k: "s", d: "skip"},
			},
			moved: now.Add(-4 * time.Minute), waited: now.Add(-4 * time.Minute),
		},
		{
			id: "id-two", kind: "queue", rank: 1, ask: "turn-done", product: "beta",
			feature: "two", repo: "beta-svc", ref: "feature/two", stage: "ship", pass: 1,
			signal: "it finished a turn", tone: "normal",
			why: "Done — want me to open a PR?",
			acts: []cqAct{
				{k: "⏎", d: "attach", ok: "attaching to beta-svc session…", keep: true},
				{k: "y", d: "mark shipped", ok: "\"two\" marked shipped"},
				{k: "s", d: "skip"},
			},
			moved: now.Add(-time.Hour), waited: now.Add(-time.Hour),
		},
		{
			id: "id-three", kind: "run", rank: 3, product: "alpha",
			feature: "three", repo: "alpha-web", stage: "observe", pass: 2,
			tone: "normal",
			acts: []cqAct{
				{k: "⏎", d: "attach", ok: "attaching to alpha-web session…", keep: true},
				{k: "x", d: "kill", ok: "killed \"three\""},
			},
			moved: now.Add(-6 * time.Second), waited: now.Add(-6 * time.Second),
		},
	}
	cqLastOutput = "6s"
}

func cqModel(t *testing.T) model {
	t.Helper()
	installFleetFixture(t)
	m := newModel()
	m.width, m.height = 130, 40
	return m
}

func fleetFeatures(m model) string {
	var out []string
	for _, r := range m.fleetRows() {
		out = append(out, r.feature)
	}
	return strings.Join(out, ",")
}

// `s` sends the row under the cursor to the back and brings the next one up
// under it. A running dispatcher is not asking for anything, so there is
// nothing to skip past.
func TestCQSkipRotates(t *testing.T) {
	m := cqModel(t)
	if got := fleetFeatures(m); got != "one,two,three" {
		t.Fatalf("fleet = %s", got)
	}
	m = press(m, "s")
	if got := fleetFeatures(m); got != "two,three,one" {
		t.Errorf("after skip fleet = %s, want two,three,one", got)
	}
	if r, _ := m.fleetSel(); r.feature != "two" {
		t.Errorf("the cursor should stay put and take the next row, got %q", r.feature)
	}
	// Down onto the running row: skip is not offered there and must not reorder.
	m = press(m, "k") // already at the top
	m = press(m, "j")
	m = press(m, "s")
	if got := fleetFeatures(m); got != "two,three,one" {
		t.Errorf("skipping a running row reordered the table: %s", got)
	}
}

// An act flashes for cqFlashLinger, swallows every key while it does, then
// clears the row it was fired on and leaves an undo behind.
func TestCQActFlashClearsAndUndoes(t *testing.T) {
	m := cqModel(t)

	next, cmd := m.handleKey("x")
	m = next.(model)
	if cmd == nil {
		t.Fatal("an act should return the command that does the work")
	}
	if m.cqFlash != "killed \"one\"" {
		t.Fatalf("cqFlash = %q", m.cqFlash)
	}

	// Everything is swallowed mid-flash, including the escape keys.
	for _, k := range []string{"s", "d", "w", "f", "2", "?"} {
		mm := press(m, k)
		if mm.cqFlash == "" || mm.lens != "floor" || mm.cqDispatch || mm.cqFilter != "" {
			t.Fatalf("%q was not swallowed during the flash", k)
		}
	}

	// A superseded tick must not clear a newer flash.
	stale, _ := m.Update(cqFlashMsg{seq: m.cqFlashSeq - 1})
	if stale.(model).cqFlash == "" {
		t.Error("a stale cqFlashMsg cleared the flash")
	}

	done, _ := m.Update(cqFlashMsg{seq: m.cqFlashSeq})
	m = done.(model)
	if m.cqFlash != "" {
		t.Error("the flash should end on its own tick")
	}
	if got := fleetFeatures(m); got != "two,three" {
		t.Errorf("fleet after the act = %s, want two,three", got)
	}
	if m.cqCleared != 1 || m.cqUndo == nil {
		t.Fatalf("cleared=%d undo=%+v", m.cqCleared, m.cqUndo)
	}

	m = press(m, "u")
	if got := fleetFeatures(m); got != "one,two,three" {
		t.Errorf("undo should put the row back at the front, got %s", got)
	}
	if m.cqCleared != 0 || m.cqUndo != nil {
		t.Errorf("undo should retract the handled count: cleared=%d undo=%+v", m.cqCleared, m.cqUndo)
	}
}

// A keep act (attach) reports without clearing anything.
func TestCQKeepActLeavesTheItem(t *testing.T) {
	m := cqModel(t)
	next, _ := m.handleKey("enter")
	m = next.(model)
	if !m.cqFlashKeep {
		t.Fatal("attach is a keep act")
	}
	done, _ := m.Update(cqFlashMsg{seq: m.cqFlashSeq})
	m = done.(model)
	if got := fleetFeatures(m); got != "one,two,three" {
		t.Errorf("a keep act must not clear the row, got %s", got)
	}
	if m.cqCleared != 0 || m.cqUndo != nil {
		t.Error("a keep act should not record an undo")
	}
}

// j/k walk the table and stop at both ends; g/G jump to them. The cursor is
// re-keyed to the row it lands on, so a rebuild cannot slide it elsewhere.
func TestFleetCursorMoves(t *testing.T) {
	m := cqModel(t)
	m = press(m, "j")
	if r, _ := m.fleetSel(); r.feature != "two" || m.fleetSelID != "id-two" {
		t.Fatalf("j = %+v / %q", r.feature, m.fleetSelID)
	}
	for i := 0; i < 10; i++ {
		m = press(m, "j")
	}
	if r, _ := m.fleetSel(); r.feature != "three" {
		t.Errorf("j should stop on the last row, got %q", r.feature)
	}
	for i := 0; i < 10; i++ {
		m = press(m, "k")
	}
	if r, _ := m.fleetSel(); r.feature != "one" {
		t.Errorf("k should stop on the first row, got %q", r.feature)
	}
	if r, _ := press(m, "G").fleetSel(); r.feature != "three" {
		t.Errorf("G = %q, want the last row", r.feature)
	}
	if r, _ := press(press(m, "G"), "g").fleetSel(); r.feature != "one" {
		t.Errorf("g = %q, want the first row", r.feature)
	}
}

// The design never reloads; this cockpit rebuilds the table on every poll. The
// cursor follows its row by id, because holding it by index would move the
// selection under the reader's hands the moment a rank changed — onto a row `x`
// would kill.
func TestFleetCursorFollowsItsRowAcrossARebuild(t *testing.T) {
	m := cqModel(t)
	m = press(m, "j")
	m = press(m, "j") // on "three", index 2
	if m.fleetSelID != "id-three" {
		t.Fatalf("precondition: selected %q", m.fleetSelID)
	}

	// A new blocker arrives and ranks above everything.
	fleet = append([]fleetRow{{
		id: "id-zero", kind: "queue", rank: 0, ask: "permission",
		feature: "zero", repo: "alpha-api", tone: "red",
	}}, fleet...)

	m = m.fleetSync()
	if m.fleetCursor != 3 {
		t.Errorf("cursor = %d, want it to have followed its row to index 3", m.fleetCursor)
	}
	if r, _ := m.fleetSel(); r.feature != "three" {
		t.Errorf("the selection moved to %q", r.feature)
	}

	// When the row genuinely leaves, the index is the fallback and it clamps.
	fleet = fleet[:2]
	m = m.fleetSync()
	if m.fleetCursor != 1 || m.fleetSelID != "id-one" {
		t.Errorf("a departed row should clamp to the end: cursor=%d id=%q", m.fleetCursor, m.fleetSelID)
	}
}

// `f` walks the four filters and comes back round; each one narrows to a real
// question, and the cursor starts again at the top of what is left.
func TestFleetFilterCycles(t *testing.T) {
	m := cqModel(t)
	want := []struct{ filter, rows string }{
		{"wants you", "one,two"},
		{"needs a look", "one,two"},
		{"running", "three"},
		{"all", "one,two,three"},
	}
	m = press(m, "j") // move off the top so the reset is visible
	for _, c := range want {
		m = press(m, "f")
		if m.fleetFilter() != c.filter {
			t.Fatalf("f = %q, want %q", m.fleetFilter(), c.filter)
		}
		if got := fleetFeatures(m); got != c.rows {
			t.Errorf("%s shows %s, want %s", c.filter, got, c.rows)
		}
		if m.fleetCursor != 0 {
			t.Errorf("%s should start again at the top, cursor = %d", c.filter, m.fleetCursor)
		}
	}
}

// A filter that matches nothing must show the table saying so. Gating the empty
// state on the filtered count — as the design does — dropped the human into the
// dispatch form instead, where that line can never appear.
func TestFleetEmptyFilterStaysOnTheTable(t *testing.T) {
	m := cqModel(t)
	fleet = fleet[:2] // queue rows only
	m = m.fleetSetFilter("running")
	if len(m.fleetRows()) != 0 {
		t.Fatalf("precondition: %d rows match", len(m.fleetRows()))
	}
	out := m.viewCQ(m.width, 30)
	if !strings.Contains(out, "nothing matches this filter") {
		t.Errorf("an empty filter should say so on the table, got:\n%s", out)
	}
	if strings.Contains(out, "WHERE") {
		t.Error("an empty filter must not drop the human into the dispatch form")
	}
}

// `d` opens the dispatch form over a full table; an untouched form still
// navigates, and a touched one takes every key as text.
func TestCQFormFallThrough(t *testing.T) {
	m := cqModel(t)
	m = press(m, "d")
	if !m.cqDispatch || m.dxTouched() || !m.dxAuto {
		t.Fatalf("d should open an empty form, auto on: %v %v %v", m.cqDispatch, m.dxTouched(), m.dxAuto)
	}

	// Untouched: a lens digit still navigates rather than typing itself.
	m2 := press(m, "3")
	if m2.lens != "backlog" {
		t.Fatalf("a digit should leave an untouched form, lens = %q", m2.lens)
	}

	// Once anything is typed, those same keys are letters again. The form opens
	// on WHERE, so they land in the repo filter.
	m = press(m, "d")
	m = press(m, "a")
	m = press(m, "w")
	if m.dxFilter != "aw" || !m.cqDispatch {
		t.Fatalf("a touched form should take w as text: %q open=%v", m.dxFilter, m.cqDispatch)
	}
	m = press(m, "backspace")
	if m.dxFilter != "a" {
		t.Errorf("backspace: %q", m.dxFilter)
	}
}

// Enter walks the four fields and only submits from the last one; esc abandons
// the whole form.
func TestCQFormEnterWalksFieldsAndEscClears(t *testing.T) {
	m := cqModel(t)
	m = press(m, "d")
	for _, want := range []dxFieldID{dxWhatF, dxGoalF, dxAutoF} {
		m = press(m, "enter")
		if m.dxField != want {
			t.Fatalf("enter should advance to field %d, got %d", want, m.dxField)
		}
	}
	// On AUTO, space is the switch rather than a character.
	m = press(m, "space")
	if m.dxAuto {
		t.Error("space on AUTO should turn it off")
	}

	m = press(m, "esc")
	if m.cqDispatch || m.dxTouched() || m.dxField != dxWhereF {
		t.Error("esc should abandon the form and reset every field")
	}
}

// Submitting with no repo to land in says so and keeps what was typed: the form
// is the only copy of the sentence.
func TestCQFormSubmitWithoutARepo(t *testing.T) {
	m := cqModel(t)
	m = press(m, "d")
	m = press(m, "tab") // → WHAT
	for _, k := range strings.Split("fix", "") {
		m = press(m, k)
	}
	next, cmd := m.handleKey("ctrl+d")
	m = next.(model)
	if cmd != nil {
		t.Error("nothing should launch when no repo matches")
	}
	if m.notice != "nothing matches — widen the filter" {
		t.Errorf("notice = %q", m.notice)
	}
	if !m.cqDispatch || m.dxWhat != "fix" {
		t.Error("a failed submit must keep the form and what was typed")
	}
}

// The dispatcher must be told what was typed, not what the branch is called.
//
// WHAT is read twice — as a name, which is capped to five slug words because it
// names the branch, the worktree and the tmux session, and as the brief, which
// is the only description of the work that ever reaches the session. Submit
// composed the brief from the capped name, so every word past the fifth was
// dropped at the form: before the record, before claude started, and with no
// other copy of the sentence anywhere. Real records carry the prompt
// "dispatching immediate look up when", cut mid-clause.
func TestCQFormDispatchesTheSentenceNotTheName(t *testing.T) {
	const what = "dispatching immediate look up when the branch already has a dispatcher"

	var gotFeature, gotPrompt string
	prev := dxLaunch
	dxLaunch = func(_ *config.Config, _, feature, prompt string) tea.Cmd {
		gotFeature, gotPrompt = feature, prompt
		return nil
	}
	t.Cleanup(func() { dxLaunch = prev })

	m := cqModel(t)
	m.cfg = &config.Config{Roots: []string{seedRepoRoot(t, "alpha-api")}}
	m = press(m, "d")
	m.dxField, m.dxWhat, m.dxGoal = dxWhatF, what, "the pr is merged"
	m, _ = m.dxSubmit()

	// The name is still the branch's own words: that is what keeps the record,
	// the branch, the worktree and the session saying one thing.
	if want := "dispatching immediate look up when"; gotFeature != want {
		t.Errorf("feature = %q, want %q", gotFeature, want)
	}
	// The brief is not.
	if first := strings.SplitN(gotPrompt, "\n", 2)[0]; first != what {
		t.Errorf("prompt line 1 = %q\nwant %q", first, what)
	}
	if !strings.Contains(gotPrompt, "done when: the pr is merged") {
		t.Errorf("prompt lost DONE WHEN:\n%s", gotPrompt)
	}
}

// dxDispatch is the seam the two readings part at: cap the name, never the
// brief. A sentence inside the cap must come back whole from both.
func TestDXDispatchCapsTheNameOnly(t *testing.T) {
	long := "rebuild the deploy watcher so it stops polling a workflow that already finished"
	feature, prompt := dxDispatch("  "+long+"  ", "", true)
	if words := strings.Fields(feature); len(words) != 5 {
		t.Errorf("feature = %q, want 5 words", feature)
	}
	if !strings.HasPrefix(prompt, long+"\n") {
		t.Errorf("prompt = %q, want it to open with the whole sentence", prompt)
	}

	// Short enough to survive the cap: both readings agree, and neither invents
	// punctuation or drops a word.
	feature, prompt = dxDispatch("retry backoff", "", true)
	if feature != "retry backoff" || !strings.HasPrefix(prompt, "retry backoff\n") {
		t.Errorf("short WHAT: feature = %q, prompt = %q", feature, prompt)
	}

	// Nothing typed is still nothing: submit's "say what it should do" test
	// reads the feature, so it must stay empty rather than become whitespace.
	if feature, _ := dxDispatch("   ", "", true); feature != "" {
		t.Errorf("blank WHAT gave feature %q", feature)
	}
}

// Switching lens leaves the lens's modes behind.
func TestCQLensDigitResetsModes(t *testing.T) {
	// A form with text in it keeps the digit — you are typing a filter.
	m := cqModel(t)
	m = press(m, "d")
	m = press(m, "x")
	m = press(m, "2")
	if m.lens != "floor" || m.dxFilter != "x2" {
		t.Errorf("a digit in a touched form is text: lens=%q filter=%q", m.lens, m.dxFilter)
	}

	// An untouched one does not trap it, and leaving resets the form.
	m = press(m, "backspace")
	m = press(m, "backspace")
	m = press(m, "3")
	if m.lens != "backlog" || m.cqDispatch || m.dxTouched() {
		t.Errorf("an untouched form should let the digit through: lens=%q filter=%q", m.lens, m.dxFilter)
	}
}

// A refresh that drops a row must drop it from the order, the suppressed set
// and the undo — otherwise cqSuppressed grows without bound.
func TestCQReconcileForgetsDepartedItems(t *testing.T) {
	m := cqModel(t)
	m = press(m, "s") // seeds the order with every id
	m.cqSuppressed["id-one"] = true
	m.cqUndo = &cqUndoEntry{id: "id-one", label: "killed \"one\""}

	fleet = fleet[1:] // id-one's record left the fleet for real
	m = m.cqReconcile()

	if len(m.cqOrder) != 2 || m.cqOrder[0] != "id-two" {
		t.Errorf("cqOrder = %v", m.cqOrder)
	}
	if m.cqSuppressed["id-one"] {
		t.Error("a departed id should leave the suppressed set")
	}
	if m.cqUndo != nil {
		t.Error("undo should be dropped once there is nothing to put back")
	}
}

// The footer's row verbs come from the row under the cursor, so they change
// with it — and it never advertises a key the handler does not have.
func TestCQFooterHelpFollowsTheCursor(t *testing.T) {
	m := cqModel(t)
	got := m.footerHelp()
	if !strings.Contains(got, "x kill") || !strings.Contains(got, "d dispatch") ||
		!strings.Contains(got, "f filter") {
		t.Errorf("fleet help = %q", got)
	}
	if strings.Contains(got, "F follow") {
		t.Error("nothing here follows a session without attaching")
	}

	// On the running row there is nothing to skip and nothing to approve.
	onRun := press(press(m, "j"), "j").footerHelp()
	if strings.Contains(onRun, "s skip") || strings.Contains(onRun, "y ") {
		t.Errorf("a running row's verbs = %q", onRun)
	}

	md := press(m, "d")
	// An untouched form advertises the exits it still has. `w` used to be one
	// of them; it is gone, so the footer must not name it.
	formHelp := md.footerHelp()
	if !strings.Contains(formHelp, "esc cancel") || !strings.Contains(formHelp, "1…6 sections") {
		t.Errorf("untouched-form help = %q", formHelp)
	}
	if strings.Contains(formHelp, "w running") {
		t.Errorf("footer still advertises the removed w key: %q", formHelp)
	}
	// A touched one drops them — they are letters now — and offers ctrl+d,
	// which is the key that actually submits (ctrl+⏎ is not reportable).
	if got := press(md, "a").footerHelp(); !strings.Contains(got, "ctrl+d dispatch") {
		t.Errorf("touched-form help = %q", got)
	}
}

// Four columns never shed: how bad, what, why and how long. Everything else
// gives way as the terminal narrows, in the order the fit() tiers set.
func TestFleetColumnsShedByWidth(t *testing.T) {
	for _, w := range []int{60, 80, 110, 176} {
		cols := fleetColumns(w)
		if cols.glyph == 0 || cols.feature < 1 || cols.age == 0 {
			t.Errorf("@%d: a load-bearing column was shed: %+v", w, cols)
		}
		if (w >= 70) != (cols.product > 0) {
			t.Errorf("@%d: product = %d", w, cols.product)
		}
		if (w >= 110) != (cols.repo > 0) || (w >= 110) != (cols.stage > 0) {
			t.Errorf("@%d: repo = %d stage = %d", w, cols.repo, cols.stage)
		}
		// The signal cell is the flex, so it takes whatever the fixed cells
		// leave — the row must still be exactly w columns wide.
		line := fleetLine(w, cols, cTransparent, [flCells]string{}, [flCells]string{})
		if dispWidth(line) != w {
			t.Errorf("@%d: a table line is %d columns wide", w, dispWidth(line))
		}
	}
}

// Every mode renders inside its box at every width tier, including mid-flash
// and under each filter.
func TestCQRendersInEveryMode(t *testing.T) {
	installFleetFixture(t)
	for _, w := range smokeWidths {
		for _, h := range []int{44, 20, 10} {
			base := newModel()
			base.width, base.height = w, h
			cases := map[string]model{
				"fleet": base,
				"draft": press(press(base, "d"), "a"),
			}
			for _, f := range fleetFilters {
				fm := base
				fm.cqFilter = f
				cases["filter "+f] = fm
			}
			flashing := base
			flashing.cqFlash = "killed \"one\""
			cases["flash"] = flashing
			for name, m := range cases {
				renderClean(t, m, name+" @"+itoa(w)+"x"+itoa(h))
			}
		}
	}
}

// TestCQIncludesReviewItems guards the design's headline ask. A finished turn
// with an open, undeployed PR is floorState "review" — "it wants to squash-merge
// into main". The collector's switch once handled only blocked/needs/working, so
// those dispatchers appeared nowhere on the lens with a merge sitting there
// waiting.
func TestCQIncludesReviewItems(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	now := time.Now()
	rec := &state.Dispatch{
		ID: "rev1", Feature: "seat limits", RepoName: "shop-api", RepoPath: dir,
		Status: state.StatusNeedsInput, StatusReason: "turn complete — waiting on you",
		PRNumber: 151, PRState: "OPEN", Commits: []string{"a", "b", "c"},
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-4 * time.Minute),
	}
	if err := state.Save(rec); err != nil {
		t.Fatal(err)
	}
	if got := floorState(rec); got != "review" {
		t.Fatalf("precondition: floorState = %q, want review", got)
	}

	saved := captureVars()
	defer restoreVars(saved)
	s := loadSnapshot(&config.Config{})

	var found *fleetRow
	for i := range s.fleet {
		if s.fleet[i].feature == "seat limits" {
			found = &s.fleet[i]
		}
	}
	if found == nil {
		t.Fatal("a review dispatcher is missing from the fleet entirely")
	}
	if found.signal != "approve a merge" {
		t.Errorf("signal = %q, want %q", found.signal, "approve a merge")
	}
	var keys []string
	for _, a := range found.acts {
		keys = append(keys, a.k)
	}
	if !strings.Contains(strings.Join(keys, ""), "y") {
		t.Errorf("review row offers no merge act: keys %v", keys)
	}
}

// TestDispatchKeyIsIdempotent guards the key the footer advertises as "d
// dispatch". The form's untouched-fall-through set left `d` out, so pressing it
// while the form was already up typed a letter into the repo filter. With
// nothing in flight the form is up by default, which made `d` look broken
// outright: the repo list narrowed to the repos containing "d" and nothing else
// happened.
func TestDispatchKeyIsIdempotent(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = nil // nothing in flight: the form owns the keyboard from the start

	m := newModel()
	m.lens = "floor"
	if !m.cqPromptOn() {
		t.Fatal("precondition: an empty fleet should leave the prompt up")
	}

	mm, _, handled := m.updateFloorQueue("d")
	if !handled {
		t.Fatal("d should be handled on the triage lens")
	}
	if mm.dxFilter != "" {
		t.Errorf("d typed itself into the repo filter: dxFilter = %q", mm.dxFilter)
	}
	if !mm.cqDispatch {
		t.Error("d should leave the dispatch form open")
	}

	// Once something IS typed, d is a letter again — you must be able to filter
	// for a repo whose name contains one.
	m2 := newModel()
	m2.lens = "floor"
	m2.cqDispatch, m2.dxFilter = true, "clau"
	m2.key = runes("d")
	mm2, _, _ := m2.updateFloorQueue(runes("d").String())
	if mm2.dxFilter != "claud" {
		t.Errorf("a touched form should take d as text, got %q", mm2.dxFilter)
	}
}

// TestShippedTabActionsAreReal guards the class of bug this repo keeps
// shipping: a notice describing work that never happened. The product panel's
// SHIPPED tab said a feature had been "dispatched again" and launched nothing,
// and printed "opening #144" without opening anything.
func TestShippedTabActionsAreReal(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	products = []product{{name: "acme"}}
	shipped = map[string][]shippedDay{"acme": {{day: "today", items: []shippedItem{
		{feature: "csv export", repo: "acme-hq", pr: "#144", session: "abc"},
	}}}}

	m := newModel()
	m.lens = "product"
	m.rightTab = "shipped" // the tab these keys belong to

	// enter opens the overlay; submitting it must launch, not merely announce.
	mm, _ := m.updateProduct("enter")
	if !mm.resumeOpen {
		t.Fatal("enter on a shipped feature should open the resume overlay")
	}
	mm.resumeText = "add the per-region footer"
	mm2, cmd := mm.updateProduct("enter")
	if cmd == nil {
		t.Error("submitting the overlay announced a dispatch but returned no command")
	}
	if mm2.resumeOpen {
		t.Error("submitting should close the overlay")
	}

	// An empty prompt is not a dispatch, and must not claim to be one.
	mm3 := mm
	mm3.resumeText = "   "
	mm4, cmd3 := mm3.updateProduct("enter")
	if cmd3 != nil {
		t.Error("an empty prompt should launch nothing")
	}
	if !strings.Contains(mm4.notice, "nothing to dispatch") {
		t.Errorf("notice = %q", mm4.notice)
	}

	// o must reach a command rather than print "opening".
	_, cmdOpen := m.updateProduct("o")
	if cmdOpen == nil {
		t.Error("o should return a command that opens the PR")
	}

	// And the footer must not advertise the key nobody implemented.
	m.width, m.height = 190, 44
	if out := m.View(); strings.Contains(out, "clone to another repo") {
		t.Error("the footer still advertises `c clone`, which nothing implements")
	}
}
