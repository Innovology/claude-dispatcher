package cockpit

// backlog_order.go is what the BACKLOG's table holds, and in what order:
// every open ticket, narrowed by source (`s`) and product (`p`), grouped by
// the product it belongs to, and each group read in the order `o` chose.
//
// The groups are products because a product is how this cockpit groups
// everything — the products lens, triage's product column, the dispatch
// form's repo picker — and a backlog of a few hundred tickets across a dozen
// repos is unreadable as one list. They come in the products lens's own order,
// so lens 2 and lens 3 list them the same way, with the tickets no product
// claims last, under "unassigned", as everywhere else.

import (
	"cmp"
	"slices"
	"strings"
)

// blOrder is an order the backlog can be read in, and whether it is turned
// round.
type blOrder struct {
	by  string // one of blOrders; "" is the default
	rev bool
}

const (
	blByPriority = "priority"
	blByFiled    = "filed"
	blByStale    = "stale"
	blByLabel    = "label"
	blByRepo     = "repo"
)

// blOrders is what `o` steps through.
var blOrders = []string{blByPriority, blByFiled, blByStale, blByLabel, blByRepo}

// blOrderNow is the order the table is drawn in: priority until `o` says
// otherwise.
func (m model) blOrderNow() blOrder {
	o := m.blOrder
	if o.by == "" {
		o.by = blByPriority
	}
	return o
}

// blNextOrder is the order after o in blOrders, the right way round.
func blNextOrder(o blOrder) blOrder {
	i := slices.Index(blOrders, o.by)
	return blOrder{by: blOrders[(i+1)%len(blOrders)]}
}

// blOrderLabel is how the header names an order. Each says which end comes
// first, since "filed" alone does not say whether new or old is on top.
func blOrderLabel(o blOrder) string {
	words := map[string][2]string{
		blByPriority: {"most urgent first", "least urgent first"},
		blByFiled:    {"newest filed first", "oldest filed first"},
		blByStale:    {"stalest first", "freshest first"},
		blByLabel:    {"by label, a → z", "by label, z → a"},
		blByRepo:     {"by repo, a → z", "by repo, z → a"},
	}[o.by]
	if o.rev {
		return words[1]
	}
	return words[0]
}

// blUnassigned is the group of a ticket no product claims.
const blUnassigned = "unassigned"

// blGroupOf is the product group a ticket is listed under.
func blGroupOf(t ticket) string {
	if t.product == "" {
		return blUnassigned
	}
	return t.product
}

// blGroups is the product groups the tickets fall in, in the order they are
// drawn: the products lens's order, then any product that lens does not list
// (a Linear token can name one with no repos), by name, then unassigned.
func blGroups(ts []ticket) []string {
	present := map[string]bool{}
	for _, t := range ts {
		present[blGroupOf(t)] = true
	}
	var out []string
	for _, p := range productOrder {
		if p != blUnassigned && present[p] {
			out = append(out, p)
			delete(present, p)
		}
	}
	var rest []string
	for p := range present {
		if p != blUnassigned {
			rest = append(rest, p)
		}
	}
	slices.Sort(rest)
	out = append(out, rest...)
	if present[blUnassigned] {
		out = append(out, blUnassigned)
	}
	return out
}

// blRow is one ticket on the table.
type blRow struct {
	t     ticket
	at    int // its index in backlogTickets
	group string
}

// blRows is the table: the tickets the source and product filters leave,
// group by group, each group in the order chosen.
func (m model) blRows() []blRow {
	var rows []blRow
	for i, t := range backlogTickets {
		if m.srcFilter != "" && m.srcFilter != "all" && t.src != m.srcFilter {
			continue
		}
		g := blGroupOf(t)
		if m.blProduct != "" && g != m.blProduct {
			continue
		}
		rows = append(rows, blRow{t: t, at: i, group: g})
	}

	o := m.blOrderNow()
	groupAt := map[string]int{}
	for i, g := range blGroups(backlogTickets) {
		groupAt[g] = i
	}
	slices.SortStableFunc(rows, func(a, b blRow) int {
		if c := cmp.Compare(groupAt[a.group], groupAt[b.group]); c != 0 {
			return c
		}
		if c := blCompare(o.by, a, b, o.rev); c != 0 {
			return c
		}
		// Ties go to the more urgent, then to the one that has waited longest,
		// then to the order the sources gave — so a redraw never reshuffles
		// tickets that tie.
		if c := blCompare(blByPriority, a, b, false); c != 0 {
			return c
		}
		if c := blCompare(blByFiled, a, b, true); c != 0 {
			return c
		}
		return cmp.Compare(a.at, b.at)
	})
	return rows
}

// blCompare orders a and b by one order's own figure: negative when a comes
// first.
//
// A ticket missing the figure — no filed date, no labels, no repo — comes after
// every ticket that has it, whichever way round the order is. Turning an order
// round must not fill the top of the table with the tickets it can say nothing
// about: an Azure item with no labels is not "the last label, alphabetically".
func blCompare(by string, a, b blRow, rev bool) int {
	var am, bm bool
	c := 0
	switch by {
	case blByPriority:
		c = cmp.Compare(blPriRank(a.t.pri), blPriRank(b.t.pri))
	case blByFiled:
		am, bm = a.t.filed.IsZero(), b.t.filed.IsZero()
		c = b.t.filed.Compare(a.t.filed) // newest first
	case blByStale:
		am, bm = a.t.touched.IsZero(), b.t.touched.IsZero()
		c = a.t.touched.Compare(b.t.touched) // longest untouched first
	case blByLabel:
		am, bm = len(a.t.tags) == 0, len(b.t.tags) == 0
		c = strings.Compare(blTagKey(a.t.tags), blTagKey(b.t.tags))
	case blByRepo:
		am, bm = a.t.repo == "", b.t.repo == ""
		c = strings.Compare(strings.ToLower(a.t.repo), strings.ToLower(b.t.repo))
	}
	switch {
	case am && bm:
		return 0
	case am:
		return 1
	case bm:
		return -1
	}
	if rev {
		return -c
	}
	return c
}

// blPriRank is a priority band's place, most urgent first.
func blPriRank(p string) int {
	switch p {
	case "urgent":
		return 0
	case "high":
		return 1
	case "med":
		return 2
	case "low":
		return 3
	}
	return 4
}

// blTagKey is a ticket's labels as one sortable string: lowercased and in
// order, so two tickets labelled "bug" and "ui" sort together whichever order
// their source listed them in.
func blTagKey(tags []string) string {
	low := make([]string, len(tags))
	for i, t := range tags {
		low[i] = strings.ToLower(t)
	}
	slices.Sort(low)
	return strings.Join(low, "\x00")
}

// backlogList is the tickets on the table, in the order they are drawn.
func (m model) backlogList() []ticket {
	rows := m.blRows()
	out := make([]ticket, len(rows))
	for i, r := range rows {
		out[i] = r.t
	}
	return out
}

// ticketSelected returns the ticket under the (clamped) backlog cursor, or a
// placeholder when the table is empty.
func (m model) ticketSelected() ticket {
	l := m.backlogList()
	if len(l) == 0 {
		return ticket{title: "no open tickets", body: m.blEmpty(), pri: "med"}
	}
	return l[clampCursor(m.backlogCursor, len(l))]
}

// blEmpty says why the table is empty, which is a different sentence for each
// thing that can empty it: a backlog with nothing in it is not a filter that
// matched nothing.
func (m model) blEmpty() string {
	if len(backlogTickets) == 0 {
		return "nothing in the backlog · github issues, linear and azure boards assigned to you show here"
	}
	return "nothing for this product and source · " + prettyKey(m.keys.KeyFor("backlog.product")) +
		" and " + prettyKey(m.keys.KeyFor("backlog.source")) + " widen it"
}

// blTo moves the cursor to row i and keys it to that ticket, so the next
// re-sort, filter or load finds it again.
func (m model) blTo(i int) model {
	rows := m.blRows()
	if len(rows) == 0 {
		m.backlogCursor, m.blSelID = 0, ""
		return m
	}
	m.backlogCursor = clampCursor(i, len(rows))
	m.blSelID = rows[m.backlogCursor].t.id
	return m
}

// blSync keeps the cursor on the ticket it was on, wherever a re-sort, a
// filter or a load has put it — the id is authoritative and the index only
// the fallback for a ticket that has left the table, as on triage (fleetSync).
// A cursor held by index alone would land on a different ticket after every
// `o`, and enter dispatches what the cursor is on.
func (m model) blSync() model {
	rows := m.blRows()
	if m.blSelID != "" {
		for i, r := range rows {
			if r.t.id == m.blSelID {
				m.backlogCursor = i
				return m
			}
		}
	}
	return m.blTo(m.backlogCursor)
}

// blProducts is what `p` steps through: every product group the backlog has
// tickets in, whatever the source filter, in the order they are drawn.
func blProducts() []string { return blGroups(backlogTickets) }

// blViewLabel is the header's account of what the table is showing: its order,
// and what it is narrowed to.
func (m model) blViewLabel() string {
	product := "every product"
	if m.blProduct != "" {
		product = m.blProduct
	}
	source := "every source"
	if m.srcFilter != "" && m.srcFilter != "all" {
		source = sourceMeta[m.srcFilter].label
	}
	return blOrderLabel(m.blOrderNow()) + " · " + product + " · " + source
}

// blColumn is the table's last column for an order: the figure it sorts by,
// so the order on screen can be checked at a glance. Labels when sorted by
// label, when it was filed when sorted by that, and otherwise how long it has
// sat untouched — the figure that column has always shown.
func blColumn(o blOrder) string {
	switch o.by {
	case blByFiled:
		return "FILED"
	case blByLabel:
		return "LABELS"
	}
	return "IDLE"
}

// blCell is a ticket's value in that column.
func blCell(t ticket, o blOrder) string {
	switch o.by {
	case blByFiled:
		return blkAge(t.filed)
	case blByLabel:
		return strings.Join(t.tags, ", ")
	}
	return blIdle(t)
}

// blIdle is how long a ticket has sat untouched, for the table's last column:
// the time its source last saw anything happen on it.
func blIdle(t ticket) string {
	if !t.touched.IsZero() {
		return blkAge(t.touched)
	}
	return t.age
}
