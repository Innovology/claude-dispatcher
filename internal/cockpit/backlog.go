package cockpit

// backlog.go is lens 5 — the BACKLOG: open tickets pulled from GitHub Issues,
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

// backlogTickets is the BACKLOG data, in display order. taken is "" when the
// design had null (no dispatcher on it yet).
var backlogTickets = []ticket{
	{id: "CTV-124", src: "lin", title: "Bulk export of audit log to S3", product: "cortiva", repo: "cortiva-hq", pri: "high", age: "2d", labels: "compliance", taken: "",
		body:   "Enterprise accounts want the audit log pushed to their own bucket nightly rather than pulled via csv. Needs per-tenant credentials and a dry-run mode.",
		prompt: "nightly audit log export to a customer-owned s3 bucket, per-tenant credentials, dry-run flag. reuse the filters from #148."},
	{id: "CTV-123", src: "lin", title: "Rate limit headers missing on 200s", product: "cortiva", repo: "cortiva-api", pri: "urgent", age: "4h", labels: "bug · customer", taken: "",
		body:   "We return X-RateLimit-* only on 429. Two integrators are polling blind. Should be on every response.",
		prompt: "emit X-RateLimit-Limit/Remaining/Reset on every api response, not just 429s. extend the bucket added in #152."},
	{id: "AB#2298", src: "ado", title: "Release-Prod migrate stage fails on eu region", product: "northwind", repo: "nw-billing", pri: "urgent", age: "11m", labels: "incident", taken: "",
		body:   "The migrate stage cannot reach the eu replica. Blocking invoice pdf from going live.",
		prompt: "diagnose why the Release-Prod migrate stage cannot reach the eu replica and fix the pipeline. do not touch the schema."},
	{id: "altsports_1#219", src: "gh", title: "Fixture CSV: support 2019 date format", product: "altsports", repo: "altsports_1", pri: "high", age: "1d", labels: "good first", taken: "fixture import",
		body:   "Three specs fail on the 2019 dd-mm-yyyy variant. Normalising dates in the parser touches every importer.",
		prompt: "normalise legacy date formats in the fixture importer, keep the other importers passing."},
	{id: "CTV-119", src: "lin", title: "Mobile: retry queue for offline writes", product: "cortiva", repo: "cortiva-mobile", pri: "med", age: "3d", labels: "mobile", taken: "",
		body:   "Offline reads landed in #48. Writes still fail silently when the device is offline.",
		prompt: "queue writes made offline and replay them on reconnect, surface a pending badge. build on the cache from #48."},
	{id: "claude-dispatcher#66", src: "gh", title: "Detect Azure Pipelines as a live signal", product: "dispatch", repo: "claude-dispatcher", pri: "high", age: "6h", labels: "tracker", taken: "",
		body:   "A successful ADO release run should mark a feature live, same as a gh deploy workflow. Half the portfolio is on ADO.",
		prompt: "treat a successful azure pipelines release run as the live signal, same as gh deploy workflows."},
	{id: "kalish-core#51", src: "gh", title: "Ingest spans missing upload id", product: "kalish", repo: "kalish-core", pri: "med", age: "5d", labels: "observability", taken: "",
		body:   "Traces exist but you cannot correlate a span back to the upload that caused it.",
		prompt: "attach the upload id to every ingest span so one trace covers one upload end to end."},
	{id: "CTV-116", src: "lin", title: "Invite: keep legacy token route?", product: "cortiva", repo: "cortiva-hq", pri: "med", age: "6m", labels: "decision", taken: "invite flow",
		body:   "Blocking the invite rebuild. Emails already sent carry /invite/:token links.",
		prompt: "decide and implement: keep /invite/:token alive for 90 days with a redirect, or hard-cut it."},
	{id: "nw-portal#88", src: "ado", title: "Portal session drops after 30 minutes", product: "northwind", repo: "nw-portal", pri: "high", age: "2d", labels: "bug", taken: "",
		body:   "Entra tokens are not refreshed, so users are bounced mid-form.",
		prompt: "refresh entra tokens silently before expiry so the portal session survives a working day."},
	{id: "altsports-web#44", src: "gh", title: "League table: tie-break by head-to-head", product: "altsports", repo: "altsports-web", pri: "low", age: "1w", labels: "rules", taken: "",
		body:   "Class rules put head-to-head above points difference. We do the opposite.",
		prompt: "correct the league tie-break order to head-to-head before points difference, with a test per rule."},
	{id: "CTV-101", src: "lin", title: "Docs drift behind the pdf export", product: "cortiva", repo: "cortiva-docs", pri: "low", age: "3d", labels: "docs · stale repo", taken: "",
		body:   "cortiva-docs has not been touched in 17 days and still documents the old export flow.",
		prompt: "update the export docs to match #144, including the pagination note."},
	{id: "kalish-ops#79", src: "gh", title: "Rota timezone: venue local vs account default", product: "kalish", repo: "kalish-ops", pri: "high", age: "41m", labels: "decision", taken: "shift rota",
		body:   "The dispatcher is idle waiting on this answer.",
		prompt: "anchor the rota to venue local time, fall back to the account default when the venue has none."},
}

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
		state := t.age + " old"
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
			fg(cDim, "space pick · enter dispatch now · ctrl+d dispatch "+itoa(pickedCount)+" picked · s source · 1 floor"),
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
	modelName := "sonnet"
	if t.pri == "urgent" {
		modelName = "opus"
	}
	mode := "edits, asks to ship"
	if strings.Contains(t.labels, "decision") {
		mode = "plan only"
	}
	takenLine := fg(cDim, "✓ no dispatcher on this yet")
	if t.taken != "" {
		takenLine = fg(cAmber, "◆ already dispatched as \""+t.taken+"\" — enter would double up")
	}

	var top []string
	top = append(top, backlogSpread(
		fg(cDim, t.id+" · "+sourceMeta[t.src].label),
		fg(priColor[t.pri], t.pri+" · "+t.age+" old"),
		rightW,
	))
	top = append(top, wrapL(fg(cWhite, t.title)))
	top = append(top, wrapL(fg(cDim, t.product+" / "+t.repo+" · "+t.labels)))
	top = append(top, blank)
	for _, ln := range backlogWrap(t.body, innerW) {
		top = append(top, wrapL(fg(cMid, ln)))
	}
	top = append(top, blank)
	top = append(top, wrapL(takenLine))
	top = append(top, blank)
	top = append(top, wrapL(fg(cRule, strings.Repeat("─", innerW))))
	top = append(top, backlogSpread(
		fg(cDim, "dispatch as"),
		fg(cFaint, "e edit · M model · p mode"),
		rightW,
	))
	for _, ln := range backlogWrap(t.prompt, innerW) {
		top = append(top, wrapL(fg(cFg, ln)))
	}
	top = append(top, wrapL(
		fg(cFaint, "branch ")+fg(cMid, branch)+
			fg(cFaint, "   model ")+fg(cMid, modelName)+
			fg(cFaint, "   mode ")+fg(cMid, mode),
	))

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

// updateBacklog mirrors handleKey('backlog'). All actions are UI-only: they set
// notices exactly as the mock does.
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
		if t.taken != "" {
			m.notice = t.id + " already has a dispatcher — \"" + t.taken + "\""
		} else {
			feature := t.title
			if feature == "" {
				feature = t.id
			}
			return m, launchCmd(m.cfg, t.repo, feature, t.prompt)
		}
	case "ctrl+d":
		m.notice = "dispatched " + itoa(len(m.picked)) + " tickets · one session each"
		m.picked = map[string]bool{}
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
