package cockpit

// cq_test.go covers the triage lens's key state machine and its three render
// modes. Like fixtures_test.go it installs its own data and restores it, so
// nothing here depends on what another test left in the package vars.

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// installCQFixture puts two asks and two running dispatchers on the triage lens.
func installCQFixture(t *testing.T) {
	t.Helper()
	prevItems, prevWorking, prevOut := cqItems, cqWorking, cqLastOutput
	t.Cleanup(func() { cqItems, cqWorking, cqLastOutput = prevItems, prevWorking, prevOut })

	cqItems = []cqItem{
		{
			id: "id-one", kind: "permission", product: "alpha", title: "one",
			repo: "alpha-api", ref: "#1", age: "4m", want: "approve a permission",
			tone: "amber", lead: "It is blocked and waiting on you.",
			detail: "2 commits",
			acts: []cqAct{
				{k: "⏎", d: "attach", ok: "attaching to alpha-api session…", keep: true},
				{k: "x", d: "kill", ok: "killed \"one\""},
				{k: "s", d: "skip"},
			},
		},
		{
			id: "id-two", kind: "turn-done", product: "beta", title: "two",
			repo: "beta-svc", ref: "feature/two", age: "1h", want: "it finished a turn",
			tone: "normal", lead: "Done — want me to open a PR?",
			detail: "1 commit",
			acts: []cqAct{
				{k: "⏎", d: "attach", ok: "attaching to beta-svc session…", keep: true},
				{k: "y", d: "mark shipped", ok: "\"two\" marked shipped"},
				{k: "s", d: "skip"},
			},
		},
	}
	cqWorking = []cqGroup{
		{name: "alpha", rows: []cqWorkRow{{feature: "three", repo: "alpha-web", phase: "act", pass: 2, out: "6s"}}},
	}
	cqLastOutput = "6s"
}

func cqModel(t *testing.T) model {
	t.Helper()
	installCQFixture(t)
	m := newModel()
	m.width, m.height = 130, 40
	return m
}

func cqTitles(m model) string {
	var out []string
	for _, it := range m.cqQueue() {
		out = append(out, it.title)
	}
	return strings.Join(out, ",")
}

// The head is the only thing you act on; `s` sends it to the back.
func TestCQSkipRotates(t *testing.T) {
	m := cqModel(t)
	if got := cqTitles(m); got != "one,two" {
		t.Fatalf("queue = %s", got)
	}
	m = press(m, "s")
	if got := cqTitles(m); got != "two,one" {
		t.Errorf("after skip queue = %s, want two,one", got)
	}
	m = press(m, "s")
	if got := cqTitles(m); got != "one,two" {
		t.Errorf("after two skips queue = %s, want one,two", got)
	}
}

// An act flashes for cqFlashLinger, swallows every key while it does, then
// clears the item it was fired on and leaves an undo behind.
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
	for _, k := range []string{"s", "d", "w", "2", "?"} {
		mm := press(m, k)
		if mm.cqFlash == "" || mm.lens != "floor" || mm.cqWork || mm.cqDispatch {
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
	if got := cqTitles(m); got != "two" {
		t.Errorf("queue after the act = %s, want two", got)
	}
	if m.cqCleared != 1 || m.cqUndo == nil {
		t.Fatalf("cleared=%d undo=%+v", m.cqCleared, m.cqUndo)
	}

	m = press(m, "u")
	if got := cqTitles(m); got != "one,two" {
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
	if got := cqTitles(m); got != "one,two" {
		t.Errorf("a keep act must not clear the item, got %s", got)
	}
	if m.cqCleared != 0 || m.cqUndo != nil {
		t.Error("a keep act should not record an undo")
	}
}

// `d` opens the dispatch form over a full queue; an untouched form still
// navigates, and a touched one takes every key as text.
func TestCQFormFallThrough(t *testing.T) {
	m := cqModel(t)
	m = press(m, "d")
	if !m.cqDispatch || m.dxTouched() || !m.dxAuto {
		t.Fatalf("d should open an empty form, auto on: %v %v %v", m.cqDispatch, m.dxTouched(), m.dxAuto)
	}

	// Untouched: w reaches the working view, and 1–8 still switch lens.
	m = press(m, "w")
	if !m.cqWork {
		t.Fatal("w should fall through an untouched form to the working view")
	}
	m = press(m, "w")
	if m.cqWork {
		t.Fatal("w again should come back")
	}

	// Once anything is typed, those same keys are letters again. The form opens
	// on WHERE, so they land in the repo filter.
	m = press(m, "a")
	m = press(m, "w")
	if m.dxFilter != "aw" || m.cqWork {
		t.Fatalf("a touched form should take w as text: %q work=%v", m.dxFilter, m.cqWork)
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

// In the working view only the lens digits, the palette and the way back do
// anything at all.
func TestCQWorkingViewSwallows(t *testing.T) {
	m := cqModel(t)
	m = press(m, "w")
	if !m.cqWork {
		t.Fatal("w should open the working view")
	}
	for _, k := range []string{"s", "x", "j", "enter", "?"} {
		mm := press(m, k)
		if !mm.cqWork || mm.cqFlash != "" || mm.helpOpen {
			t.Fatalf("%q should be swallowed by the working view", k)
		}
	}
	if got := cqTitles(press(m, "s")); got != "one,two" {
		t.Errorf("skip must not reorder from the working view, got %s", got)
	}
	m = press(m, "esc")
	if m.cqWork {
		t.Error("esc should leave the working view")
	}
}

// Switching lens leaves the lens's modes behind.
func TestCQLensDigitResetsModes(t *testing.T) {
	m := cqModel(t)
	m = press(m, "w")
	m = press(m, "2")
	if m.lens != "products" || m.cqWork {
		t.Errorf("a digit should leave the working view: lens=%q work=%v", m.lens, m.cqWork)
	}

	// A form with text in it keeps the digit — you are typing a filter.
	m = cqModel(t)
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
	if m.lens != "product" || m.cqDispatch || m.dxTouched() {
		t.Errorf("an untouched form should let the digit through: lens=%q filter=%q", m.lens, m.dxFilter)
	}
}

// j/k scroll the evidence excerpt and stop at both ends of what is showable —
// not at the line count, which would leave the last screenful of k presses
// doing nothing. J/K do the same for the queue pane.
func TestCQScrollPanesClamp(t *testing.T) {
	m := cqModel(t)
	lines := make([]hunkLine, 0, 60)
	for i := 0; i < 60; i++ {
		lines = append(lines, hunkLine{sign: "+", text: "line " + itoa(i)})
	}
	cqItems[0].evFile, cqItems[0].evLines = "internal/thing.go", lines

	evMax, _ := m.cqScrollMax()
	if evMax < 1 {
		t.Fatalf("a 60-line diff should overflow the evidence pane, evMax = %d", evMax)
	}

	if m = press(m, "j"); m.cqEvScroll != 1 {
		t.Fatalf("j = %d, want 1", m.cqEvScroll)
	}
	for i := 0; i < 200; i++ {
		m = press(m, "j")
	}
	if m.cqEvScroll != evMax {
		t.Errorf("j should stop at the last showable line: %d, want %d", m.cqEvScroll, evMax)
	}
	// One k off the bottom must move the pane, not just an invisible counter.
	if m = press(m, "k"); m.cqEvScroll != evMax-1 {
		t.Errorf("k off the bottom = %d, want %d", m.cqEvScroll, evMax-1)
	}
	for i := 0; i < 200; i++ {
		m = press(m, "k")
	}
	if m.cqEvScroll != 0 {
		t.Errorf("k should stop at the top: %d", m.cqEvScroll)
	}

	// The queue pane only scrolls when it has more entries than rows, so shrink
	// the terminal rather than invent entries.
	m.height = 16
	if _, restMax := m.cqScrollMax(); restMax < 1 {
		t.Fatalf("a 16-row terminal should overflow the queue pane, restMax = %d", restMax)
	}
	if m = press(m, "J"); m.cqRestScroll != 1 {
		t.Errorf("J = %d, want 1", m.cqRestScroll)
	}
	if m = press(m, "K"); m.cqRestScroll != 0 {
		t.Errorf("K = %d, want 0", m.cqRestScroll)
	}
}

// An offset belongs to the item it was measured against: when the head changes,
// both panes go back to the top rather than opening part-way down a diff the
// human has not seen the start of.
func TestCQSnapPanesOnNewHead(t *testing.T) {
	m := cqModel(t)
	lines := make([]hunkLine, 0, 60)
	for i := 0; i < 60; i++ {
		lines = append(lines, hunkLine{sign: "+", text: "line " + itoa(i)})
	}
	cqItems[0].evFile, cqItems[0].evLines = "internal/thing.go", lines

	m = press(m, "j")
	m = press(m, "j")
	if m.cqEvScroll == 0 {
		t.Fatal("expected the evidence pane to have scrolled")
	}
	m = press(m, "s") // skip: a different item is now the head
	if m.cqEvScroll != 0 || m.cqRestScroll != 0 {
		t.Errorf("a new head should snap both panes: ev=%d rest=%d", m.cqEvScroll, m.cqRestScroll)
	}
}

// A refresh that drops an ask must drop it from the order, the suppressed set
// and the undo — otherwise cqSuppressed grows without bound.
func TestCQReconcileForgetsDepartedItems(t *testing.T) {
	m := cqModel(t)
	m = press(m, "s") // seeds the order with both ids
	m.cqSuppressed["id-one"] = true
	m.cqUndo = &cqUndoEntry{id: "id-one", label: "killed \"one\""}

	cqItems = cqItems[1:] // id-one's record left the queue for real
	m = m.cqReconcile()

	if len(m.cqOrder) != 1 || m.cqOrder[0] != "id-two" {
		t.Errorf("cqOrder = %v", m.cqOrder)
	}
	if m.cqSuppressed["id-one"] {
		t.Error("a departed id should leave the suppressed set")
	}
	if m.cqUndo != nil {
		t.Error("undo should be dropped once there is nothing to put back")
	}
}

func TestCQFooterHelpPerMode(t *testing.T) {
	m := cqModel(t)
	if got := m.footerHelp(); !strings.Contains(got, "x kill") || !strings.Contains(got, "d dispatch") {
		t.Errorf("item help = %q", got)
	}
	if got := press(m, "w").footerHelp(); !strings.Contains(got, "back") {
		t.Errorf("working help = %q", got)
	}
	md := press(m, "d")
	// An untouched form still advertises the exits it still has.
	if got := md.footerHelp(); !strings.Contains(got, "w running") || !strings.Contains(got, "1…8 sections") {
		t.Errorf("untouched-form help = %q", got)
	}
	// A touched one drops them — they are letters now — and offers ctrl+d,
	// which is the key that actually submits (ctrl+⏎ is not reportable).
	got := press(md, "a").footerHelp()
	if !strings.Contains(got, "ctrl+d dispatch") || strings.Contains(got, "1…8") {
		t.Errorf("touched-form help = %q", got)
	}
}

// Every mode renders inside its box at every width tier, including mid-flash.
func TestCQRendersInEveryMode(t *testing.T) {
	installCQFixture(t)
	for _, w := range smokeWidths {
		for _, h := range []int{44, 20, 10} {
			base := newModel()
			base.width, base.height = w, h
			cases := map[string]model{
				"item":    base,
				"working": press(base, "w"),
				"draft":   press(press(base, "d"), "a"),
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
// into main". collectCQ's switch once handled only blocked/needs/working, so
// those dispatchers appeared in neither the queue nor the working view and
// dropped off the lens with a merge sitting there waiting.
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

	var found *cqItem
	for i := range s.cqItems {
		if s.cqItems[i].title == "seat limits" {
			found = &s.cqItems[i]
		}
	}
	if found == nil {
		t.Fatal("a review dispatcher is missing from the queue entirely")
	}
	if found.want != "approve a merge" {
		t.Errorf("want = %q, want %q", found.want, "approve a merge")
	}
	var keys []string
	for _, a := range found.acts {
		keys = append(keys, a.k)
	}
	if !strings.Contains(strings.Join(keys, ""), "y") {
		t.Errorf("review item offers no merge act: keys %v", keys)
	}
}

// TestDispatchKeyIsIdempotent guards the key the footer advertises as "d
// dispatch". The form's untouched-fall-through set left `d` out, so pressing it
// while the form was already up typed a letter into the repo filter. With a
// clear queue the form is up by default, which made `d` look broken outright:
// the repo list narrowed to the repos containing "d" and nothing else happened.
func TestDispatchKeyIsIdempotent(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	cqItems = nil // a clear queue: the form owns the keyboard from the start

	m := newModel()
	m.lens = "floor"
	if !m.cqPromptOn() {
		t.Fatal("precondition: a clear queue should leave the prompt up")
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
