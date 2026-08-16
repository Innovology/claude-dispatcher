package cockpit

// backlog.go is lens 3 — the BACKLOG: open tickets pulled from GitHub Issues,
// Linear and Azure Boards into one dispatch queue. The left pane is the ticket
// table (pick with space, dispatch with enter); the right pane is the selected
// ticket's detail plus the "dispatch as" preview and the picked list.
//
// Faithful port of the design's BACKLOG data, backlogList/ticketSelected,
// handleKey('backlog') and renderVals.backlog.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// backlogList filters the tickets by the active source filter. "all" is
// everything; otherwise src must equal the filter (gh | lin | ado).
func (m model) backlogList() []ticket {
	if m.srcFilter == "" || m.srcFilter == "all" {
		return backlogTickets
	}
	out := make([]ticket, 0, len(backlogTickets))
	for _, t := range backlogTickets {
		if t.src == m.srcFilter {
			out = append(out, t)
		}
	}
	return out
}

// ticketSelected returns the ticket under the (clamped) backlog cursor, or a
// placeholder when the backlog is empty (real portfolio with nothing queued).
func (m model) ticketSelected() ticket {
	l := m.backlogList()
	if len(l) == 0 {
		return ticket{title: "no open tickets", body: "nothing in the backlog for this source.", pri: "med", age: "—"}
	}
	return l[clampCursor(m.backlogCursor, len(l))]
}

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
// found per repo); a Linear or Azure ticket has neither product nor repo, and
// the line used to render as " / · In Progress".
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
func (m model) viewBacklog(w, h int) string {
	list := m.backlogList()
	pickedCount := len(m.picked)
	showDetail := m.fit().showDetail

	leftW := w
	rightW := 0
	if showDetail {
		leftW = w * 58 / 100
		rightW = w - leftW - 1 // 1 col for the vertical rule
	}

	left := m.backlogLeft(list, pickedCount, leftW, h)
	if !showDetail {
		return clampLines(left, h)
	}
	right := m.backlogRight(pickedCount, rightW, h)
	out := hjoin(padBlockTo(left, h), vrule(h, cRule), padBlockTo(right, h))
	return clampLines(out, h)
}

// backlogLeft builds the ticket table pane.
func (m model) backlogLeft(list []ticket, pickedCount, leftW, h int) string {
	innerW := leftW - 2*pad
	on := clampCursor(m.backlogCursor, len(list))

	// header: "backlog · N open · M picked" + sources kicker
	title := backlogSpread(
		fg(cDim, "backlog · "+itoa(len(list))+" open · "+itoa(pickedCount)+" picked"),
		fg(cFaint, "github issues · linear · azure boards"),
		leftW,
	)
	colHead := "  " + row(innerW, "",
		c("", 4, cFaint),
		c("TICKET", 17, cFaint),
		flexc("TITLE", cFaint),
		c("REPO", 17, cFaint),
		cr("SOURCE", 9, cFaint),
		cr("STATE", 12, cFaint),
	) + "  "

	lines := []string{title, colHead}

	for n, t := range list {
		cursor := n == on
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
		// Azure work items carry no updated-at, so the age is blank; print
		// nothing rather than a bare " old".
		state := ""
		if t.age != "" {
			state = t.age + " old"
		}
		stateColor := cDim
		if t.taken != "" {
			state = "dispatched"
			stateColor = cGreen
		}

		edgeCh := " "
		if edge != "" {
			edgeCh = "▎"
		}
		leftGut := paint(edge, bg, edgeCh) + paint("", bg, " ")
		body := row(innerW, bg,
			c(marker, 2, cMid),
			c(priGlyph, 2, priColor[t.pri]),
			c(t.id, 17, idColor),
			flexc(t.title, titleColor),
			c(t.repo, 17, cDim),
			cr(sourceMeta[t.src].label, 9, sourceMeta[t.src].color),
			cr(state, 12, stateColor),
		)
		rightGut := paint("", bg, "  ")
		lines = append(lines, leftGut+body+rightGut)
	}

	footer := []string{
		"  " + fg(cRule, strings.Repeat("─", innerW)) + "  ",
		backlogSpread(
			// "triage", not the design's "1 floor": "floor" is the internal lens
			// id, and the lens bar two rows up calls it triage.
			fg(cDim, "space pick · enter dispatch now · ctrl+d dispatch "+itoa(pickedCount)+" picked · s source · 1 triage"),
			"",
			leftW,
		),
	}

	// Pin the footer to the bottom of the pane.
	top := padBlockTo(vjoin(lines...), maxi(h-len(footer), 0))
	return vjoin(append([]string{top}, footer...)...)
}

// backlogRight builds the selected-ticket detail pane.
func (m model) backlogRight(pickedCount, rightW, h int) string {
	t := m.ticketSelected()
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

	pri := t.pri
	if t.age != "" {
		pri += " · " + t.age + " old"
	}

	var top []string
	top = append(top, backlogSpread(
		fg(cDim, t.id+" · "+sourceMeta[t.src].label),
		fg(priColor[t.pri], pri),
		rightW,
	))
	top = append(top, wrapL(fg(cWhite, t.title)))
	top = append(top, wrapL(fg(cDim, backlogWhere(t))))
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
func (m model) updateBacklog(k string) (model, tea.Cmd) {
	l := m.backlogList()
	t := m.ticketSelected()
	switch k {
	case "j", "down":
		m.backlogCursor = mini(m.backlogCursor+1, len(l)-1)
	case "k", "up":
		m.backlogCursor = maxi(m.backlogCursor-1, 0)
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
			return m, launchCmd(m.cfg, t.repo, backlogFeature(t), t.prompt)
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
			cmds = append(cmds, launchCmd(m.cfg, bt.repo, backlogFeature(bt), bt.prompt))
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
		m.backlogCursor = 0
	}
	return m, nil
}
