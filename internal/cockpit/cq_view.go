package cockpit

// cq_view.go renders the v3 triage lens: the command queue. It replaces the v2
// list + detail floor entirely — one full-width column, one ask at a time, the
// rest of the queue beneath it. Three mutually exclusive modes:
//
//	item     the queue has a head and we are neither drafting nor in the
//	         working view — that item in full, plus the rest of the queue
//	working  `w` — every running dispatcher, grouped by product
//	empty    the queue is empty, or `d` opened a draft — the dispatch prompt
//
// The design is a flexbox column: fixed pixel spacers rounded to rows, then one
// `flex:1` gap that pins the closing line to the bottom. Every spacer carries a
// shed order, so a short terminal loses whitespace before it loses a sentence
// (cqShed), and the queue list shrinks before any spacer does — whitespace is
// only the shape of the pane, a hidden ask is something the human never sees.
//
// All content comes from cq.go: the collector composes the sentences from real
// records and leaves out any clause it has no source for. Nothing is invented
// here; where a field is empty the row is dropped rather than padded out.

import "strings"

// ---- row plumbing -----------------------------------------------------------

// cqRow is one line of a mode's column. shed is 0 for content (never dropped)
// and otherwise the order in which spacers give up their row when the pane is
// too short: 1 goes first. fill marks the design's `flex:1` gap.
type cqRow struct {
	s    string
	shed int
	fill bool
}

func cqFixed(s string) cqRow { return cqRow{s: s} }
func cqGap(order int) cqRow  { return cqRow{shed: order} }
func cqFill() cqRow          { return cqRow{fill: true} }

// cqSolid counts the rows that occupy a line unconditionally. The flex gap is
// not one of them — like the design's `min-height:0` it collapses to nothing
// when there is no slack left to absorb.
func cqSolid(rows []cqRow) int {
	n := 0
	for _, r := range rows {
		if !r.fill {
			n++
		}
	}
	return n
}

// cqCheapest returns the index and order of the spacer that should shed next.
func cqCheapest(rows []cqRow) (idx, order int) {
	idx = -1
	for i, r := range rows {
		if r.shed > 0 && (idx < 0 || r.shed < order) {
			idx, order = i, r.shed
		}
	}
	return idx, order
}

func cqDrop(rows []cqRow, at int) []cqRow { return append(rows[:at:at], rows[at+1:]...) }

// cqShed drops spacers, cheapest first, until rows fit in budget lines. It stops
// once only content is left: clipping a sentence is clampLines's call, not the
// layout's.
func cqShed(rows []cqRow, budget int) []cqRow {
	for cqSolid(rows) > budget {
		at, _ := cqCheapest(rows)
		if at < 0 {
			return rows
		}
		rows = cqDrop(rows, at)
	}
	return rows
}

// cqShedPair sheds across a head and a tail as one pool, so their spacers
// interleave in the design's priority order rather than each half draining
// separately.
func cqShedPair(head, tail []cqRow, budget int) ([]cqRow, []cqRow) {
	for len(head)+len(tail) > budget {
		hi, hp := cqCheapest(head)
		ti, tp := cqCheapest(tail)
		switch {
		case hi < 0 && ti < 0:
			return head, tail
		case ti < 0 || (hi >= 0 && hp <= tp):
			head = cqDrop(head, hi)
		default:
			tail = cqDrop(tail, ti)
		}
	}
	return head, tail
}

// cqRender sheds, expands the flex gap into the leftover height and clamps.
func cqRender(rows []cqRow, h int) string {
	if h < 1 {
		return ""
	}
	rows = cqShed(rows, h)
	extra := maxi(0, h-cqSolid(rows))
	out := make([]string, 0, h)
	for _, r := range rows {
		if r.fill {
			for i := 0; i < extra; i++ {
				out = append(out, "")
			}
			extra = 0
			continue
		}
		out = append(out, r.s)
	}
	return clampLines(vjoin(out...), h)
}

// cqInner is the writable width inside the page gutters.
func cqInner(w int) int { return maxi(1, w-2*pad) }

// ---- shared values ----------------------------------------------------------

// cqLeadColor maps an item's tone to the colour its lead sentence is written
// in. A normal tone is dim, not foreground: the lead is the quiet explanation
// under a loud title, and only red and amber earn the eye.
func cqLeadColor(tone string) string {
	switch tone {
	case "red":
		return cRed
	case "amber":
		return cAmber
	}
	return cDim
}

// cqLabel is an uppercase product label. The design letter-spaces these by
// 0.14em, which has no cell representation, so they stay unspaced. A record
// whose repo maps to no configured product groups under "other", as the rest of
// the cockpit does.
func cqLabel(p string) string {
	if p == "" {
		p = "other"
	}
	return strings.ToUpper(p)
}

// cqWhere is the item's locator line. cqItem carries the facts separately so
// the view can drop the parts that are missing — a dispatch that never branched
// has no ref, and closing the gap beats printing "· ·".
func cqWhere(it cqItem) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{it.repo, it.ref, it.age} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// cqLeadText is the item's sentence. The collector always composes one, but a
// record it could read nothing from would leave the pane's loudest line blank,
// which reads as "nothing to say" — say what is actually known instead.
func cqLeadText(it cqItem) string {
	if it.lead != "" {
		return it.lead
	}
	if it.want != "" {
		return it.want
	}
	return "It stopped and is waiting on you."
}

// cqPosition tells the human how deep this product's run is, so acting on the
// head reads as progress through a product rather than an isolated
// interruption. It describes the head of the queue, which is the only item the
// line is ever shown for.
func cqPosition(q []cqItem) string {
	if len(q) == 0 {
		return ""
	}
	n := 0
	for _, it := range q {
		if it.product == q[0].product {
			n++
		}
	}
	if n > 1 {
		return "1 of " + itoa(n) + " in this product"
	}
	return "last in this product"
}

// cqRunningCount is how many dispatchers are running across every group.
func cqRunningCount() int {
	n := 0
	for _, g := range cqWorking {
		n += len(g.rows)
	}
	return n
}

// cqWorkTotalLine heads the working view. "none of them need you" is true by
// construction rather than by assertion: anything that needs you is in the
// queue, not here.
func cqWorkTotalLine() string {
	n := cqRunningCount()
	if n == 0 {
		return "nothing running · anything that wants you is in the queue"
	}
	s := itoa(n) + " running · none of them need you"
	if cqLastOutput != "" {
		s += " · last output " + cqLastOutput + " ago"
	}
	return s
}

// cqUnattendedLine closes the item and prompt modes: what is getting on with it
// while you work the queue. The last-output clause is dropped when no session
// has a transcript we could read, rather than answered with a different number.
func cqUnattendedLine() string {
	n := cqRunningCount()
	if n == 0 {
		return "nothing running unattended"
	}
	s := itoa(n) + " running unattended"
	if cqLastOutput != "" {
		s += " · last output " + cqLastOutput + " ago"
	}
	return s + " · w to see them"
}

// cqOutCell renders a working row's last-output age. Sub-minute is the healthy
// case for a live session so it stays dim; minutes or longer means it has gone
// quiet and is lifted. An unreadable transcript is an em dash, not a zero.
func cqOutCell(out string) (text, hex string) {
	if out == "" {
		return "—", cFaint
	}
	if strings.HasSuffix(out, "s") {
		return out + " ago", cDim
	}
	return out + " ago", cMid
}

// cqChipRows lays already-coloured chips out with the design's 6ch gap,
// wrapping at chip boundaries onto at most two rows.
func cqChipRows(chips []string, inner int) []string {
	gap := strings.Repeat(" ", 6)
	var out []string
	cur := ""
	for _, ch := range chips {
		if cur == "" {
			cur = ch
			continue
		}
		if dispWidth(cur)+len(gap)+dispWidth(ch) > inner {
			out = append(out, truncateAnsi(cur, inner))
			if len(out) == 2 {
				return out
			}
			cur = ch
			continue
		}
		cur += gap + ch
	}
	if cur != "" && len(out) < 2 {
		out = append(out, truncateAnsi(cur, inner))
	}
	return out
}

// ---- entry ------------------------------------------------------------------

// viewCQ picks the mode and renders it. lensBody calls it for the triage lens.
func (m model) viewCQ(w, h int) string {
	switch {
	case m.cqWork:
		return m.cqViewWorking(w, h)
	case m.cqDispatch || len(m.cqQueue()) == 0:
		return m.cqViewEmpty(w, h)
	default:
		return m.cqViewItem(w, h)
	}
}

// ---- mode: one item ---------------------------------------------------------

// cqViewItem is the head of the queue in full — what it wants, the evidence,
// and the acts that answer it — with the remainder listed under a rule so the
// depth behind the current ask is visible without leaving it.
func (m model) cqViewItem(w, h int) string {
	q := m.cqQueue()
	if len(q) == 0 {
		return m.cqViewEmpty(w, h)
	}
	inner := cqInner(w)
	cur := q[0]

	head := []cqRow{cqGap(2), cqGap(2)}
	head = append(head, cqFixed(flG(flSpread(
		fg(cDim, cqLabel(cur.product)),
		fg(cFaint, cqPosition(q)), inner))))
	head = append(head, cqGap(6))
	head = append(head, cqFixed(flG(flSpread(
		fg(cFg, cur.title),
		fg(cFaint, cqWhere(cur)), inner))))
	head = append(head, cqGap(4))
	head = append(head, cqFixed(flG(fg(cqLeadColor(cur.tone), truncate(cqLeadText(cur), inner)))))
	// The collector drops any evidence clause it has no source for, so an empty
	// detail means there was nothing true to say — printing a blank row instead
	// would read as "nothing to say about it", which is a different claim.
	for _, d := range []string{cur.detail, cur.detail2} {
		if d != "" {
			head = append(head, cqFixed(flG(fg(cDim, truncate(d, inner)))))
		}
	}
	head = append(head, cqGap(3))
	for _, ln := range m.cqActionRows(cur, inner) {
		head = append(head, cqFixed(flG(ln)))
	}
	head = append(head, cqGap(1), cqGap(1))
	head = append(head, cqFixed(flG(fg(cRule, strings.Repeat("─", inner)))))
	head = append(head, cqGap(5))

	tail := []cqRow{
		cqFixed(flG(fg(cFaint, truncate(cqUnattendedLine(), inner)))),
		cqGap(7),
	}

	// The rest list shrinks before any spacer sheds.
	rest := q[1:]
	hidden := 0
	if room := maxi(0, h-len(head)-len(tail)); len(rest) > room {
		hidden = len(rest) - room
		rest = rest[:room]
	}

	rows := make([]cqRow, 0, len(head)+len(rest)+len(tail)+1)
	rows = append(rows, head...)
	for i, r := range rest {
		// The 7ch lead column is blank on all but the first row, so the count of
		// what did not fit rides there instead of costing a row of its own.
		lead := ""
		if i == 0 {
			lead = "then"
		}
		if hidden > 0 && i == len(rest)-1 {
			lead = "+" + itoa(hidden)
		}
		rows = append(rows, cqFixed(cqRestRow(w, r, lead)))
	}
	rows = append(rows, cqFill())
	rows = append(rows, tail...)
	return cqRender(rows, h)
}

// cqActionRows is the acts line, or the flash that replaces it for the 850ms
// after an act. The design makes the two mutually exclusive and so does this.
func (m model) cqActionRows(it cqItem, inner int) []string {
	if m.cqFlash != "" {
		// A keep act (attach) did not clear anything, so it reports in mid grey
		// rather than the green that means "one fewer thing wants you".
		col := cGreen
		if m.cqFlashKeep {
			col = cMid
		}
		return []string{fg(col, truncate(m.cqFlash, inner))}
	}
	acts := it.acts
	if len(acts) == 0 {
		// `s` is handled unconditionally by the queue's key handler, so an item
		// the collector could offer no acts for still shows a way onward.
		acts = []cqAct{{k: "s", d: "skip"}}
	}
	chips := make([]string, 0, len(acts))
	for _, a := range acts {
		chips = append(chips, fg(cFg, a.k)+" "+fg(cDim, a.d))
	}
	return cqChipRows(chips, inner)
}

// cqRestRow is one line of the queue behind the head.
//
// row() splits flex width evenly and has no ratio support, so every
// proportional column is computed as a fixed cell and exactly one column is
// flex — with a single flex cell it absorbs the remainder exactly and the row
// stays column-exact. c() reserves no inter-cell space, so gapped columns
// pre-truncate to W-1.
//
// Columns shed by width, repo first: product is the key the human scans by and
// want is the reason the row exists, so the repo name is the one to lose.
func cqRestRow(w int, r cqItem, lead string) string {
	inner := cqInner(w)
	showRepo := w >= 110
	showProduct := w >= 70

	fixed := 7 + 6
	if showProduct {
		fixed += 13
	}
	rem := maxi(0, inner-fixed)
	var titleW, repoW int
	if showRepo {
		titleW = rem * 11 / 33
		repoW = rem * 10 / 33
	} else {
		titleW = rem * 11 / 23
	}

	segs := []seg{c("", pad, ""), c(lead, 7, cFaint)}
	if showProduct {
		segs = append(segs, c(truncate(cqLabel(r.product), 12), 13, cFaint))
	}
	segs = append(segs, c(truncate(r.title, titleW-1), titleW, cMid))
	if showRepo {
		segs = append(segs, c(truncate(r.repo, repoW-1), repoW, cFaint))
	}
	segs = append(segs, flexc(r.want, cDim), cr(r.age, 6, cFaint), c("", pad, ""))
	return row(w, "", segs...)
}

// ---- mode: working ----------------------------------------------------------

// cqBlockLine is one line of the working list. isRow marks the dispatcher rows,
// so a truncated list can say how many dispatchers it hid rather than how many
// lines it cut.
type cqBlockLine struct {
	s     string
	isRow bool
}

// cqViewWorking is everything running without you, grouped by product.
//
// The design scrolls this list, but the working view has no cursor and no
// scroll key — the key handler swallows everything except the lens digits, the
// palette and the way back. So it does not fake a viewport: it shows what fits
// and counts what it could not.
func (m model) cqViewWorking(w, h int) string {
	inner := cqInner(w)

	head := []cqRow{
		cqGap(2),
		cqFixed(flG(fg(cDim, truncate(cqWorkTotalLine(), inner)))),
		cqGap(3),
	}
	tail := []cqRow{
		cqGap(4),
		cqFixed(flG(fg(cFaint, "w or esc back to the queue"))),
		cqGap(1),
	}
	head, tail = cqShedPair(head, tail, maxi(1, h-1))
	capacity := maxi(0, h-len(head)-len(tail))

	block := cqWorkLines(w, true)
	if len(block) > capacity {
		// The per-group spacer is the cheapest thing on this screen.
		block = cqWorkLines(w, false)
	}
	if len(block) > capacity {
		keep := maxi(0, capacity-1)
		gone := 0
		for _, b := range block[keep:] {
			if b.isRow {
				gone++
			}
		}
		block = append(block[:keep:keep], cqBlockLine{
			s: flG(fg(cFaint, truncate("… "+itoa(gone)+" more running", inner))),
		})
	}

	rows := make([]cqRow, 0, len(head)+len(block)+len(tail)+1)
	rows = append(rows, head...)
	for _, b := range block {
		rows = append(rows, cqFixed(b.s))
	}
	rows = append(rows, cqFill())
	rows = append(rows, tail...)
	return cqRender(rows, h)
}

// cqWorkLines flattens the product groups into lines. gaps controls the blank
// row that opens every group, including the first.
func cqWorkLines(w int, gaps bool) []cqBlockLine {
	inner := cqInner(w)
	var out []cqBlockLine
	for _, g := range cqWorking {
		if len(g.rows) == 0 {
			continue
		}
		if gaps {
			out = append(out, cqBlockLine{})
		}
		out = append(out, cqBlockLine{s: flG(flSpread(
			fg(cFaint, cqLabel(g.name)),
			fg(cFaint, itoa(len(g.rows))+" running"), inner))})
		for _, x := range g.rows {
			out = append(out, cqBlockLine{s: cqWorkRowLine(w, x), isRow: true})
		}
	}
	return out
}

// cqWorkRowLine is one running dispatcher. Same one-flex-column discipline as
// cqRestRow. Repo is again the column that goes when width runs out: the rows
// are already grouped by product, and `doing` is the only liveness signal.
func cqWorkRowLine(w int, x cqWorkRow) string {
	inner := cqInner(w)
	showRepo := w >= 110

	rem := maxi(0, inner-9)
	var featW, repoW int
	if showRepo {
		featW = rem * 11 / 31
		repoW = rem * 10 / 31
	} else {
		featW = rem * 11 / 21
	}

	out, outHex := cqOutCell(x.out)
	segs := []seg{c("", pad, ""), c(truncate(x.feature, featW-1), featW, cMid)}
	if showRepo {
		segs = append(segs, c(truncate(x.repo, repoW-1), repoW, cFaint))
	}
	segs = append(segs, flexc(x.doing, cDim), cr(out, 9, outHex), c("", pad, ""))
	return row(w, "", segs...)
}

// ---- mode: empty / draft ----------------------------------------------------

// cqViewEmpty is the dispatch prompt: what you see when the queue is clear, and
// what `d` opens over a queue that is not.
func (m model) cqViewEmpty(w, h int) string {
	inner := cqInner(w)

	head := []cqRow{
		cqGap(3), cqGap(3),
		cqFixed(flG(fg(cDim, truncate(m.cqPromptLead(), inner)))),
		cqGap(1), cqGap(1),
		cqFixed(flG(fg(cFaint, "what should we build?"))),
		cqGap(4),
		cqFixed(flG(cqDraftLine(m.cqDraft, inner))),
		cqGap(2), cqGap(2),
	}
	for _, ln := range cqChipRows(cqPromptChips(), inner) {
		head = append(head, cqFixed(flG(ln)))
	}

	tail := []cqRow{
		cqFixed(flG(fg(cFaint, truncate(cqUnattendedLine(), inner)))),
		cqGap(5),
	}

	rows := make([]cqRow, 0, len(head)+len(tail)+1)
	rows = append(rows, head...)
	rows = append(rows, cqFill())
	rows = append(rows, tail...)
	return cqRender(rows, h)
}

// cqPromptLead states where the queue stands, so the prompt never implies the
// queue is clear when `d` was pressed over a full one — and never claims it is
// clear before the first snapshot has been read.
func (m model) cqPromptLead() string {
	n := len(m.cqQueue())
	if m.cqDispatch {
		switch {
		case n == 1:
			return "1 blocker still waiting · esc goes back"
		case n > 1:
			return itoa(n) + " blockers still waiting · esc goes back"
		}
		return "queue clear"
	}
	if m.loading {
		return "reading your dispatch records, repos and forges…"
	}
	switch {
	case m.cqCleared == 1:
		return "queue clear · 1 thing handled"
	case m.cqCleared > 1:
		return "queue clear · " + itoa(m.cqCleared) + " things handled"
	}
	return "queue clear"
}

// cqDraftLine is the typed draft with the block caret after it.
//
// The caret is steady. The design blinks it, but the cockpit has no blink tick
// and adding one would cost a full redraw twice a second for a cursor the
// terminal already draws. When the draft outgrows the line it scrolls left,
// keeping the caret — the point of interest — on screen.
func cqDraftLine(draft string, inner int) string {
	caret := paint("#0a0a0a", cWhite, " ") // the design's caret: dark glyph on white
	r := []rune(draft)
	for len(r) > 0 && dispWidth(string(r))+1 > inner {
		r = r[1:]
	}
	return fg(cFg, string(r)) + caret
}

// cqPromptChips are the prompt's affordances. The backlog chip appears only
// when the backlog collector actually found untaken tickets: the design's count
// is mock, and "0 ready to go" would be a claim about a backlog nobody read.
func cqPromptChips() []string {
	chips := []string{
		fg(cFg, "⏎") + " " + fg(cDim, "dispatch"),
		fg(cFg, "tab") + " " + fg(cDim, "pick repos"),
	}
	if n := flCountTickets(func(t ticket) bool { return t.taken == "" }); n > 0 {
		chips = append(chips, fg(cFg, "5")+" "+fg(cDim, "backlog · "+itoa(n)+" ready to go"))
	}
	return chips
}

// ---- footer -----------------------------------------------------------------

// cqFooterHelp is the one chrome row this lens drives: the keys that work right
// now, which differ per mode. footerHelp() delegates here for the floor lens.
func (m model) cqFooterHelp() string {
	q := m.cqQueue()
	switch {
	case m.cqWork:
		return "w or esc back · 1…8 sections · : palette"
	case m.cqDispatch || len(q) == 0:
		if m.cqDraft != "" {
			return "enter dispatch · esc clear"
		}
		return "type the work · enter dispatch · w running · esc clear · 1…8 sections"
	}
	parts := make([]string, 0, len(q[0].acts)+4)
	for _, a := range q[0].acts {
		parts = append(parts, a.k+" "+a.d)
	}
	parts = append(parts, "d dispatch", "w running", "u undo", "? keys")
	return strings.Join(parts, " · ")
}
