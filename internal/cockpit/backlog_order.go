package cockpit

// backlog_order.go is what the BACKLOG's table holds, and in what order:
// every open ticket, narrowed by source (`s`) and product (`p`), and grouped
// by the product it belongs to.
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
)

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
	group string
}

// blRows is the table: the tickets the source and product filters leave,
// group by group, each in the order its sources gave.
func (m model) blRows() []blRow {
	var rows []blRow
	for _, t := range backlogTickets {
		if m.srcFilter != "" && m.srcFilter != "all" && t.src != m.srcFilter {
			continue
		}
		g := blGroupOf(t)
		if m.blProduct != "" && g != m.blProduct {
			continue
		}
		rows = append(rows, blRow{t: t, group: g})
	}

	groupAt := map[string]int{}
	for i, g := range blGroups(backlogTickets) {
		groupAt[g] = i
	}
	slices.SortStableFunc(rows, func(a, b blRow) int {
		return cmp.Compare(groupAt[a.group], groupAt[b.group])
	})
	return rows
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
// filter or load finds it again.
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

// blSync keeps the cursor on the ticket it was on, wherever a filter or a load
// has put it — the id is authoritative and the index only the fallback for a
// ticket that has left the table, as on triage (fleetSync). A cursor held by
// index alone would land on a different ticket whenever a load brought the
// backlog back in another order, and enter dispatches what the cursor is on.
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

// blViewLabel is the header's account of what the table is narrowed to.
func (m model) blViewLabel() string {
	product := "every product"
	if m.blProduct != "" {
		product = m.blProduct
	}
	source := "every source"
	if m.srcFilter != "" && m.srcFilter != "all" {
		source = sourceMeta[m.srcFilter].label
	}
	return product + " · " + source
}

// blIdle is how long a ticket has sat untouched, for the table's last column:
// the time its source last saw anything happen on it.
func blIdle(t ticket) string {
	if !t.touched.IsZero() {
		return blkAge(t.touched)
	}
	return t.age
}
