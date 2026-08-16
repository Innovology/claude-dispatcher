package cockpit

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// widths span the three responsive tiers: narrow (<110), standard (110–170)
// and wide (≥170).
var smokeWidths = []int{80, 130, 190}

// press routes a key string through the model the way the program would.
func press(m model, k string) model {
	next, _ := m.handleKey(k)
	return next.(model)
}

// renderClean asserts View produces output and never exceeds the terminal
// height (a pane overflowing its box would corrupt the alt-screen).
func renderClean(t *testing.T, m model, ctx string) {
	t.Helper()
	out := m.View()
	if strings.TrimSpace(out) == "" {
		t.Fatalf("%s: empty render", ctx)
	}
	if got := strings.Count(out, "\n") + 1; got > m.height {
		t.Fatalf("%s: render is %d lines, exceeds height %d", ctx, got, m.height)
	}
}

// TestEveryLensRenders switches through all six lenses at every width tier and
// checks each renders within bounds. The product panel is not on a digit any
// more, so it gets its own sweep below.
func TestEveryLensRenders(t *testing.T) {
	for _, w := range smokeWidths {
		for i := 1; i <= 6; i++ {
			m := newModel()
			m.width, m.height = w, 44
			m = press(m, itoa(i))
			renderClean(t, m, "lens "+itoa(i)+" @"+itoa(w))
		}
	}
}

// TestChromeOverlaysFromTriage walks the overlays reachable from the triage
// lens. Its own keys are covered in cq_test.go; what matters here is that the
// palette and the help sheet still open over it and render.
func TestChromeOverlaysFromTriage(t *testing.T) {
	installFleetFixture(t) // a non-empty fleet, so the prompt does not eat the keys
	m := newModel()
	m.width, m.height = 190, 44

	m = press(m, "?")
	renderClean(t, m, "help")
	m = press(m, "esc")

	m = press(m, ":")
	m = press(m, "u")
	renderClean(t, m, "palette")
	m = press(m, "esc")

	m = press(m, "w")
	renderClean(t, m, "running only")
	m = press(m, "w")
}

// TestShipConfirmAndMergeAnimation walks the ship confirmation and the merge
// animation. No key opens a ship confirm on the triage lens now, so the pending
// confirm is set up directly.
func TestShipConfirmAndMergeAnimation(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m.confirm = &confirmState{
		label: "ship \"one\" to production?", kind: "ship", feature: "one", repo: "alpha-api",
	}
	renderClean(t, m, "confirm bar")
	mm, _ := m.doConfirm()
	if mm.shipFx == nil {
		t.Fatal("confirming a ship should start the merge animation")
	}
	for i := 0; i < 20; i++ {
		next, _ := mm.advanceShip()
		mm = next
	}
	if mm.shipFx != nil || mm.justLanded != "one" {
		t.Errorf("the animation should finish and land: fx=%+v landed=%q", mm.shipFx, mm.justLanded)
	}
	renderClean(t, mm, "post-ship")
}

// TestProductTabs cycles the review/team/shipped tabs and their overlays.
func TestProductTabs(t *testing.T) {
	installFixture(t) // there has to be a product to open
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "2")     // products
	m = press(m, "enter") // open a product
	if m.lens != "product" {
		t.Fatalf("enter on products did not open the product lens")
	}
	if m.rightTab != "overview" {
		t.Errorf("enter should open the panel on overview, got %q", m.rightTab)
	}
	for _, tab := range []string{"R", "T", "S", "H", "O"} {
		m = press(m, tab)
		renderClean(t, m, "product tab "+tab)
	}
	// j on the overview tab is a no-op, not a hidden walk of the shipped cursor.
	before := m.shipCursor
	m = press(m, "j")
	if m.shipCursor != before {
		t.Errorf("j on overview moved shipCursor to %d", m.shipCursor)
	}
	// review overlay
	m = press(m, "R")
	m = press(m, "enter")
	renderClean(t, m, "review or list")
	m = press(m, "esc")
	// shipped → resume overlay
	m = press(m, "S")
	m = press(m, "enter")
	renderClean(t, m, "resume or list")
	m = press(m, "esc")
	// history → the same overlay, on a dispatcher that never shipped
	m = press(m, "H")
	m = press(m, "enter")
	renderClean(t, m, "history resume or list")
	m = press(m, "esc")
	// esc with no overlay up closes the panel and leaves the products lens
	// showing, which is the only way back out — q is quit, not close.
	m = press(m, "esc")
	if m.lens != "products" {
		t.Errorf("esc should close the panel, lens = %q", m.lens)
	}
}

// TestProductPanelRendersAtEveryWidth covers the panel itself: it is off the
// digits, so the lens sweep above never reaches it.
func TestProductPanelRendersAtEveryWidth(t *testing.T) {
	installFixture(t)
	for _, w := range smokeWidths {
		for _, tab := range []string{"O", "R", "T", "S", "H"} {
			m := newModel()
			m.width, m.height = w, 44
			m = press(m, "2")
			m = press(m, "enter")
			m = press(m, tab)
			renderClean(t, m, "product panel "+tab+" @"+itoa(w))
		}
	}
}

// TestBacklogAndDecisions covers the remaining interactive lenses.
func TestBacklogAndDecisions(t *testing.T) {
	installFixture(t)
	m := newModel()
	m.width, m.height = 190, 44

	m = press(m, "3") // backlog
	for _, k := range []string{"j", "space", "j", "s", "s"} {
		m = press(m, k)
		renderClean(t, m, "backlog "+k)
	}

	m = press(m, "5") // decisions
	for _, k := range []string{"j", "J", "l", "a", "s", "e", "h", "K"} {
		m = press(m, k)
		renderClean(t, m, "decisions "+k)
	}
}

// TestAnimationMessagesDoNotPanic feeds the timer messages through Update.
func TestAnimationMessagesDoNotPanic(t *testing.T) {
	m := newModel()
	m.width, m.height = 130, 40
	var tm tea.Model = m
	for _, msg := range []tea.Msg{shipTickMsg{}, landClearMsg{}, undoClearMsg{seq: 1}, cqFlashMsg{seq: 1}, tea.WindowSizeMsg{Width: 120, Height: 40}} {
		tm, _ = tm.Update(msg)
	}
	if strings.TrimSpace(tm.(model).View()) == "" {
		t.Fatal("empty render after messages")
	}
}
