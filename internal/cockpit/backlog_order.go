package cockpit

// backlog_order.go is what the BACKLOG's table holds, and in what order:
// every open ticket, narrowed by source (`s`), product (`p`) and the `/`
// query, grouped by the product it belongs to, and each group read in the
// order `o` chose.
//
// The groups are products because a product is how this cockpit groups
// everything — the products lens, triage's product column, the dispatch
// form's repo picker — and a backlog of a few hundred tickets across a dozen
// repos is unreadable as one list. They come in the products lens's own order,
// so lens 2 and lens 3 list them the same way, with the tickets no product
// claims last, under "unassigned", as everywhere else.

import (
	"cmp"
	"maps"
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
	blByMatch    = "match"
)

// blOrders is what `o` steps through. Best match leads while a query is up and
// is not offered otherwise: with nothing searched, there is nothing to match.
func blOrders(searching bool) []string {
	orders := []string{blByPriority, blByFiled, blByStale, blByLabel, blByRepo}
	if searching {
		return append([]string{blByMatch}, orders...)
	}
	return orders
}

// blOrderNow is the order the table is drawn in.
//
// There are two, one for browsing and one for a search, because a search reads
// best first and esc should hand back the order you left rather than whatever
// you sorted the hits by. Each defaults on its own: priority while browsing,
// best match while searching.
func (m model) blOrderNow() blOrder {
	if m.blSearching() {
		o := m.blHitOrder
		if o.by == "" {
			o.by = blByMatch
		}
		return o
	}
	o := m.blOrder
	if o.by == "" {
		o.by = blByPriority
	}
	return o
}

// blSetOrder is blOrderNow's setter: the search's order while one is up, the
// browsing order otherwise.
func (m model) blSetOrder(o blOrder) model {
	if m.blSearching() {
		m.blHitOrder = o
	} else {
		m.blOrder = o
	}
	return m
}

// blNextOrder is the order after o in blOrders, the right way round.
func blNextOrder(o blOrder, searching bool) blOrder {
	orders := blOrders(searching)
	i := slices.Index(orders, o.by)
	return blOrder{by: orders[(i+1)%len(orders)]}
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
		blByMatch:    {"best match first", "weakest match first"},
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
	at    int // its index in backlogTickets, which is what a search hit names
	group string
	// hit is what the query found on it: its score, and how many of the
	// query's words matched best in its id or title. Zero with no query.
	hit textHit
	// tagKey is its labels as the label order compares them (blTagKey), built
	// once a row rather than twice a comparison.
	tagKey string
}

// blRows is the table: the tickets the source, product and query leave, group
// by group, each group in the order chosen.
func (m model) blRows() []blRow {
	found := m.blFoundNow()
	var rows []blRow
	for i, t := range backlogTickets {
		if m.srcFilter != "" && m.srcFilter != "all" && t.src != m.srcFilter {
			continue
		}
		g := blGroupOf(t)
		if m.blProduct != "" && g != m.blProduct {
			continue
		}
		r := blRow{t: t, at: i, group: g, tagKey: blTagKey(t.tags)}
		if found != nil {
			h, hit := found.hits[i]
			if !hit {
				continue
			}
			r.hit = h
		}
		rows = append(rows, r)
	}

	o := m.blOrderNow()
	groupAt := map[string]int{}
	for i, g := range blGroups(backlogTickets) {
		groupAt[g] = i
	}
	// Read best first, the groups go by their best hit too. Otherwise "#123"
	// would put the 1230s of the first product above the one ticket it names,
	// in the third — and the cursor, which a search puts on the top row, on a
	// ticket that is not the answer.
	if o.by == blByMatch && !o.rev {
		best := map[string]blRow{}
		for _, r := range rows {
			if b, ok := best[r.group]; !ok || blCompare(blByMatch, r, b, false) < 0 {
				best[r.group] = r
			}
		}
		groups := slices.Collect(maps.Keys(best))
		slices.SortStableFunc(groups, func(a, b string) int {
			if c := blCompare(blByMatch, best[a], best[b], false); c != 0 {
				return c
			}
			return cmp.Compare(groupAt[a], groupAt[b])
		})
		for i, g := range groups {
			groupAt[g] = i
		}
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
		am, bm = a.tagKey == "", b.tagKey == ""
		c = strings.Compare(a.tagKey, b.tagKey)
	case blByRepo:
		am, bm = a.t.repo == "", b.t.repo == ""
		c = strings.Compare(strings.ToLower(a.t.repo), strings.ToLower(b.t.repo))
	case blByMatch:
		// Best first: the higher score, then the more of the query in the title.
		if c = cmp.Compare(b.hit.score, a.hit.score); c == 0 {
			c = cmp.Compare(b.hit.inHead, a.hit.inHead)
		}
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

// blFoundNow is what the query found, for the backlog on screen: the search
// the last keystroke ran, or — where a load has since replaced the backlog it
// ran over — the same query run again, so a hit can never name the wrong row.
func (m model) blFoundNow() *blFound {
	if !m.blSearching() {
		return nil
	}
	if f := m.blFound; f != nil && f.ix == currentBlIndex() {
		return f
	}
	return currentBlIndex().search(m.blQuery)
}

// ticketSelected returns the ticket under the (clamped) backlog cursor, or a
// placeholder when the table is empty.
func (m model) ticketSelected() ticket { return m.blTicketIn(m.blRows()) }

// blTicketIn is ticketSelected for rows already in hand, so a key or a frame
// that has the table does not filter, group and sort it again to read one
// ticket off it.
func (m model) blTicketIn(rows []blRow) ticket {
	if r, ok := blSelectedRow(rows, m.backlogCursor); ok {
		return r.t
	}
	return ticket{title: "no open tickets", body: m.blEmpty(), pri: "med"}
}

// blEmpty says why the table is empty, which is a different sentence for each
// thing that can empty it: a backlog with nothing in it is not a filter that
// matched nothing.
func (m model) blEmpty() string {
	switch {
	case len(backlogTickets) == 0:
		return "nothing in the backlog · github issues, linear and azure boards assigned to you show here"
	case m.blSearching() && m.blHitsElsewhere() == 0:
		return "no ticket matches /" + m.blQuery + " · esc clears the search"
	}
	// The query matches, or there is none: what emptied the table is the
	// product and source it is narrowed to, and esc would clear the wrong thing.
	return "nothing for this product and source · " + prettyKey(m.keys.KeyFor("backlog.product")) +
		" and " + prettyKey(m.keys.KeyFor("backlog.source")) + " widen it"
}

// blHitsElsewhere is how many tickets the query matches across the whole
// backlog, whatever product and source the table is narrowed to: what says
// whether an empty table is the query's doing or the filters'.
func (m model) blHitsElsewhere() int {
	if f := m.blFoundNow(); f != nil {
		return len(f.hits)
	}
	return 0
}

// blTo moves the cursor to row i and keys it to that ticket, so the next
// re-sort, filter or load finds it again.
func (m model) blTo(i int) model { return m.blToIn(m.blRows(), i) }

// blToIn is blTo for rows already in hand.
func (m model) blToIn(rows []blRow, i int) model {
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
	return m.blToIn(rows, m.backlogCursor)
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

// blCell is a ticket's value in that column. Labels are printed in the order
// the label order compares them (blTagKey), so a ticket labelled "ui, bug"
// does not read as out of place among the "bug"s it sorts with.
func blCell(t ticket, o blOrder) string {
	switch o.by {
	case blByFiled:
		return blkAge(t.filed)
	case blByLabel:
		tags := slices.Clone(t.tags)
		slices.SortFunc(tags, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
		return strings.Join(tags, ", ")
	}
	return blIdle(t)
}

// blColumnSorts is whether the last column is the figure the order sorts by.
// IDLE is that only for the stalest order; under priority, repo or best match
// it is a figure beside the order.
func blColumnSorts(o blOrder) bool {
	return o.by == blByFiled || o.by == blByLabel || o.by == blByStale
}

// blIdle is how long a ticket has sat untouched, for the table's last column:
// the time its source last saw anything happen on it, worked out when it is
// drawn, or blank when the source did not say.
func blIdle(t ticket) string { return blkAge(t.touched) }
