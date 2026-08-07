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

// TestEveryLensRenders switches through all eight lenses at every width tier and
// checks each renders within bounds.
func TestEveryLensRenders(t *testing.T) {
	for _, w := range smokeWidths {
		for i := 1; i <= 8; i++ {
			m := newModel()
			m.width, m.height = w, 44
			m = press(m, itoa(i))
			renderClean(t, m, "lens "+itoa(i)+" @"+itoa(w))
		}
	}
}

// TestFloorInteractions exercises the floor lens navigation, filtering,
// grouping, marks and overlays.
func TestFloorInteractions(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44

	for _, k := range []string{"j", "j", "l", "k", "h", "t", "t", "w", "w", "space", "M", "p", "G", "g", "D"} {
		m = press(m, k)
		renderClean(t, m, "floor key "+k)
	}
	// close diff
	m = press(m, "esc")

	// filter flow: open, type, render, escape.
	m = press(m, "/")
	for _, ch := range []string{"c", "o", "r"} {
		m = press(m, ch)
	}
	renderClean(t, m, "floor filtered")
	m = press(m, "esc")

	// help + palette overlays.
	m = press(m, "?")
	renderClean(t, m, "help")
	m = press(m, "esc")
	m = press(m, ":")
	m = press(m, "u")
	renderClean(t, m, "palette")
	m = press(m, "esc")
}

// TestFloorShipAndConfirm walks the ship confirmation and the merge animation.
func TestFloorShipAndConfirm(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "y") // opens confirm on the selected shippable dispatcher
	if m.confirm == nil {
		t.Skip("selected dispatcher is not shippable; nothing to confirm")
	}
	renderClean(t, m, "confirm bar")
	mm, _ := m.doConfirm()
	// advance the merge animation to completion.
	for i := 0; i < 20; i++ {
		next, _ := mm.advanceShip()
		mm = next
	}
	renderClean(t, mm, "post-ship")
}

// TestProductTabs cycles the review/team/shipped tabs and their overlays.
func TestProductTabs(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "2")     // products
	m = press(m, "enter") // open a product
	if m.lens != "product" {
		t.Fatalf("enter on products did not open the product lens")
	}
	for _, tab := range []string{"R", "T", "S"} {
		m = press(m, tab)
		renderClean(t, m, "product tab "+tab)
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
}

// TestBacklogAndDecisions covers the remaining interactive lenses.
func TestBacklogAndDecisions(t *testing.T) {
	m := newModel()
	m.width, m.height = 190, 44

	m = press(m, "5") // backlog
	for _, k := range []string{"j", "space", "j", "s", "s"} {
		m = press(m, k)
		renderClean(t, m, "backlog "+k)
	}

	m = press(m, "7") // decisions
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
	for _, msg := range []tea.Msg{followTickMsg{}, shipTickMsg{}, landClearMsg{}, undoClearMsg{seq: 1}, tea.WindowSizeMsg{Width: 120, Height: 40}} {
		tm, _ = tm.Update(msg)
	}
	if strings.TrimSpace(tm.(model).View()) == "" {
		t.Fatal("empty render after messages")
	}
}
