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
		{name: "alpha", rows: []cqWorkRow{{feature: "three", repo: "alpha-web", doing: "writing code", out: "6s"}}},
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

// `d` opens the draft over a full queue; the empty draft still navigates, and a
// non-empty one takes every key as text.
func TestCQDraftFallThroughAndSubmit(t *testing.T) {
	m := cqModel(t)
	m = press(m, "d")
	if !m.cqDispatch || m.cqDraft != "" {
		t.Fatalf("d should open an empty draft: %v %q", m.cqDispatch, m.cqDraft)
	}

	// Empty draft: w reaches the working view, and 1–8 still switch lens.
	m = press(m, "w")
	if !m.cqWork {
		t.Fatal("w should fall through an empty draft to the working view")
	}
	m = press(m, "w")
	if m.cqWork {
		t.Fatal("w again should come back")
	}

	// Once there is a draft, those same keys are text.
	m = press(m, "a")
	m = press(m, "w")
	if m.cqDraft != "aw" || m.cqWork {
		t.Fatalf("a non-empty draft should take w as text: %q work=%v", m.cqDraft, m.cqWork)
	}
	m = press(m, "backspace")
	if m.cqDraft != "a" {
		t.Errorf("backspace: %q", m.cqDraft)
	}

	// Enter hands the work to the dispatch form, which still has a repo to pick.
	next, cmd := m.handleKey("enter")
	m = next.(model)
	if m.dispatchForm == nil || cmd == nil {
		t.Fatal("enter on a draft should open the dispatch form")
	}
	if got := m.dispatchForm.prompt.Value(); got != "a" {
		t.Errorf("the draft should reach the form's prompt, got %q", got)
	}
	if m.cqDispatch || m.cqDraft != "" {
		t.Error("submitting should close the draft")
	}
}

func TestCQDraftEscAndEmptySubmit(t *testing.T) {
	m := cqModel(t)
	m = press(m, "d")
	m = press(m, "z")
	m = press(m, "esc")
	if m.cqDispatch || m.cqDraft != "" {
		t.Error("esc should abandon the draft")
	}

	m = press(m, "d")
	next, cmd := m.handleKey("enter")
	m = next.(model)
	if cmd != nil || m.dispatchForm != nil {
		t.Error("an empty draft should dispatch nothing")
	}
	if m.notice != "nothing to dispatch" {
		t.Errorf("notice = %q", m.notice)
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

	// A draft with text in it keeps the digit — you are writing a sentence.
	m = cqModel(t)
	m = press(m, "d")
	m = press(m, "x")
	m = press(m, "2")
	if m.lens != "floor" || m.cqDraft != "x2" {
		t.Errorf("a digit in a non-empty draft is text: lens=%q draft=%q", m.lens, m.cqDraft)
	}

	// An empty one does not trap it.
	m = press(m, "backspace")
	m = press(m, "backspace")
	m = press(m, "3")
	if m.lens != "product" || m.cqDispatch || m.cqDraft != "" {
		t.Errorf("an empty draft should let the digit through: lens=%q draft=%q", m.lens, m.cqDraft)
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
	if got := md.footerHelp(); !strings.Contains(got, "type the work") {
		t.Errorf("empty-draft help = %q", got)
	}
	if got := press(md, "a").footerHelp(); got != "enter dispatch · esc clear" {
		t.Errorf("draft help = %q", got)
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
