package cockpit

// interactions2_test.go extends smoke_test.go / live_test.go with the key
// branches those didn't reach: overlay open/close variants, the palette's
// navigation and dispatch, the settings editor's field types, the floor's
// follow/kill/attach/reply/header-ticket/stack keys, the backlog's
// dispatch/pick-all keys, the decisions lens's remaining nav, the products
// and product lenses' remaining tabs/overlays, and a handful of small pure
// helpers in model.go/seed.go/live.go. Tests that read/write the package's
// global data vars always snapshot and restore them; nothing here touches a
// real tmux session, gh, or the network.

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
)

// ---- keys.go: applyEdit / filteredCommands / runCommand ---------------------

func TestApplyEditBranches(t *testing.T) {
	if next, submit, cancel := applyEdit("hello", "esc"); next != "hello" || submit || !cancel {
		t.Errorf("esc: %q %v %v", next, submit, cancel)
	}
	if next, submit, cancel := applyEdit("hello", "enter"); next != "hello" || !submit || cancel {
		t.Errorf("enter: %q %v %v", next, submit, cancel)
	}
	if next, _, _ := applyEdit("hello", "backspace"); next != "hell" {
		t.Errorf("backspace: %q", next)
	}
	if next, _, _ := applyEdit("", "backspace"); next != "" {
		t.Errorf("backspace on empty: %q", next)
	}
	if next, _, _ := applyEdit("hi", "space"); next != "hi " {
		t.Errorf("space: %q", next)
	}
	if next, _, _ := applyEdit("hi", " "); next != "hi " {
		t.Errorf("literal space: %q", next)
	}
	if next, _, _ := applyEdit("hi", "!"); next != "hi!" {
		t.Errorf("single rune default: %q", next)
	}
	if next, submit, cancel := applyEdit("hi", "up"); next != "hi" || submit || cancel {
		t.Errorf("unknown multi-rune key should be a no-op: %q %v %v", next, submit, cancel)
	}
}

func TestFilteredCommandsQuery(t *testing.T) {
	m := newModel()
	if got := len(m.filteredCommands()); got != len(commands) {
		t.Errorf("empty query should return every command: got %d, want %d", got, len(commands))
	}
	m.paletteText = "backlog"
	if got := m.filteredCommands(); len(got) != 1 || got[0].name != "backlog" {
		t.Errorf("query 'backlog': %+v", got)
	}
	m.paletteText = "zz-nothing-matches-zz"
	if got := m.filteredCommands(); len(got) != 0 {
		t.Errorf("query with no match should be empty: %+v", got)
	}
}

func TestRunCommandBranches(t *testing.T) {
	// settings/roots both open the settings editor.
	m := newModel()
	m.paletteOpen, m.paletteText = true, "settings"
	mm, _ := m.runCommand()
	if mm.settings == nil || mm.paletteOpen {
		t.Error("runCommand('settings') should open settings and close the palette")
	}

	m = newModel()
	m.paletteOpen, m.paletteText = true, "roots"
	mm, _ = m.runCommand()
	if mm.settings == nil {
		t.Error("runCommand('roots') should also open settings")
	}

	// A direct-map command switches lens.
	m = newModel()
	m.paletteOpen, m.paletteText = true, "usage"
	mm, _ = m.runCommand()
	if mm.lens != "usage" {
		t.Errorf("runCommand('usage') lens = %q", mm.lens)
	}

	// A "product X" command opens the product lens.
	m = newModel()
	m.paletteOpen, m.paletteText = true, "product cortiva"
	mm, _ = m.runCommand()
	if mm.lens != "product" {
		t.Errorf("runCommand('product cortiva') lens = %q", mm.lens)
	}

	// A command outside the direct map / product prefix leaves the lens
	// unchanged and just sets a notice. "merge" is chosen because it is the
	// only command whose name or hint contains "merge".
	m = newModel()
	m.lens = "floor"
	m.paletteOpen, m.paletteText = true, "merge"
	mm, _ = m.runCommand()
	if mm.lens != "floor" || mm.notice != ":merge" {
		t.Errorf("runCommand('merge') = lens=%q notice=%q", mm.lens, mm.notice)
	}

	// paletteCursor beyond the filtered list clamps to the last entry.
	m = newModel()
	m.paletteOpen, m.paletteText, m.paletteCursor = true, "", 9999
	mm, _ = m.runCommand()
	if mm.paletteOpen {
		t.Error("runCommand with a clamped cursor should still close the palette")
	}

	// No commands match: palette just closes.
	m = newModel()
	m.paletteOpen, m.paletteText = true, "zz-nothing-zz"
	mm, _ = m.runCommand()
	if mm.paletteOpen || mm.paletteText != "" {
		t.Error("runCommand with zero matches should close the palette")
	}
}

// ---- handleKey: overlay open/close variants ----------------------------------

func TestHandleKeyOverlayVariants(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44

	// help: open with '?', close with 'q'.
	m = press(m, "?")
	if !m.helpOpen {
		t.Fatal("? should open help")
	}
	m = press(m, "q")
	if m.helpOpen {
		t.Error("q should close help")
	}

	// diff: open with 'D', close with 'q'.
	m = press(m, "D")
	if !m.diffOpen {
		t.Fatal("D should open diff")
	}
	m = press(m, "q")
	if m.diffOpen {
		t.Error("q should close diff")
	}

	// filter: open, type, submit with enter (keeps the typed filter).
	m = press(m, "/")
	m = press(m, "x")
	m = press(m, "enter")
	if m.filterOpen {
		t.Error("enter should close the filter editor")
	}
	if m.filter != "x" {
		t.Errorf("submitted filter = %q, want to keep typed text", m.filter)
	}
	m = press(m, "esc") // clear marks/filter back to a clean state

	// palette: open, navigate down/up, then run via enter.
	m = press(m, ":")
	if !m.paletteOpen {
		t.Fatal(": should open the palette")
	}
	m = press(m, "down")
	m = press(m, "up")
	m = press(m, "enter")
	if m.paletteOpen {
		t.Error("enter should run the command and close the palette")
	}

	// palette: open then esc cancels.
	m = press(m, ":")
	m = press(m, "esc")
	if m.paletteOpen {
		t.Error("esc should close the palette")
	}

	// settings: open with ',' then esc.
	m = press(m, ",")
	if m.settings == nil {
		t.Fatal(", should open settings")
	}
	m = press(m, "esc")
	if m.settings != nil {
		t.Error("esc should close settings")
	}

	// tab toggles narrowPane both ways.
	before := m.narrowPane
	m = press(m, "tab")
	if m.narrowPane == before {
		t.Error("tab should toggle narrowPane")
	}
	m = press(m, "tab")
	if m.narrowPane != before {
		t.Error("tab again should toggle narrowPane back")
	}

	// q / ctrl+c both quit.
	_, cmd := m.handleKey("q")
	if cmd == nil {
		t.Error("q at top level should return tea.Quit")
	}
	_, cmd = m.handleKey("ctrl+c")
	if cmd == nil {
		t.Error("ctrl+c should return tea.Quit")
	}
}

func TestHandleKeyConfirmCancel(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "x") // opens a kill confirm on the selected dispatcher
	if m.confirm == nil {
		t.Skip("nothing selected to confirm a kill on")
	}
	m = press(m, "n")
	if m.confirm != nil {
		t.Error("n should cancel the confirm")
	}
	if m.notice != "cancelled" {
		t.Errorf("notice = %q, want cancelled", m.notice)
	}

	m = press(m, "x")
	if m.confirm == nil {
		t.Fatal("x should reopen a confirm")
	}
	m = press(m, "esc")
	if m.confirm != nil {
		t.Error("esc should also cancel the confirm")
	}
}

func TestHandleKeyReplyFocused(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "r") // focuses the reply input on the floor's selected dispatcher
	if !m.replyFocused {
		t.Fatal("r should focus reply")
	}
	m = press(m, "h")
	m = press(m, "i")
	if m.replyText != "hi" {
		t.Errorf("replyText = %q", m.replyText)
	}
	// esc cancels without submitting.
	m = press(m, "esc")
	if m.replyFocused {
		t.Error("esc should unfocus reply")
	}

	// submit path returns a replyCmd (feature resolves via floorSelectedFeature).
	m = press(m, "r")
	m = press(m, "!")
	next, cmd := m.handleKey("enter")
	mm := next.(model)
	if mm.replyFocused {
		t.Error("enter should unfocus reply")
	}
	if cmd == nil {
		t.Error("submitting a reply should return a cmd")
	}
}

func TestUndoKey(t *testing.T) {
	m := newModel()
	m.undo = "ship widget"
	mm, _ := m.handleKey("u")
	if mm.(model).undo != "" {
		t.Error("u should clear a pending undo")
	}
	if !strings.Contains(mm.(model).notice, "undone") {
		t.Errorf("notice = %q", mm.(model).notice)
	}
}

// ---- floor.go: remaining updateFloor branches --------------------------------

func TestFloorFollowToggle(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "F")
	if !m.follow {
		t.Fatal("F should turn follow on")
	}
	m = press(m, "F")
	if m.follow {
		t.Error("F again should turn follow off")
	}
}

func TestFloorAttachKillMarkDoneDeny(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44

	// enter/a attach: seed data has no live record, so this degrades to a
	// notice rather than a real tmux exec.
	next, _ := m.handleKey("enter")
	mm := next.(model)
	if !strings.Contains(mm.notice, "no live") {
		t.Errorf("attach notice = %q", mm.notice)
	}

	m2 := newModel()
	m2.width, m2.height = 190, 44
	next2, cmd := m2.handleKey("d")
	if cmd == nil {
		t.Error("d should return a markDoneCmd")
	}
	_ = next2

	m3 := newModel()
	m3.width, m3.height = 190, 44
	m3 = press(m3, "n")
	if m3.notice == "" {
		t.Error("n should always leave a notice")
	}
}

func TestFloorGroupHeaderTicketNav(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "l") // cursor is still 0: the first entry in product grouping is a header
	if !m.entry().header {
		t.Skip("first entry is not a header under the current seed grouping")
	}
	for _, k := range []string{"j", "j", "k", "enter", "d"} {
		m = press(m, k)
		renderClean(t, m, "header ticket nav "+k)
	}
	if m.notice == "" {
		t.Error("header ticket dispatch should always leave a notice")
	}
}

func TestFloorStackNavAndReply(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "j") // move off the header row
	m = press(m, "l") // detail pane
	if m.entry().header {
		t.Skip("cursor landed back on a header under the current seed grouping")
	}
	for _, k := range []string{"j", "k", "enter"} {
		m = press(m, k)
		renderClean(t, m, "stack nav "+k)
	}
	m = press(m, "r")
	if !m.replyFocused {
		t.Fatal("r in the stack pane should focus reply")
	}
	m = press(m, "esc")
	if m.replyFocused {
		t.Error("esc should cancel the stack-pane reply")
	}
}

// ---- backlog.go: dispatch / pick-all / source cycle --------------------------

func TestBacklogDispatchAndPickAll(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "5") // backlog lens, cursor 0: CTV-124, taken == ""

	// Untaken ticket: enter returns a launchCmd (m.cfg is nil, so the cmd
	// itself degrades harmlessly if ever invoked — see actions_test.go).
	next, cmd := m.handleKey("enter")
	if cmd == nil {
		t.Error("enter on an untaken ticket should return a launchCmd")
	}
	m = next.(model)

	// Move to a ticket that already has a dispatcher (seed: index 3, "fixture import").
	m.backlogCursor = 3
	next2, cmd2 := m.handleKey("enter")
	m = next2.(model)
	if cmd2 != nil {
		t.Error("enter on an already-taken ticket should not dispatch")
	}
	if !strings.Contains(m.notice, "already has a dispatcher") {
		t.Errorf("notice = %q", m.notice)
	}

	// Pick a couple then dispatch the lot.
	m = press(m, "space")
	m.backlogCursor = 0
	m = press(m, "space")
	m = press(m, "ctrl+d")
	if len(m.picked) != 0 {
		t.Error("ctrl+d should clear the picked set")
	}
	if !strings.Contains(m.notice, "dispatched") {
		t.Errorf("ctrl+d notice = %q", m.notice)
	}

	// Cycle the source filter all the way around (all → gh → lin → ado → all).
	for i := 0; i < 4; i++ {
		m = press(m, "s")
	}
	if m.srcFilter != "all" {
		t.Errorf("source filter after a full cycle = %q, want all", m.srcFilter)
	}
}

// ---- decisions.go: remaining nav ---------------------------------------------

func TestDecisionsRemainingKeys(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "7")
	for _, k := range []string{"down", "up", "o", "right", "esc"} {
		m = press(m, k)
		renderClean(t, m, "decisions "+k)
	}
}

func TestDecisionsEmptyRepoBranch(t *testing.T) {
	saved := captureVars()
	defer applySnapshot(saved)
	decisions = map[string][]decision{"empty-repo": {}}
	decisionRepoOrder = []string{"empty-repo"}
	// pluginForRepo falls back to plugins[3] by convention (mirrors the
	// design's PLUGINS[3] builtin fallback), so the slice must carry at
	// least 4 entries even in this synthetic scenario.
	plugins = []plugin{
		{id: "p0", name: "p0"}, {id: "p1", name: "p1"}, {id: "p2", name: "p2"},
		{id: "builtin", name: "cockpit records"},
	}

	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "7")
	m = press(m, "a")
	if m.notice != "" {
		t.Errorf("accept on an empty record list should leave no notice, got %q", m.notice)
	}
	m = press(m, "s")
	if m.notice != "" {
		t.Errorf("supersede on an empty record list should leave no notice, got %q", m.notice)
	}
	m = press(m, "o")
	renderClean(t, m, "decisions open on empty repo")
}

// ---- products.go / product.go: remaining tabs/overlays -----------------------

func TestProductsBoundaryAndEnter(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "2")
	m = press(m, "k") // already at 0: should clamp, not go negative
	if m.productCursor != 0 {
		t.Errorf("productCursor = %d, want 0", m.productCursor)
	}
	for i := 0; i < len(products)+2; i++ {
		m = press(m, "j")
	}
	if m.productCursor != len(products)-1 {
		t.Errorf("productCursor = %d, want clamped to %d", m.productCursor, len(products)-1)
	}
	m = press(m, "enter")
	if m.lens != "product" {
		t.Error("enter should open the product lens")
	}
}

func TestProductReviewTabFull(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "3") // product lens, defaults to "cortiva" (productCursor 0)
	m = press(m, "R")

	// cursor 0 is a "mine" review: approving your own PR is refused.
	m = press(m, "a")
	if !strings.Contains(m.notice, "cannot approve") {
		t.Errorf("approve own PR notice = %q", m.notice)
	}
	m = press(m, "j") // #150, also mine — still not approvable
	m = press(m, "j") // #147: not mine
	m = press(m, "a")
	if !strings.Contains(m.notice, "approved") {
		t.Errorf("approve notice = %q", m.notice)
	}
	m = press(m, "c")
	if !strings.Contains(m.notice, "changes requested") {
		t.Errorf("changes notice = %q", m.notice)
	}
	m = press(m, "d")
	if !strings.Contains(m.notice, "reviewer dispatched") {
		t.Errorf("dispatch-reviewer notice = %q", m.notice)
	}
	m = press(m, "k") // move back up

	// open the review overlay and walk its keys.
	m = press(m, "enter")
	if !m.reviewOpen {
		t.Fatal("enter should open the review overlay")
	}
	m = press(m, "esc")
	if m.reviewOpen {
		t.Error("esc should close the review overlay")
	}
	m = press(m, "enter")
	m = press(m, "a")
	if m.reviewOpen {
		t.Error("a in the overlay should close it")
	}
	m = press(m, "R")
	m = press(m, "enter")
	m = press(m, "c")
	if m.reviewOpen {
		t.Error("c in the overlay should close it")
	}
	m = press(m, "R")
	m = press(m, "enter")
	m = press(m, "d")
	if m.reviewOpen {
		t.Error("d in the overlay should close it")
	}
}

func TestProductTeamTabNoop(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "3")
	m = press(m, "T")
	m = press(m, "j") // team tab has no key handling: should just no-op
	renderClean(t, m, "team tab j")
}

func TestProductShippedTabFull(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "3")
	m = press(m, "S")
	m = press(m, "j")
	m = press(m, "k")
	m = press(m, "c")
	if !strings.Contains(m.notice, "clone") {
		t.Errorf("clone notice = %q", m.notice)
	}
	m = press(m, "o")
	if !strings.Contains(m.notice, "opening") {
		t.Errorf("open notice = %q", m.notice)
	}
	m = press(m, "enter")
	if !m.resumeOpen {
		t.Fatal("enter on a shipped item should open the resume overlay")
	}
	m = press(m, "x")
	m = press(m, "y")
	if m.resumeText != "xy" {
		t.Errorf("resumeText = %q", m.resumeText)
	}
	m = press(m, "esc")
	if m.resumeOpen {
		t.Error("esc should cancel the resume overlay")
	}
	m = press(m, "enter")
	m = press(m, "enter") // submit with empty text
	if m.resumeOpen {
		t.Error("submitting resume should close the overlay")
	}
	if !strings.Contains(m.notice, "resumed session") {
		t.Errorf("resume notice = %q", m.notice)
	}
}

// ---- settings.go: field editing across every kind ----------------------------

func TestSettingsEditEveryField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	saved := captureVars()
	defer applySnapshot(saved)

	m := newModel()
	m.width, m.height = 190, 44
	m.cfg = &config.Config{}
	m.settings = newSettings(m.cfg)

	for i, f := range settingsFields {
		m.settings.cursor = i
		mm, _ := m.handleKey("enter")
		m = mm.(model)
		if !m.settings.editing {
			t.Fatalf("field %s: enter should start editing", f.key)
		}
		switch f.kind {
		case setInt:
			for _, ch := range "42" {
				mm, _ = m.handleKey(string(ch))
				m = mm.(model)
			}
		case setRoots:
			for _, ch := range "/tmp/a, /tmp/b" {
				mm, _ = m.handleKey(string(ch))
				m = mm.(model)
			}
		default:
			for _, ch := range "value" {
				mm, _ = m.handleKey(string(ch))
				m = mm.(model)
			}
		}
		mm, _ = m.handleKey("enter")
		m = mm.(model)
		if m.settings.editing {
			t.Fatalf("field %s: enter should commit and stop editing", f.key)
		}
		if m.settings.saved == "" {
			t.Errorf("field %s: expected a saved flash", f.key)
		}
	}

	if m.cfg.WeeklyTokenLimit != 42 {
		t.Errorf("weekly_token_limit = %d, want 42", m.cfg.WeeklyTokenLimit)
	}
	if len(m.cfg.Roots) != 2 {
		t.Errorf("roots = %v, want 2 entries", m.cfg.Roots)
	}

	// esc while editing discards without committing.
	m.settings.cursor = 0
	mm, _ := m.handleKey("enter")
	m = mm.(model)
	mm, _ = m.handleKey("z")
	m = mm.(model)
	mm, _ = m.handleKey("esc")
	m = mm.(model)
	if m.settings.editing {
		t.Error("esc should stop editing")
	}

	// j/k navigate the field list and clear the saved flash.
	m.settings.saved = "flash"
	mm, _ = m.handleKey("j")
	m = mm.(model)
	if m.settings.saved != "" {
		t.Error("j should clear the saved flash")
	}
	mm, _ = m.handleKey("k")
	m = mm.(model)

	renderClean(t, m, "settings view")
}

func TestSettingsSaveWithNilCfg(t *testing.T) {
	m := newModel()
	m.settings = newSettings(nil)
	mm, _ := m.handleKey("enter") // start editing field 0
	m = mm.(model)
	mm, _ = m.handleKey("enter") // commit with nil cfg: no crash, no save attempt
	m = mm.(model)
	if m.settings.editing {
		t.Error("commit should stop editing even with a nil cfg")
	}
}

func TestKeyToMsgBranches(t *testing.T) {
	cases := map[string]tea.KeyType{
		"space":     tea.KeySpace,
		"backspace": tea.KeyBackspace,
		"left":      tea.KeyLeft,
		"right":     tea.KeyRight,
	}
	for k, want := range cases {
		msg := keyToMsg(k).(tea.KeyMsg)
		if msg.Type != want {
			t.Errorf("keyToMsg(%q).Type = %v, want %v", k, msg.Type, want)
		}
	}
	msg := keyToMsg("x").(tea.KeyMsg)
	if msg.Type != tea.KeyRunes || string(msg.Runes) != "x" {
		t.Errorf("keyToMsg('x') = %+v", msg)
	}
	msg = keyToMsg("unknown-multi").(tea.KeyMsg)
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 0 {
		t.Errorf("keyToMsg(multi) = %+v", msg)
	}
}

// ---- model.go: small selection helpers ---------------------------------------

func TestModelSmallHelpers(t *testing.T) {
	m := newModel()
	m.groupBy = ""
	if got := m.groupByMode(); got != "product" {
		t.Errorf("groupByMode default = %q", got)
	}
	m.groupBy = "repo"
	if got := m.groupByMode(); got != "repo" {
		t.Errorf("groupByMode explicit = %q", got)
	}

	x := dispatch{feature: "f1", agents: []agent{{model: "opus"}}}
	if got := m.modelOf(x); got != "opus" {
		t.Errorf("modelOf falls back to agent model: %q", got)
	}
	m.modelsBy = map[string]string{"f1": "haiku"}
	if got := m.modelOf(x); got != "haiku" {
		t.Errorf("modelOf override: %q", got)
	}
	xNoAgent := dispatch{feature: "f2"}
	if got := m.modelOf(xNoAgent); got != "sonnet" {
		t.Errorf("modelOf default: %q", got)
	}

	y := dispatch{feature: "f3", mode: "auto"}
	if got := m.modeOf(y); got != "auto" {
		t.Errorf("modeOf falls back to x.mode: %q", got)
	}
	m.modesBy = map[string]string{"f3": "full"}
	if got := m.modeOf(y); got != "full" {
		t.Errorf("modeOf override: %q", got)
	}
	yNoMode := dispatch{feature: "f4"}
	if got := m.modeOf(yNoMode); got != "edits" {
		t.Errorf("modeOf default: %q", got)
	}
}

func TestDoConfirmNilAndKill(t *testing.T) {
	m := newModel()
	mm, cmd := m.doConfirm()
	if cmd != nil {
		t.Error("doConfirm with nil confirm should be a no-op")
	}
	_ = mm

	m2 := newModel()
	m2.width, m2.height = 190, 44
	m2 = press(m2, "x")
	if m2.confirm == nil {
		t.Skip("nothing selected to confirm a kill on")
	}
	mm2, cmd2 := m2.doConfirm()
	if cmd2 == nil {
		t.Error("doConfirm(kill) should return a batched cmd")
	}
	if mm2.confirm != nil {
		t.Error("doConfirm should clear the pending confirm")
	}
	if mm2.undo == "" {
		t.Error("doConfirm(kill) should offer an undo")
	}
}

// ---- seed.go: fallback branches -----------------------------------------------

func TestRepoProductAndModeByIDFallbacks(t *testing.T) {
	if got := repoProduct("no-such-repo-xyz"); got != "—" {
		t.Errorf("repoProduct(unknown) = %q, want —", got)
	}
	if got := modeByID("no-such-mode"); got != modes[1] {
		t.Errorf("modeByID(unknown) = %+v, want the 'edits' fallback", got)
	}
	if got := modeByID("plan"); got.label != "plan only" {
		t.Errorf("modeByID(plan) = %+v", got)
	}
}

// ---- live.go: forge() branches ------------------------------------------------

func TestCtxForgeBranches(t *testing.T) {
	ctx := &collectCtx{}

	if got := ctx.forge("/nonexistent/path/xyz"); got != "gh" {
		t.Errorf("no-remote path should default to gh: %q", got)
	}

	repo := newTestGitRepo(t, "forge-gh")
	if out, err := exec.Command("git", "-C", repo, "remote", "add", "origin", "https://github.com/acme/widgets.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	if got := ctx.forge(repo); got != "gh" {
		t.Errorf("github remote: %q", got)
	}

	repo2 := newTestGitRepo(t, "forge-ado")
	if out, err := exec.Command("git", "-C", repo2, "remote", "add", "origin", "https://dev.azure.com/acme/widgets/_git/widgets").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	if got := ctx.forge(repo2); got != "ado" {
		t.Errorf("azure devops remote: %q", got)
	}

	repo3 := newTestGitRepo(t, "forge-vs")
	if out, err := exec.Command("git", "-C", repo3, "remote", "add", "origin", "https://acme.visualstudio.com/widgets/_git/widgets").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
	if got := ctx.forge(repo3); got != "ado" {
		t.Errorf("visualstudio.com remote: %q", got)
	}
}

// ---- default-branch lenses: queue/usage/velocity accept unhandled keys -------

func TestPassthroughLensesIgnoreKeys(t *testing.T) {
	for _, lens := range []string{"4", "6", "8"} {
		m := newModel()
		m.width, m.height = 190, 44
		m = press(m, lens)
		m = press(m, "j") // no updater registered: handleKey's default branch
		renderClean(t, m, "passthrough lens "+lens)
	}
}
