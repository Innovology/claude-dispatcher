package cockpit

// backlog.go is lens 3 — the BACKLOG: open tickets pulled from GitHub Issues,
// Linear and Azure Boards into one dispatch queue. The left pane is the ticket
// table (pick with space, dispatch with enter); the right pane is the selected
// ticket's detail plus the "dispatch as" preview and the picked list.
//
// What the table holds — grouped by product, narrowed by source and product —
// is backlog_order.go. This file draws it and handles its keys. The table is
// windowed on the cursor, because a backlog is routinely longer than the
// screen and a table that stopped at the bottom edge read as the whole of it,
// with j walking the cursor off into rows nobody could see.

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/account"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/keymap"
)

// backlogSlug lowercases id and collapses every run of non-alphanumerics to a
// single dash — the design's t.id.toLowerCase().replace(/[^a-z0-9]+/g,'-').
func backlogSlug(id string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return b.String()
}

// backlogPad pads an already-coloured string to exactly w columns, truncating
// (ANSI-safe) when it overflows.
func backlogPad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	d := dispWidth(s)
	if d < w {
		return s + strings.Repeat(" ", w-d)
	}
	if d > w {
		return truncateAnsi(s, w)
	}
	return s
}

// backlogSpread places left and right on one line of width w, with a pad gutter
// on both edges and the gap between them filled. Same shape as spread() but
// parametrised by width so each pane can use its own.
func backlogSpread(left, right string, w int) string {
	inner := w - 2*pad
	if inner < 1 {
		inner = w
	}
	g := strings.Repeat(" ", pad)
	lw, rw := dispWidth(left), dispWidth(right)
	if lw+1+rw > inner {
		return g + backlogPad(truncateAnsi(left, inner), inner) + g
	}
	gap := inner - lw - rw
	return g + left + strings.Repeat(" ", gap) + right + g
}

// backlogWrap greedily word-wraps plain text s into lines of at most w columns.
func backlogWrap(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := ""
	for _, word := range words {
		switch {
		case cur == "":
			cur = word
		case dispWidth(cur)+1+dispWidth(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// backlogWhere is the ticket's "product / repo · labels" line, with the clauses
// it does not have left out. Only GitHub issues resolve to a repo (they are
// found per repo); a Linear ticket names the product whose token it came back
// on and no repo, an Azure one neither, and the line used to render as
// " / · In Progress".
func backlogWhere(t ticket) string {
	var parts []string
	switch {
	case t.product != "" && t.repo != "":
		parts = append(parts, t.product+" / "+t.repo)
	case t.repo != "":
		parts = append(parts, t.repo)
	case t.product != "":
		parts = append(parts, t.product)
	}
	if t.labels != "" {
		parts = append(parts, t.labels)
	}
	return strings.Join(parts, " · ")
}

// viewBacklog renders the backlog table (left) and the selected-ticket detail
// (right). Narrow terminals collapse to the table only.

// viewBacklog renders the backlog table (left) and the selected-ticket detail
// (right). Narrow terminals collapse to the table only.
func (m model) viewBacklog(w, h int) string {
	rows := m.blRows()
	pickedCount := len(m.picked)
	showDetail := m.fit().showDetail

	leftW := w
	rightW := 0
	if showDetail {
		leftW = w * 58 / 100
		rightW = w - leftW - 1 // 1 col for the vertical rule
	}

	left := m.backlogLeft(rows, pickedCount, leftW, h)
	if !showDetail {
		return clampLines(left, h)
	}
	right := m.backlogRight(rows, pickedCount, rightW, h)
	out := hjoin(padBlockTo(left, h), vrule(h, cRule), padBlockTo(right, h))
	return clampLines(out, h)
}

// blFooterLines is how many lines the table pane's footer takes.
const blFooterLines = 2

// blHeadLines is how many lines sit above the table: the title and the column
// heads.
const blHeadLines = 2

// blTableHeight is how many lines the table gets in a pane h tall.
func (m model) blTableHeight(h int) int { return maxi(h-blHeadLines-blFooterLines, 0) }

// blPage is how far pgdown and pgup move: a screen of the table, less a line,
// so the ticket at the edge stays in sight to read on from.
func (m model) blPage() int { return maxi(m.blTableHeight(m.bodyHeight())-1, 1) }

// backlogLeft builds the ticket table pane.
func (m model) backlogLeft(rows []blRow, pickedCount, leftW, h int) string {
	innerW := leftW - 2*pad

	// "12 of 340" while anything narrows the table, so a filter left on is
	// never read as the size of the backlog.
	count := itoa(len(rows)) + " open"
	if len(rows) != len(backlogTickets) {
		count = itoa(len(rows)) + " of " + itoa(len(backlogTickets)) + " open"
	}
	lines := []string{backlogSpread(
		fg(cDim, "backlog · "+count+" · "+itoa(pickedCount)+" picked"),
		fg(cFaint, m.blViewLabel()),
		leftW,
	)}
	lines = append(lines, "  "+row(innerW, "",
		c("", 4, cFaint),
		c("TICKET", 17, cFaint),
		flexc("TITLE", cFaint),
		c("REPO", 17, cFaint),
		cr("SOURCE", 9, cFaint),
		cr("IDLE", 12, cFaint),
	)+"  ")
	lines = append(lines, m.blTable(rows, innerW, m.blTableHeight(h))...)

	footer := []string{
		"  " + fg(cRule, strings.Repeat("─", innerW)) + "  ",
		backlogSpread(fg(cDim, m.blPaneHelp()), "", leftW),
	}

	// Pin the footer to the bottom of the pane.
	top := padBlockTo(vjoin(lines...), maxi(h-len(footer), 0))
	return vjoin(append([]string{top}, footer...)...)
}

// blPaneHelp is the table pane's own key line: how to change what the table
// shows. The footer under the whole screen names what to do with a ticket.
func (m model) blPaneHelp() string {
	return strings.Join([]string{
		m.keyHint("backlog.product", "product"), m.keyHint("backlog.source", "source"),
		prettyKey(m.keys.KeyFor("backlog.first")) + "/" + prettyKey(m.keys.KeyFor("backlog.last")) + " ends",
	}, " · ")
}

// blTable is the table's lines, product group by product group, windowed on
// the cursor.
//
// A group starts with a divider naming it and counting its tickets. Where the window cuts the table, its top line says how many tickets are
// above — and, when the divider naming the product on screen has scrolled
// away, which product that is — and its bottom line how many are below. A
// table that simply stopped at the bottom edge read as the whole backlog.
func (m model) blTable(rows []blRow, w, h int) []string {
	if h <= 0 {
		return nil
	}
	if len(rows) == 0 {
		return []string{"  " + fg(cFaint, truncate(m.blEmpty(), w)) + "  "}
	}
	sel := clampCursor(m.backlogCursor, len(rows))
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.group]++
	}

	// Each line is a ticket or the divider above one.
	type line struct {
		row int
		div bool
	}
	var lines []line
	first := make([]int, len(rows)) // the line each ticket is drawn on
	for i, r := range rows {
		if i == 0 || rows[i-1].group != r.group {
			lines = append(lines, line{row: i, div: true})
		}
		first[i] = len(lines)
		lines = append(lines, line{row: i})
	}

	start, end := window(first[sel], len(lines), h)
	out := make([]string, 0, end-start)
	for _, ln := range lines[start:end] {
		r := rows[ln.row]
		switch {
		case ln.div:
			out = append(out, blDivider(w, r.group+" · "+itoa(counts[r.group])))
		default:
			out = append(out, m.blTicketLine(r, ln.row == sel, w))
		}
	}

	// What the window cut off, said where it cut. Never over the cursor's own
	// line: window centres it, so from three lines up neither edge is it.
	if h >= 3 {
		if start > 0 {
			above := 0
			for _, f := range first {
				if f <= start {
					above++
				}
			}
			more := "↑ " + countOf(above, "more ticket", "more tickets") + " above"
			if next := lines[start+1]; next.div {
				out[0] = "  " + fg(cFaint, more)
			} else {
				out[0] = blDivider(w, rows[next.row].group+" · "+more)
			}
		}
		if end < len(lines) {
			below := 0
			for _, f := range first {
				if f >= end-1 {
					below++
				}
			}
			out[len(out)-1] = "  " + fg(cFaint, "↓ "+countOf(below, "more ticket", "more tickets")+" below")
		}
	}
	return out
}

// blDivider is the line above a product's tickets, set into a rule so it reads
// as a boundary rather than as a row.
func blDivider(w int, label string) string {
	lead, text := "── ", label+" "
	rest := maxi(0, w-dispWidth(lead)-dispWidth(text))
	return "  " + fg(cRule, lead) + fg(cFaint, text) + fg(cRule, strings.Repeat("─", rest)) + "  "
}

// blTicketLine is one ticket's row: what is picked or under the cursor, its
// priority, id and title, its repo and source, and how long it has sat
// untouched.
func (m model) blTicketLine(r blRow, cursor bool, w int) string {
	t := r.t
	pick := m.picked[t.id]
	bg := ""
	if cursor {
		bg = cSel
	}
	edge := ""
	switch {
	case pick:
		edge = cGreen
	case cursor:
		edge = cMid
	}
	marker := ""
	switch {
	case pick:
		marker = "✓"
	case cursor:
		marker = "▸"
	}
	priGlyph := "·"
	switch t.pri {
	case "urgent":
		priGlyph = "■"
	case "high":
		priGlyph = "◆"
	}
	titleColor := cFg
	switch {
	case t.taken != "":
		titleColor = cDim
	case cursor:
		titleColor = cWhite
	}
	idColor := cMid
	if cursor {
		idColor = cWhite
	}
	state, stateColor := blIdle(t), cDim
	if t.taken != "" {
		state, stateColor = "dispatched", cGreen
	}

	edgeCh := " "
	if edge != "" {
		edgeCh = "▎"
	}
	leftGut := paint(edge, bg, edgeCh) + paint("", bg, " ")
	body := row(w, bg,
		c(marker, 2, cMid),
		c(priGlyph, 2, priColor[t.pri]),
		c(t.id, 17, idColor),
		flexc(t.title, titleColor),
		c(t.repo, 17, cDim),
		cr(sourceMeta[t.src].label, 9, sourceMeta[t.src].color),
		cr(state, 12, stateColor),
	)
	rightGut := paint("", bg, "  ")
	return leftGut + body + rightGut
}

// blSelectedRow is the row under the (clamped) cursor; ok is false when the
// table is empty.
func blSelectedRow(rows []blRow, cursor int) (blRow, bool) {
	if len(rows) == 0 {
		return blRow{}, false
	}
	return rows[clampCursor(cursor, len(rows))], true
}

// blDates is when a ticket was filed and when anything last happened on it,
// leaving out what its source does not say.
func blDates(t ticket) string {
	var parts []string
	if !t.filed.IsZero() {
		parts = append(parts, "filed "+blkAge(t.filed)+" ago")
	}
	if !t.touched.IsZero() {
		parts = append(parts, "last touched "+blkAge(t.touched)+" ago")
	}
	return strings.Join(parts, " · ")
}

// backlogRight builds the selected-ticket detail pane.
func (m model) backlogRight(rows []blRow, pickedCount, rightW, h int) string {
	t := m.ticketSelected()
	if r, ok := blSelectedRow(rows, m.backlogCursor); ok {
		t = r.t
	}
	innerW := rightW - 2*pad

	wrapL := func(inner string) string {
		return strings.Repeat(" ", pad) + backlogPad(inner, innerW) + strings.Repeat(" ", pad)
	}
	blank := wrapL("")

	branch := "feature/" + backlogSlug(t.id)
	takenLine := fg(cDim, "✓ no dispatcher on this yet")
	if t.taken != "" {
		takenLine = fg(cAmber, "◆ already dispatched as \""+t.taken+"\" — enter would double up")
	}

	var top []string
	top = append(top, backlogSpread(
		fg(cDim, t.id+" · "+sourceMeta[t.src].label),
		fg(priColor[t.pri], t.pri),
		rightW,
	))
	top = append(top, wrapL(fg(cWhite, t.title)))
	top = append(top, wrapL(fg(cDim, backlogWhere(t))))
	if d := blDates(t); d != "" {
		top = append(top, wrapL(fg(cFaint, d)))
	}
	top = append(top, blank)
	for _, ln := range backlogWrap(t.body, innerW) {
		top = append(top, wrapL(fg(cMid, ln)))
	}
	top = append(top, blank)
	top = append(top, wrapL(takenLine))
	top = append(top, blank)
	top = append(top, wrapL(fg(cRule, strings.Repeat("─", innerW))))
	top = append(top, wrapL(fg(cDim, "dispatch as")))
	for _, ln := range backlogWrap(t.prompt, innerW) {
		top = append(top, wrapL(fg(cFg, ln)))
	}
	// Only the branch is stated, because only the branch is known. The model
	// and permission mode were printed here as fact and chosen by nothing —
	// no config, no state and no argument to launchCmd selects either.
	top = append(top, wrapL(fg(cFaint, "branch ")+fg(cMid, branch)))
	// A ticket from Linear or Azure names no repo, and a dispatch needs one.
	// Say so here rather than let enter fail with "repo not found: ".
	if t.repo == "" {
		top = append(top, wrapL(fg(cAmber, "◆ no repo on this ticket — dispatch it from the form instead")))
	}

	// picked list, pinned to the bottom (design's margin-top:auto).
	var picked []string
	picked = append(picked, blank)
	picked = append(picked, wrapL(fg(cDim, "picked · "+itoa(pickedCount))))
	if pickedCount == 0 {
		picked = append(picked, wrapL(fg(cFaint, "space picks a ticket · ctrl+d dispatches the lot")))
	} else {
		for _, pt := range backlogTickets {
			if !m.picked[pt.id] {
				continue
			}
			picked = append(picked, wrapL(row(innerW, "",
				c(pt.id, 17, cMid),
				flexc(pt.title, cFg),
				c(" "+pt.repo, dispWidth(pt.repo)+1, cFaint),
			)))
		}
	}

	body := padBlockTo(vjoin(top...), maxi(h-len(picked), 0))
	return vjoin(append([]string{body}, picked...)...)
}

// backlogFeature names the feature a ticket would be dispatched as: its title,
// falling back to the ticket id when the source gave it none.
func backlogFeature(t ticket) string {
	if t.title != "" {
		return t.title
	}
	return t.id
}

// updateBacklog mirrors handleKey('backlog'), with the dispatching keys wired
// to real launches.
//
// The list keys are resolved first and the backlog's own after them — the two
// scopes are layered, so the keymap refuses a key bound in both. Every key
// that moves the cursor keys it to the ticket it lands on, and every key that
// narrows the table finds that ticket again (blSync), because enter dispatches
// what the cursor is on.
func (m model) updateBacklog(k string) (model, tea.Cmd) {
	if k = m.keys.Resolve(keymap.List, k); k == "" {
		return m, nil
	}
	if k = m.keys.Resolve(keymap.Backlog, k); k == "" {
		return m, nil
	}
	rows := m.blRows()
	t := m.ticketSelected()
	switch k {
	case "j", "down":
		return m.blTo(m.backlogCursor + 1), nil
	case "k", "up":
		return m.blTo(m.backlogCursor - 1), nil
	case "g", "home":
		return m.blTo(0), nil
	case "G", "end":
		return m.blTo(len(rows) - 1), nil
	case "pgdown":
		return m.blTo(m.backlogCursor + m.blPage()), nil
	case "pgup":
		return m.blTo(m.backlogCursor - m.blPage()), nil
	case "p":
		// A product that has since left the backlog is not in the list, so
		// the next press starts it again from the first.
		ps := blProducts()
		if i := slices.Index(ps, m.blProduct); i+1 < len(ps) {
			m.blProduct = ps[i+1]
		} else {
			m.blProduct = ""
		}
		return m.blSync(), nil
	case " ", "space":
		if m.picked == nil {
			m.picked = map[string]bool{}
		}
		if m.picked[t.id] {
			delete(m.picked, t.id)
		} else {
			m.picked[t.id] = true
		}
	case "enter":
		switch {
		case t.taken != "":
			m.notice = t.id + " already has a dispatcher — \"" + t.taken + "\""
		case t.repo == "":
			// Linear and Azure tickets carry no repo. launchCmd would look one
			// up by an empty name and report "repo not found: ", which reads as
			// a broken cockpit rather than a ticket the backlog cannot place.
			m.notice = t.id + " names no repo — dispatch it from the form"
		default:
			m.notice = ""
			// A dispatch from here lands on a lens that does not list it, so the
			// placeholder is what makes it findable the moment the human presses 1
			// to go and look — see pending.go.
			m = m.markPending(m.pendingFor(t.repo, backlogFeature(t), t.prompt))
			// The backlog asks no questions — enter on a ticket is the whole
			// interaction — so it dispatches in the default mode, on the default
			// model, without fan-out. That is the posture the ticket implies:
			// work handed off to happen elsewhere, with nobody sitting on its
			// permission prompt. Anything else is dispatched from a form that
			// asks.
			return m, launchCmd(m.cfg, t.repo, backlogFeature(t), t.prompt, dispatchpkg.DefaultMode, dispatchpkg.DefaultModel, dispatchpkg.DefaultRoot, nil, false, account.Default)
		}
	case "ctrl+d":
		// This used to announce a dispatch and launch nothing — the worst kind
		// of notice, because the user believes the work is out. It now launches
		// one dispatcher per picked ticket, skipping any that already has one.
		var cmds []tea.Cmd
		var skipped, noRepo int
		for _, bt := range backlogTickets {
			if !m.picked[bt.id] {
				continue
			}
			if bt.taken != "" {
				skipped++
				continue
			}
			if bt.repo == "" {
				noRepo++
				continue
			}
			m = m.markPending(m.pendingFor(bt.repo, backlogFeature(bt), bt.prompt))
			cmds = append(cmds, launchCmd(m.cfg, bt.repo, backlogFeature(bt), bt.prompt, dispatchpkg.DefaultMode, dispatchpkg.DefaultModel, dispatchpkg.DefaultRoot, nil, false, account.Default))
		}
		m.picked = map[string]bool{}
		var why []string
		if skipped > 0 {
			why = append(why, itoa(skipped)+" already taken")
		}
		if noRepo > 0 {
			why = append(why, itoa(noRepo)+" name no repo")
		}
		if len(cmds) == 0 {
			m.notice = "nothing to dispatch"
			if len(why) > 0 {
				m.notice = strings.Join(why, " · ")
			}
			return m, nil
		}
		m.notice = "dispatching " + itoa(len(cmds)) + " " + plural(len(cmds), "ticket", "tickets") + " · one session each"
		if len(why) > 0 {
			m.notice += " · " + strings.Join(why, " · ")
		}
		return m, tea.Batch(cmds...)
	case "s":
		order := []string{"all", "gh", "lin", "ado"}
		idx := 0
		for i, o := range order {
			if o == m.srcFilter {
				idx = i
				break
			}
		}
		m.srcFilter = order[(idx+1)%len(order)]
		return m.blSync(), nil
	}
	return m, nil
}
