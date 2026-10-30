package cockpit

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// backlogFixture installs ts as the backlog and order as the products lens's
// product order, restoring both (and the search index cut from them) after.
func backlogFixture(t *testing.T, order []string, ts []ticket) {
	t.Helper()
	prevTickets, prevOrder, prevIndex := backlogTickets, productOrder, backlogIndex
	t.Cleanup(func() { backlogTickets, productOrder, backlogIndex = prevTickets, prevOrder, prevIndex })
	backlogTickets, productOrder, backlogIndex = ts, order, nil
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

// Every order sorts inside each product group, says which end comes first, and
// turns round with O. A ticket missing the order's figure — no filed date, no
// labels, no repo — is last whichever way round, so reversing an order never
// fills the top of the table with what it can say nothing about.
func TestBacklogOrders(t *testing.T) {
	backlogFixture(t, nil, []ticket{
		{id: "r#1", pri: "low", repo: "web", filed: daysAgo(30), touched: daysAgo(1), tags: []string{"ui"}},
		{id: "r#2", pri: "urgent", repo: "api", filed: daysAgo(2), touched: daysAgo(20), tags: []string{"bug", "api"}},
		{id: "r#3", pri: "high", filed: daysAgo(10), touched: daysAgo(5)},
		{id: "r#4", pri: "med", repo: "cli"},
	})
	cases := []struct {
		by         string
		label      string
		want, back []string
	}{
		{blByPriority, "most urgent first", []string{"r#2", "r#3", "r#4", "r#1"}, []string{"r#1", "r#4", "r#3", "r#2"}},
		{blByFiled, "newest filed first", []string{"r#2", "r#3", "r#1", "r#4"}, []string{"r#1", "r#3", "r#2", "r#4"}},
		{blByStale, "stalest first", []string{"r#2", "r#3", "r#1", "r#4"}, []string{"r#1", "r#3", "r#2", "r#4"}},
		{blByLabel, "by label, a → z", []string{"r#2", "r#1", "r#3", "r#4"}, []string{"r#1", "r#2", "r#3", "r#4"}},
		{blByRepo, "by repo, a → z", []string{"r#2", "r#4", "r#1", "r#3"}, []string{"r#1", "r#4", "r#2", "r#3"}},
	}
	m := backlogModel(40)
	for _, c := range cases {
		m.blOrder = blOrder{by: c.by}
		if got := ids(m.blRows()); !slices.Equal(got, c.want) {
			t.Errorf("%s: rows = %v, want %v", c.by, got, c.want)
		}
		if got := m.blViewLabel(); !strings.HasPrefix(got, c.label) {
			t.Errorf("%s: header says %q, want it to lead with %q", c.by, got, c.label)
		}
		m.blOrder.rev = true
		if got := ids(m.blRows()); !slices.Equal(got, c.back) {
			t.Errorf("%s reversed: rows = %v, want %v", c.by, got, c.back)
		}
	}
}

// o steps through the orders and O turns the current one round; the column at
// the end of the table follows, so the order on screen can be checked against
// the figure it sorts by.
func TestBacklogOrderKeysAndTheColumnThatShowsThem(t *testing.T) {
	backlogFixture(t, nil, []ticket{
		{id: "r#1", title: "one", pri: "med", filed: daysAgo(30), touched: daysAgo(3), tags: []string{"bug"}},
	})
	m := backlogModel(40)
	var seen []string
	for range blOrders(false) {
		seen = append(seen, m.blOrderNow().by)
		m = press(m, "o")
	}
	if !slices.Equal(seen, blOrders(false)) {
		t.Errorf("o visited %v, want %v", seen, blOrders(false))
	}
	if got := m.blOrderNow(); got.by != blByPriority || got.rev {
		t.Errorf("a full turn of o is back at priority, got %+v", got)
	}
	if m = press(m, "O"); !m.blOrderNow().rev {
		t.Error("O did not turn the order round")
	}
	if m = press(m, "o"); m.blOrderNow().rev {
		t.Error("a new order starts the right way round")
	}

	for _, c := range []struct{ by, head, cell string }{
		{blByFiled, "FILED", "30d"},
		{blByLabel, "LABELS", "bug"},
		{blByStale, "IDLE", "3d"},
	} {
		m.blOrder = blOrder{by: c.by}
		out := stripAnsi(m.View())
		if !strings.Contains(out, c.head) || !strings.Contains(out, c.cell) {
			t.Errorf("%s: the column should read %s / %s:\n%s", c.by, c.head, c.cell, out)
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
	// One product, no quotes: a ticket a line under its divider, so a page of
	// lines, less three, lands on the ticket a table height less three down.
	if want := m.blTableHeight(m.bodyHeight()) - 3; m.backlogCursor != want {
		t.Errorf("pgdown moved to %d, want %d", m.backlogCursor, want)
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

// The cursor is held by the ticket, not the row. A re-sort, a filter and a load
// all move tickets about, and enter dispatches whatever the cursor is on: held
// by index, `o` would put it on a different ticket every time.
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
	if m = press(m, "o"); m.ticketSelected().id != "a#2" {
		t.Errorf("re-sorting moved the cursor to %q", m.ticketSelected().id)
	}
	if m = press(m, "O"); m.ticketSelected().id != "a#2" {
		t.Errorf("reversing moved the cursor to %q", m.ticketSelected().id)
	}

	// A load that brings the tickets back in another order.
	backlogTickets = []ticket{backlogTickets[2], backlogTickets[1], backlogTickets[0]}
	if m = m.blSearchRefresh(); m.ticketSelected().id != "a#2" {
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
	if !strings.Contains(out, "backlog · 1 of 4 open") || !strings.Contains(out, "most urgent first · alpha · github") {
		t.Errorf("filtered header:\n%s", out)
	}

	// Narrowed to nothing, the table says what emptied it and how to widen it.
	m = press(m, "s") // linear
	m = press(m, "s") // boards
	if out := stripAnsi(m.View()); !strings.Contains(out, "nothing for this product and source · p and s widen it") {
		t.Errorf("an emptied table should say why:\n%s", out)
	}
}

// Missing a figure is not the same as having the least of it.
func TestBlCompareKeepsMissingFiguresLast(t *testing.T) {
	have := blRow{t: ticket{filed: daysAgo(3), tags: []string{"a"}, repo: "x"}, tagKey: blTagKey([]string{"a"})}
	none := blRow{t: ticket{}}
	for _, by := range []string{blByFiled, blByStale, blByLabel, blByRepo} {
		if by == blByStale {
			have.t.touched = daysAgo(3)
		}
		for _, rev := range []bool{false, true} {
			if c := blCompare(by, none, have, rev); c <= 0 {
				t.Errorf("%s rev=%v: a ticket without the figure sorted first", by, rev)
			}
			if c := blCompare(by, none, none, rev); c != 0 {
				t.Errorf("%s rev=%v: two without it should tie, got %d", by, rev, c)
			}
		}
	}
	if blPriRank("urgent") >= blPriRank("high") || blPriRank("low") >= blPriRank("bogus") {
		t.Error("priority bands out of order")
	}
	if blTagKey([]string{"UI", "bug"}) != blTagKey([]string{"bug", "ui"}) {
		t.Error("labels sort the same whatever order the source listed them in")
	}
}

// Where the window's cut falls right above a product's divider, the line that
// replaces it only counts what is above: the divider below it names the
// product. And a table given no height draws nothing.
func TestBacklogCutAboveADivider(t *testing.T) {
	var ts []ticket
	for i := 1; i <= 10; i++ {
		ts = append(ts, ticket{id: "a#" + itoa(i), product: "alpha", pri: "med", filed: daysAgo(100 - i)})
	}
	for i := 1; i <= 10; i++ {
		ts = append(ts, ticket{id: "b#" + itoa(i), product: "beta", pri: "med", filed: daysAgo(100 - i)})
	}
	backlogFixture(t, []string{"alpha", "beta"}, ts)
	m := backlogModel(40)
	rows := m.blRows()
	// Lines: alpha's divider, its 10 tickets, beta's divider, beta's tickets.
	// Seven lines centred on b#2 (line 13) start at line 10, so the line after
	// the cut is beta's divider.
	m.backlogCursor = 11
	out := m.blTable(rows, 80, 7)
	if got := stripAnsi(out[0]); strings.TrimSpace(got) != "↑ 10 more tickets above" {
		t.Errorf("top line = %q", got)
	}
	if got := stripAnsi(out[1]); !strings.Contains(got, "── beta · 10") {
		t.Errorf("the divider follows the cut: %q", got)
	}
	if got := m.blTable(rows, 80, 0); got != nil {
		t.Errorf("no height, no lines: %q", got)
	}
}

// A backlog key rebound away is swallowed, as every rebound key is.
func TestBacklogKeysAreRebindable(t *testing.T) {
	m := keymapFixture(t, map[string]string{"backlog.order": "S"})
	backlogFixture(t, nil, []ticket{{id: "r#1", pri: "med"}})
	m.lens = "backlog"
	if m = press(m, "o"); m.blOrderNow().by != blByPriority {
		t.Errorf("o still re-sorts after backlog.order left it: %q", m.blOrderNow().by)
	}
	if m = press(m, "S"); m.blOrderNow().by != blByFiled {
		t.Errorf("the rebound key: %q", m.blOrderNow().by)
	}
	if got := blOrderLabel(blOrder{by: blByFiled, rev: true}); got != "oldest filed first" {
		t.Errorf("reversed label = %q", got)
	}
}

// Rebinding a list key onto a backlog key's old letter, and a backlog key onto
// a list key's, each lands where the human put it.
func TestBacklogRebindsAcrossTheLayers(t *testing.T) {
	m := keymapFixture(t, map[string]string{"list.down": "x", "backlog.product": "j"})
	backlogFixture(t, []string{"alpha"}, []ticket{
		{id: "a#1", product: "alpha", pri: "med"},
		{id: "a#2", product: "alpha", pri: "med"},
	})
	m.lens = "backlog"
	if m = press(m, "x"); m.backlogCursor != 1 || m.blProduct != "" {
		t.Errorf("x is list.down: cursor %d, product %q", m.backlogCursor, m.blProduct)
	}
	if m = press(m, "j"); m.blProduct != "alpha" || m.backlogCursor != 1 {
		t.Errorf("j is backlog.product: product %q, cursor %d", m.blProduct, m.backlogCursor)
	}
}

// The sheet prints each pair of backlog keys on one row, as it does every
// down/up and first/last pair. The page keys were named pagedown and pageup,
// which the sheet does not pair, so `?` showed pgdown and never pgup — and a
// rebound pgup was nowhere on the sheet that exists to say what the keys are.
func TestTheSheetPairsTheBacklogKeys(t *testing.T) {
	m := keymapFixture(t, map[string]string{"backlog.page.up": "ctrl+b"})
	var got []string
	for _, s := range m.helpSections() {
		if s.section != "the backlog · lens 3" {
			continue
		}
		for _, r := range s.keys {
			got = append(got, r.k)
		}
	}
	for _, want := range []string{"pgdown / ctrl+b", "g / G"} {
		if !slices.Contains(got, want) {
			t.Errorf("the backlog's section has %q, want a row %q", got, want)
		}
	}
}

// The last column can be read against the order it shows. Labels are printed
// in the order they are compared in, and a dispatched ticket keeps the figure
// the table is sorted by — "dispatched" stands in only for a figure that is not.
func TestTheColumnMatchesTheOrder(t *testing.T) {
	tk := ticket{id: "r#1", tags: []string{"ui", "Bug"}, filed: daysAgo(30), touched: daysAgo(3), taken: "out"}
	if got := blCell(tk, blOrder{by: blByLabel}); got != "Bug, ui" {
		t.Errorf("labels = %q, want them in the order they sort in", got)
	}
	if !slices.Equal(tk.tags, []string{"ui", "Bug"}) {
		t.Errorf("printing them must not reorder the ticket's own: %q", tk.tags)
	}
	backlogFixture(t, nil, []ticket{tk})
	m := backlogModel(40)
	for _, c := range []struct{ by, want string }{
		{blByPriority, "dispatched"}, {blByFiled, "30d"}, {blByLabel, "Bug, ui"}, {blByStale, "3d"},
	} {
		m.blOrder = blOrder{by: c.by}
		row := ""
		for _, l := range strings.Split(stripAnsi(m.View()), "\n") {
			if strings.Contains(l, "▸") && strings.Contains(l, "r#1") {
				row, _, _ = strings.Cut(l, "│") // the table, not the detail beside it
				break
			}
		}
		if !strings.Contains(row, c.want) {
			t.Errorf("%s: the row reads %q, want %q in it", c.by, row, c.want)
		}
	}
}

// Every line of the table is cut to the pane, the dividers and the lines that
// say what the window cut off included. One wider line widens the whole pane
// when the two panes are joined, and pushes the detail pane off the screen.
func TestTheTableNeverOutgrowsItsPane(t *testing.T) {
	long := strings.Repeat("a-very-long-product-name-", 4)
	var ts []ticket
	for i := 0; i < 30; i++ {
		ts = append(ts, ticket{id: "r#" + itoa(i), product: long, pri: "med", filed: daysAgo(60 - i)})
	}
	backlogFixture(t, []string{long}, ts)
	m := backlogModel(20)
	m = m.blTo(15)
	// A row's own columns need 59; the product name here is 100 long.
	for _, w := range []int{60, 100} {
		for i, l := range m.blTable(m.blRows(), w, 10) {
			if got := dispWidth(l); got > w+4 {
				t.Errorf("width %d, line %d is %d wide: %q", w, i, got, stripAnsi(l))
			}
		}
	}
	if got := dispWidth(blDivider(10, long)); got != 14 {
		t.Errorf("a divider at 10 is %d wide", got)
	}
}

// Every key the sheet prints with the shipped keymap fits its key column.
func TestEveryShippedKeyLabelFitsTheSheet(t *testing.T) {
	m := backlogModel(40)
	for _, s := range m.helpSections() {
		for _, r := range s.keys {
			if dispWidth(r.k) > helpKeyCol {
				t.Errorf("%s: %q is %d wide, the key column %d", s.section, r.k, dispWidth(r.k), helpKeyCol)
			}
		}
	}
}
