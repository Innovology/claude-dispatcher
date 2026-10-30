package cockpit

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// backlogFixture installs ts as the backlog and order as the products lens's
// product order, restoring both after.
func backlogFixture(t *testing.T, order []string, ts []ticket) {
	t.Helper()
	prevTickets, prevOrder := backlogTickets, productOrder
	t.Cleanup(func() { backlogTickets, productOrder = prevTickets, prevOrder })
	backlogTickets, productOrder = ts, order
}

// backlogModel is a cockpit on the backlog lens, sized to show the detail pane.
func backlogModel(h int) model {
	m := newModel()
	m.width, m.height = 190, h
	return press(m, "3")
}

func ids(rows []blRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.t.id
	}
	return out
}

// daysAgo is n days before now, and a minute more, so blkAge reads it as n days
// whenever the test runs.
func daysAgo(n int) time.Time { return time.Now().Add(-time.Duration(n)*24*time.Hour - time.Minute) }

// The table is grouped by product, in the products lens's own order — so lens 2
// and lens 3 list products the same way — then any product that lens does not
// list (a Linear token can name one with no repos), then what no product
// claims, last, as everywhere else.
func TestBacklogGroupsByProductInTheProductsLensOrder(t *testing.T) {
	backlogFixture(t, []string{"zeta", "alpha", "unassigned"}, []ticket{
		{id: "a#1", product: "alpha", pri: "urgent"},
		{id: "n#1", product: "", pri: "urgent"},
		{id: "g#1", product: "gamma", pri: "med"},
		{id: "z#1", product: "zeta", pri: "low"},
		{id: "a#2", product: "alpha", pri: "med"},
	})
	m := backlogModel(40)
	got := ids(m.blRows())
	want := []string{"z#1", "a#1", "a#2", "g#1", "n#1"}
	if !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if g := blGroups(backlogTickets); !slices.Equal(g, []string{"zeta", "alpha", "gamma", "unassigned"}) {
		t.Errorf("groups = %v", g)
	}

	// Each group is introduced by a divider that names it and counts it.
	out := stripAnsi(m.View())
	for _, want := range []string{"── zeta · 1", "── alpha · 2", "── gamma · 1", "── unassigned · 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("no divider %q in:\n%s", want, out)
		}
	}
}

// The table is windowed on the cursor. It used to draw every ticket from the
// top and stop at the pane's edge, so a backlog longer than the screen read as
// the whole of it and j walked the cursor off into rows nobody could see.
func TestBacklogScrollsWithTheCursor(t *testing.T) {
	var ts []ticket
	for i := 1; i <= 60; i++ {
		ts = append(ts, ticket{id: "r#" + itoa(i), title: "ticket " + itoa(i), pri: "med", filed: daysAgo(61 - i)})
	}
	backlogFixture(t, nil, ts)
	m := backlogModel(24)

	out := stripAnsi(m.View())
	if !strings.Contains(out, "ticket 1 ") || strings.Contains(out, "ticket 60") {
		t.Fatalf("the top of the table should be on screen, not the end:\n%s", out)
	}
	if !strings.Contains(out, "more tickets below") {
		t.Errorf("a cut table must say what is below it:\n%s", out)
	}

	m = press(m, "G")
	out = stripAnsi(m.View())
	if !strings.Contains(out, "ticket 60") || strings.Contains(out, "ticket 1 ") {
		t.Errorf("G should scroll to the last ticket:\n%s", out)
	}
	if !strings.Contains(out, "more tickets above") {
		t.Errorf("a cut table must say what is above it:\n%s", out)
	}
	// The divider naming the product has scrolled away, so the line saying what
	// is above names it instead.
	if !strings.Contains(out, "── unassigned · ↑") {
		t.Errorf("the top line should carry the product on screen:\n%s", out)
	}
	if got := m.ticketSelected().id; got != "r#60" {
		t.Errorf("G selected %q", got)
	}

	m = press(m, "g")
	if got := m.ticketSelected().id; got != "r#1" {
		t.Errorf("g selected %q", got)
	}
	m = press(m, "pgdown")
	if m.backlogCursor != m.blPage() {
		t.Errorf("pgdown moved to %d, want a page (%d)", m.backlogCursor, m.blPage())
	}
	if out := stripAnsi(m.View()); !strings.Contains(out, "▸ · "+m.ticketSelected().id) {
		t.Errorf("the cursor's ticket must be on screen after pgdown:\n%s", out)
	}
	m = press(m, "pgup")
	if m.backlogCursor != 0 {
		t.Errorf("pgup moved to %d, want 0", m.backlogCursor)
	}
	for _, w := range smokeWidths {
		m.width = w
		renderClean(t, press(m, "G"), "backlog scrolled @"+itoa(w))
	}
}

// The cursor is held by the ticket, not the row. A filter and a load both move
// tickets about, and enter dispatches whatever the cursor is on.
func TestBacklogCursorStaysOnItsTicket(t *testing.T) {
	backlogFixture(t, []string{"alpha", "beta"}, []ticket{
		{id: "a#1", product: "alpha", pri: "urgent", filed: daysAgo(1)},
		{id: "a#2", product: "alpha", pri: "low", filed: daysAgo(9)},
		{id: "b#1", product: "beta", pri: "med", filed: daysAgo(5)},
	})
	m := backlogModel(40)
	m = press(m, "j") // a#2
	if got := m.ticketSelected().id; got != "a#2" {
		t.Fatalf("selected %q", got)
	}

	// A load that brings the tickets back in another order.
	backlogTickets = []ticket{backlogTickets[2], backlogTickets[1], backlogTickets[0]}
	if m = m.blSync(); m.ticketSelected().id != "a#2" {
		t.Errorf("a load moved the cursor to %q", m.ticketSelected().id)
	}

	// A filter that leaves it out lands on what is there, and the cursor is
	// keyed to that ticket from then on.
	m = press(m, "p") // alpha
	m = press(m, "p") // beta
	if m.blProduct != "beta" || m.ticketSelected().id != "b#1" {
		t.Errorf("product %q, selected %q", m.blProduct, m.ticketSelected().id)
	}
	if m = press(m, "p"); m.blProduct != "" || m.ticketSelected().id != "b#1" {
		t.Errorf("p after the last product is every product again, on the same ticket: %q, %q", m.blProduct, m.ticketSelected().id)
	}
}

// p narrows to one product at a time, and the header never lets a filter left
// on read as the size of the backlog.
func TestBacklogProductAndSourceFilters(t *testing.T) {
	backlogFixture(t, []string{"alpha", "beta"}, []ticket{
		{id: "a#1", src: "gh", product: "alpha", pri: "med"},
		{id: "ENG-1", src: "lin", product: "alpha", pri: "med"},
		{id: "b#1", src: "gh", product: "beta", pri: "med"},
		{id: "AB#1", src: "ado", pri: "med"},
	})
	m := backlogModel(40)
	if out := stripAnsi(m.View()); !strings.Contains(out, "backlog · 4 open") || !strings.Contains(out, "every product · every source") {
		t.Errorf("unfiltered header:\n%s", out)
	}
	var visited []string
	for i := 0; i < 4; i++ {
		m = press(m, "p")
		visited = append(visited, m.blProduct)
	}
	if !slices.Equal(visited, []string{"alpha", "beta", "unassigned", ""}) {
		t.Errorf("p visited %q", visited)
	}

	m = press(m, "p") // alpha
	if got := ids(m.blRows()); !slices.Equal(got, []string{"a#1", "ENG-1"}) {
		t.Errorf("alpha rows = %v", got)
	}
	m = press(m, "s") // github
	if got := ids(m.blRows()); !slices.Equal(got, []string{"a#1"}) {
		t.Errorf("alpha on github = %v", got)
	}
	out := stripAnsi(m.View())
	if !strings.Contains(out, "backlog · 1 of 4 open") || !strings.Contains(out, "alpha · github") {
		t.Errorf("filtered header:\n%s", out)
	}

	// Narrowed to nothing, the table says what emptied it and how to widen it.
	m = press(m, "s") // linear
	m = press(m, "s") // boards
	if out := stripAnsi(m.View()); !strings.Contains(out, "nothing for this product and source · p and s widen it") {
		t.Errorf("an emptied table should say why:\n%s", out)
	}
}
