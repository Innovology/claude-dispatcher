package cockpit

// interactions2_test.go extends smoke_test.go / live_test.go with the key
// branches those didn't reach: overlay open/close variants, the palette's
// navigation and dispatch, the settings editor's field types, the backlog's
// dispatch/pick-all keys, the decisions lens's remaining nav, the products
// and product lenses' remaining tabs/overlays, and a handful of small pure
// helpers in model.go/types.go/live.go. The triage lens's own keys live in
// cq_test.go. Tests that read/write the package's global data vars always
// snapshot and restore them; nothing here touches a real tmux session, gh, or
// the network.

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
)

// ---- keys.go: applyEdit / filteredCommands / runCommand ---------------------

// runes builds the message a terminal delivers for a run of typed characters.
// Bubbletea batches a fast burst or a paste into one message like this.
func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestApplyEditBranches(t *testing.T) {
	if next, submit, cancel := applyEdit("hello", "esc", tea.KeyMsg{Type: tea.KeyEsc}); next != "hello" || submit || !cancel {
		t.Errorf("esc: %q %v %v", next, submit, cancel)
	}
	if next, submit, cancel := applyEdit("hello", "enter", tea.KeyMsg{Type: tea.KeyEnter}); next != "hello" || !submit || cancel {
		t.Errorf("enter: %q %v %v", next, submit, cancel)
	}
	if next, _, _ := applyEdit("hello", "backspace", tea.KeyMsg{Type: tea.KeyBackspace}); next != "hell" {
		t.Errorf("backspace: %q", next)
	}
	if next, _, _ := applyEdit("", "backspace", tea.KeyMsg{Type: tea.KeyBackspace}); next != "" {
		t.Errorf("backspace on empty: %q", next)
	}
	if next, _, _ := applyEdit("hi", "space", tea.KeyMsg{}); next != "hi " {
		t.Errorf("space by name: %q", next)
	}
	if next, _, _ := applyEdit("hi", " ", tea.KeyMsg{Type: tea.KeySpace}); next != "hi " {
		t.Errorf("literal space: %q", next)
	}
	if next, _, _ := applyEdit("hi", "!", runes("!")); next != "hi!" {
		t.Errorf("single rune: %q", next)
	}
	if next, submit, cancel := applyEdit("hi", "up", tea.KeyMsg{Type: tea.KeyUp}); next != "hi" || submit || cancel {
		t.Errorf("a named key types nothing: %q %v %v", next, submit, cancel)
	}
	if next, _, _ := applyEdit("", "alt+d", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d"), Alt: true}); next != "" {
		t.Errorf("alt chord is not text: %q", next)
	}
}

// A burst of typing, or a paste, arrives as ONE message carrying every rune.
// Rebuilding text from the key name alone dropped the whole run, which left the
// filter, palette, reply box and the new-dispatch form dead to normal typing.
func TestApplyEditKeepsWholeBurst(t *testing.T) {
	burst := runes("count")
	next, _, _ := applyEdit("", burst.String(), burst)
	if next != "count" {
		t.Errorf("burst dropped: got %q, want %q", next, "count")
	}

	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix the retry loop"), Paste: true}
	if next, _, _ = applyEdit("", paste.String(), paste); next != "fix the retry loop" {
		t.Errorf("paste dropped: got %q", next)
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

	// A "product X" command opens the product panel — and has to seed it the
	// same way enter on the products list does, or the panel comes up on
	// whichever tab and shipped row the last visit left behind.
	m = newModel()
	m.rightTab, m.shipCursor = "shipped", 4
	m.paletteOpen, m.paletteText = true, "product"
	mm, _ = m.runCommand()
	if mm.lens != "product" {
		t.Errorf("runCommand('product') lens = %q", mm.lens)
	}
	if mm.rightTab != "overview" || mm.shipCursor != 0 {
		t.Errorf("runCommand('product') left a stale panel: tab=%q shipCursor=%d", mm.rightTab, mm.shipCursor)
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
	// Off the triage lens first: there the dispatch prompt owns the keyboard
	// whenever the queue is empty, so '?' and ':' would be typed, not routed.
	m = press(m, "2")

	// help: open with '?', close with 'q'.
	m = press(m, "?")
	if !m.helpOpen {
		t.Fatal("? should open help")
	}
	m = press(m, "q")
	if m.helpOpen {
		t.Error("q should close help")
	}

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

// ctrl+c is resolved before every overlay and lens, so no mode can trap the
// process — including the triage dispatch form, which otherwise swallows q.
func TestCtrlCAlwaysQuits(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m.cqDispatch = true
	m = press(m, "x") // typed into the form's repo filter
	if m.dxFilter != "x" {
		t.Fatalf("dxFilter = %q, want the key typed into the form", m.dxFilter)
	}
	if _, cmd := m.handleKey("ctrl+c"); cmd == nil {
		t.Error("ctrl+c should quit from the dispatch form")
	}
	m.settings = newSettings(nil)
	if _, cmd := m.handleKey("ctrl+c"); cmd == nil {
		t.Error("ctrl+c should quit from an overlay")
	}
}

func TestHandleKeyConfirmCancel(t *testing.T) {
	// Nothing on the triage lens opens a confirm any more (its acts fire behind
	// an 850ms flash), so the pending confirm is set up directly — the bar and
	// its y/n/esc handling are still live chrome.
	m := newModel()
	m.width, m.height = 190, 44
	m.confirm = &confirmState{label: "kill \"one\"", kind: "kill", feature: "one"}

	m = press(m, "n")
	if m.confirm != nil {
		t.Error("n should cancel the confirm")
	}
	if m.notice != "cancelled" {
		t.Errorf("notice = %q, want cancelled", m.notice)
	}

	m.confirm = &confirmState{label: "kill \"one\"", kind: "kill", feature: "one"}
	renderClean(t, m, "confirm bar")
	m = press(m, "esc")
	if m.confirm != nil {
		t.Error("esc should also cancel the confirm")
	}
}

func TestUndoKey(t *testing.T) {
	m := newModel()
	m.lens = "products"
	m.undo = "ship widget"
	mm, _ := m.handleKey("ctrl+z")
	if mm.(model).undo != "" {
		t.Error("ctrl+z should clear a pending undo")
	}
	if !strings.Contains(mm.(model).notice, "undone") {
		t.Errorf("notice = %q", mm.(model).notice)
	}
}

// Undo is a chord because `U` upgrades the machine in place: the two must not
// be one slipped shift apart. `u` is the assignment editor's unassign now, and
// must not undo anything anywhere.
func TestUndoIsAChordNotAShiftFromUpgrade(t *testing.T) {
	m := newModel()
	m.lens = "products"
	m.undo = "ship widget"
	if got := press(m, "u").undo; got != "ship widget" {
		t.Errorf("u undid something: undo = %q", got)
	}

	// The one thing the chord buys over `u`: it works while the dispatch prompt
	// owns the keyboard, because a chord cannot be part of a sentence.
	m = newModel()
	m.width, m.height = 190, 44
	if !m.cqPromptOn() {
		t.Fatal("expected the empty fleet to leave the prompt holding the keyboard")
	}
	m.undo = "ship widget"
	if mm := press(m, "ctrl+z"); mm.undo != "" || !strings.Contains(mm.notice, "undone") {
		t.Errorf("ctrl+z did not reach undo at the prompt: undo=%q notice=%q", mm.undo, mm.notice)
	}
}

// ---- backlog.go: dispatch / pick-all / source cycle --------------------------

func TestBacklogDispatchAndPickAll(t *testing.T) {
	installFixture(t)
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "3") // backlog lens, cursor 0: an untaken ticket

	// Untaken ticket: enter returns a launchCmd (m.cfg is nil, so the cmd
	// itself degrades harmlessly if ever invoked — see actions_test.go).
	next, cmd := m.handleKey("enter")
	if cmd == nil {
		t.Error("enter on an untaken ticket should return a launchCmd")
	}
	m = next.(model)

	// Move to a ticket that already has a dispatcher (fixture: index 3).
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
	next3, batch := m.handleKey("ctrl+d")
	m = next3.(model)
	if len(m.picked) != 0 {
		t.Error("ctrl+d should clear the picked set")
	}
	// The notice is present tense — the launches are in flight, not finished —
	// and there must be a real command behind it. It used to say "dispatched"
	// and return nothing at all.
	if !strings.Contains(m.notice, "dispatching") {
		t.Errorf("ctrl+d notice = %q", m.notice)
	}
	if batch == nil {
		t.Error("ctrl+d announced a dispatch but returned no command")
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
	m = press(m, "5")
	for _, k := range []string{"down", "up", "o", "right", "esc"} {
		m = press(m, k)
		renderClean(t, m, "decisions "+k)
	}
}

func TestDecisionsEmptyRepoBranch(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	decisions = map[string][]decision{"empty-repo": {}}
	decisionRepoOrder = []string{"empty-repo"}
	// pluginForRepo falls back to the plugin whose id is "builtin", so the
	// slice must carry one even in this synthetic scenario.
	plugins = []plugin{
		{id: "p0", name: "p0"}, {id: "p1", name: "p1"}, {id: "p2", name: "p2"},
		{id: "builtin", name: "cockpit records"},
	}

	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "5")
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
	installFixture(t)
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
	installFixture(t)
	m := newModel()
	m.width, m.height = 190, 44
	// The product view is a panel inside lens 2 now: 2 selects the products
	// lens, enter opens the panel on the product under the cursor.
	m = press(m, "2")
	m = press(m, "enter")
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
	// The fixture matters: with no products, enter opens nothing and the test
	// would silently stop exercising the team tab.
	installFixture(t)
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "2")
	m = press(m, "enter")
	m = press(m, "T")
	m = press(m, "j") // team tab has no key handling: should just no-op
	renderClean(t, m, "team tab j")
}

func TestProductShippedTabFull(t *testing.T) {
	installFixture(t)
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "2")
	m = press(m, "enter")
	m = press(m, "S")
	m = press(m, "j")
	m = press(m, "k")
	// `o` returns a command that really opens the PR; it used to print
	// "opening #144" and open nothing. `c` is gone — nothing implemented it.
	if _, cmd := m.updateProduct("o"); cmd == nil {
		t.Error("o should return a command that opens the pull request")
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
	// An empty prompt launches nothing and must not claim otherwise. It used to
	// announce "dispatched again" while returning no command at all.
	if !strings.Contains(m.notice, "nothing to dispatch") {
		t.Errorf("empty resume notice = %q", m.notice)
	}
}

// ---- settings.go: field editing across every kind ----------------------------

func TestSettingsEditEveryField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	saved := captureVars()
	defer restoreVars(saved)

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

func TestDoConfirmNilAndKill(t *testing.T) {
	m := newModel()
	mm, cmd := m.doConfirm()
	if cmd != nil {
		t.Error("doConfirm with nil confirm should be a no-op")
	}
	_ = mm

	m2 := newModel()
	m2.width, m2.height = 190, 44
	m2.confirm = &confirmState{label: "kill \"one\"", kind: "kill", features: []string{"one"}}
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

func TestRepoProductFallback(t *testing.T) {
	if got := repoProduct("no-such-repo-xyz"); got != "—" {
		t.Errorf("repoProduct(unknown) = %q, want —", got)
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

// ---- default-branch lenses: usage/velocity accept unhandled keys ------------

func TestPassthroughLensesIgnoreKeys(t *testing.T) {
	for _, lens := range []string{"4", "6"} {
		m := newModel()
		m.width, m.height = 190, 44
		m = press(m, lens)
		m = press(m, "j") // no updater registered: handleKey's default branch
		renderClean(t, m, "passthrough lens "+lens)
	}
}
