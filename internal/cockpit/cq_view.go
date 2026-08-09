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
// The design is a flexbox column: fixed pixel spacers rounded to rows, then —
// in the item mode — two proportional scroll panes, evidence over queue at 3:1,
// which between them absorb whatever height is left. Every spacer carries a shed
// order, so a short terminal loses whitespace before it loses a sentence
// (cqShed), and the panes shrink before any spacer does — whitespace is only the
// shape of the pane, a hidden ask is something the human never sees.
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

// cqStalledCount is how many running dispatchers are sitting on a green PR
// nobody has merged.
func cqStalledCount() int {
	n := 0
	for _, g := range cqWorking {
		for _, x := range g.rows {
			if x.stalled {
				n++
			}
		}
	}
	return n
}

// cqWorkTotalLine heads the working view. "none of them need you" is true by
// construction rather than by assertion: anything that needs you is in the
// queue, not here.
//
// The design's headline counts the ones "not converging, or green and not
// merging". Only the second half is countable — non-convergence needs a check
// result sampled over time — so the line says the half that is measured.
func cqWorkTotalLine() string {
	n := cqRunningCount()
	if n == 0 {
		return "nothing running · anything that wants you is in the queue"
	}
	s := itoa(n) + " running · none of them need you"
	if k := cqStalledCount(); k > 0 {
		s = itoa(n) + " running · " + itoa(k) + " green and not merging"
	}
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

// ---- the chain and the status strip -----------------------------------------

// cqChainCell renders plan → act → observe → ship with `phase` lit and every
// other segment — and all three arrows — inert.
//
// An empty phase lights nothing, which is the honest picture of a dispatcher we
// have read no transcript for: the chain is our inference (cqPhase), not a state
// Claude Code reports, so it must be able to say "we do not know".
func cqChainCell(phase string) string {
	s := ""
	for _, sg := range cqChainSegs(phase) {
		s += fg(sg.hex, sg.text)
	}
	return s
}

// cqChainSegs is the same chain as row() cells, for the working rows — where the
// selection highlight runs under it. A pre-coloured string cannot be nested
// inside a background cell (its own resets would punch holes in the highlight),
// so each segment is its own cell and row() paints both layers in one pass.
func cqChainSegs(phase string) []seg {
	arrow := dispWidth("→")
	col := func(name string) string {
		if name == phase {
			return cWhite
		}
		return cChainOff
	}
	return []seg{
		c("plan", 4, col("plan")), c("→", arrow, cChainOff),
		c("act", 3, col("act")), c("→", arrow, cChainOff),
		c("observe", 7, col("observe")), c("→", arrow, cChainOff),
		c("ship", 4, col("ship")),
	}
}

// cqChainWidth is the chain's rendered width. The arrows are measured, not
// counted: "→" is East-Asian ambiguous, so whether it costs one cell or two is
// the terminal's business, not ours.
func cqChainWidth() int { return 18 + 3*dispWidth("→") }

// cqPhaseWord is the chain reduced to the segment that is lit, for rows too
// narrow to carry the whole thing. A dash, not a guess, when nothing is lit.
func cqPhaseWord(phase string) string {
	if phase == "" {
		return "—"
	}
	return phase
}

// cqPassLine counts the prompts submitted to a dispatcher.
//
// It says "turn", not "pass": the design's number is repair rounds, and nothing
// in this repo records those. An advertised meaning the number does not have is
// the same bug as an advertised key that does nothing.
func cqPassLine(pass int) string {
	if pass <= 0 {
		return ""
	}
	return "turn " + itoa(pass)
}

// cqCtxLine is how full the context window was on the last assistant turn, and
// which model was in it.
//
// No percentage is printed. The occupancy is real (transcript usage), but the
// denominator is not knowable from a model id — the same id runs with a 200k or
// a 1M window depending on how the session was started — so a percentage would
// be a claim about a window nobody measured. The count alone is true.
func cqCtxLine(it cqItem) string {
	if !it.ctxKnown || it.ctxTokens <= 0 {
		return ""
	}
	s := cqTokens(it.ctxTokens) + " context"
	if it.model != "" {
		s += " · " + it.model
	}
	return s
}

func cqTokens(n int) string {
	if n >= 1000 {
		return itoa(n/1000) + "k"
	}
	return itoa(n)
}

// cqStatusStrip is the chain, the turn count and the context line on one row.
// The chain and the count are fixed; the context line is the tail that gives way
// when the terminal is narrow. Each clause is dropped whole — leading separator
// included — when its source said nothing.
func cqStatusStrip(it cqItem, inner int) string {
	s := cqChainCell(it.phase)
	for _, part := range []string{cqPassLine(it.pass), cqCtxLine(it)} {
		if part == "" {
			continue
		}
		rest := inner - dispWidth(s) - 4
		if rest <= 0 {
			break
		}
		s += "  " + fg(cDim, "· "+truncate(part, rest))
	}
	return truncateAnsi(s, inner)
}

// cqGoalLabelW is the goal row's label column: the widest label it uses
// ("prompt") plus the design's two-column gap.
const cqGoalLabelW = 8

// cqGoalRow is what the dispatcher is working towards. The label names what the
// text actually is (see cqGoal), so a prompt is never presented as a
// completion criterion; with neither recorded the row says so plainly.
func cqGoalRow(w int, it cqItem) string {
	label, text, color := it.goalLabel, it.goal, cMid
	if text == "" {
		label, text, color = "goal", "no goal set", cFaint
	}
	return row(w, "",
		c("", pad, ""),
		c(label, cqGoalLabelW, cFaint),
		flexc(text, color),
		c("", pad, ""))
}

// cqEvSignColor colours a diff line by its sign. A hunk header is structure
// rather than content, so it is faint and keeps its whole "@@ … @@" text.
func cqEvSignColor(sign string) (mark, hex string) {
	switch sign {
	case "+":
		return "+", cGreen
	case "-":
		return "-", cRed
	case "@":
		return " ", cFaint
	}
	return " ", cMid
}

// cqEvLine is one line of the evidence excerpt.
func cqEvLine(w int, e hunkLine) string {
	mark, hex := cqEvSignColor(e.sign)
	return row(w, "", c("", pad, ""), c(mark, 3, hex), flexc(e.text, hex), c("", pad, ""))
}

// cqEvPaneLines is the whole evidence excerpt: the file (or the label standing
// in for one), the note, then the lines. Every part is dropped when the
// collector had no source for it, so a dispatcher we can show nothing about
// gets an empty pane rather than a padded-out placeholder.
func cqEvPaneLines(w int, it cqItem) []string {
	inner := cqInner(w) - pad // the design's padding-right:2ch on this pane
	var out []string
	if it.evFile != "" {
		out = append(out, flG(fg(cFaint, truncate(it.evFile, inner))))
	}
	if it.evNote != "" {
		out = append(out, flG(fg(cMid, truncate(it.evNote, inner))))
	}
	if len(out) > 0 && len(it.evLines) > 0 {
		out = append(out, "")
	}
	for _, e := range it.evLines {
		out = append(out, cqEvLine(w, e))
	}
	return out
}

// cqPaneWindow clips lines to an h-row window starting at off, padding a short
// pane out so whatever sits under it keeps its place.
//
// Neither pane has a scrollbar, so an overflow in either direction is stated in
// words on the row it displaces. That costs a line of content, which is the
// right trade: a pane that silently hides half a diff is worse than one that
// says how much it is hiding.
func cqPaneWindow(lines []string, off, h int) []string {
	if h <= 0 {
		return nil
	}
	off = mini(maxi(0, off), maxi(0, len(lines)-h))
	if len(lines) <= h {
		return cqPadLines(lines, h)
	}
	win := append([]string(nil), lines[off:off+h]...)
	if off > 0 {
		win[0] = flG(fg(cFaint, "↑ "+itoa(off)+" more"))
	}
	if rest := len(lines) - off - h; rest > 0 {
		win[h-1] = flG(fg(cFaint, "↓ "+itoa(rest)+" more"))
	}
	return win
}

func cqPadLines(lines []string, h int) []string {
	out := make([]string, 0, h)
	out = append(out, lines...)
	for len(out) < h {
		out = append(out, "")
	}
	return out
}

// cqSplitPanes divides the leftover height between the evidence pane and the
// queue pane at the design's 3:1.
//
// The evidence pane never takes more rows than it has lines. The design lets a
// short pane sit half empty because it is scrolling pixels inside a fixed box;
// in a terminal those rows are simply black, and the queue behind the ask is a
// better use for them.
func cqSplitPanes(budget, evContent int) (evH, qH int) {
	if budget <= 0 {
		return 0, 0
	}
	evH = (budget*3 + 2) / 4
	if evContent < evH {
		evH = evContent
	}
	return evH, budget - evH
}

// cqMinPanes is the height the two panes are guaranteed before any spacer
// sheds — three rows of evidence and one of queue.
//
// This inverts cqShed's usual contract on purpose. Everywhere else the flex gap
// is only whitespace and collapses first; here it is the evidence, and a pane
// squeezed to nothing so a blank row can survive would be exactly backwards.
const cqMinPanes = 4

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

// cqItemLayout is the item mode's whole layout decision: the fixed rows, the
// two panes' contents, and how many rows each pane gets.
//
// It is separate from the render because the key handler needs the same answer.
// A scroll offset is only meaningful against the height it will be shown at —
// clamping j/k to the number of lines instead would leave the last screenful of
// them dead, pressing k several times before the pane moved — so the keys size
// the panes exactly as the view is about to.
type cqItemLayout struct {
	rows     []cqRow
	evLines  []string
	restRows []cqRestEntry
	evH, qH  int
}

func (m model) cqItemLayout(w, h int) cqItemLayout {
	q := m.cqQueue()
	if len(q) == 0 {
		return cqItemLayout{}
	}
	inner := cqInner(w)
	cur := q[0]

	// Everything above the evidence pane: who is asking, what they want, and the
	// two lines of context that frame it.
	head := []cqRow{
		cqFixed(flG(flSpread(
			fg(cDim, cqLabel(cur.product)),
			fg(cFaint, cqPosition(q)), inner))),
		cqGap(6),
		cqFixed(flG(flSpread(
			fg(cFg, cur.title),
			fg(cFaint, cqWhere(cur)), inner))),
		cqFixed(flG(fg(cqLeadColor(cur.tone), truncate(cqLeadText(cur), inner)))),
		cqFixed(cqGoalRow(w, cur)),
	}
	// The collector drops any evidence clause it has no source for, so an empty
	// detail means there was nothing true to say — printing a blank row instead
	// would read as "nothing to say about it", which is a different claim.
	if cur.detail != "" {
		head = append(head, cqFixed(flG(fg(cDim, truncate(cur.detail, inner)))))
	}
	head = append(head, cqGap(1))

	// Between the panes: where the work stands, and the acts that answer it.
	mid := []cqRow{cqGap(2), cqFixed(flG(cqStatusStrip(cur, inner))), cqGap(5)}
	for _, ln := range m.cqActionRows(cur, inner) {
		mid = append(mid, cqFixed(flG(ln)))
	}
	mid = append(mid,
		cqGap(3),
		cqFixed(flG(fg(cRule, strings.Repeat("─", inner)))),
		cqGap(4))

	tail := []cqRow{cqFixed(flG(truncateAnsi(
		fg(cFg, "w")+" "+fg(cDim, "everything in flight")+
			" "+fg(cFaint, "· "+cqUnattendedLine()), inner)))}

	evLines := cqEvPaneLines(w, cur)
	restRows := cqRestEntries(q)

	// The panes are the flex, so they are laid out as fill rows and sized from
	// whatever the spacers leave — but shedding stops short of cqMinPanes, so a
	// short terminal gives up whitespace before it gives up evidence.
	rows := make([]cqRow, 0, len(head)+len(mid)+len(tail)+2)
	rows = append(rows, head...)
	rows = append(rows, cqFill())
	rows = append(rows, mid...)
	rows = append(rows, cqFill())
	rows = append(rows, tail...)
	rows = cqShed(rows, maxi(1, h-cqMinPanes))

	evH, qH := cqSplitPanes(maxi(0, h-cqSolid(rows)), len(evLines))
	return cqItemLayout{rows: rows, evLines: evLines, restRows: restRows, evH: evH, qH: qH}
}

// cqViewItem is the head of the queue in full — what it wants, the evidence,
// and the acts that answer it — with the remainder listed under a rule so the
// depth behind the current ask is visible without leaving it.
func (m model) cqViewItem(w, h int) string {
	if len(m.cqQueue()) == 0 {
		return m.cqViewEmpty(w, h)
	}
	L := m.cqItemLayout(w, h)

	// The two fill rows are the panes, in order: evidence, then the queue. Both
	// pane functions clamp the model's offset against their own content, so a
	// queue that shrank under the reader lands at the bottom, never past it.
	out := make([]cqRow, 0, len(L.rows)+L.evH+L.qH)
	pane := 0
	for _, r := range L.rows {
		if !r.fill {
			out = append(out, r)
			continue
		}
		pane++
		lines := cqPaneWindow(L.evLines, m.cqEvScroll, L.evH)
		if pane == 2 {
			lines = cqRestPane(w, L.restRows, m.cqRestScroll, L.qH)
		}
		for _, ln := range lines {
			out = append(out, cqFixed(ln))
		}
	}
	return cqRender(out, h)
}

// cqScrollMax is how far each pane can be scrolled at the current terminal
// size: the lines it holds less the rows it will be given. Both are 0 when the
// content fits, which is what makes j and k inert rather than merely invisible.
func (m model) cqScrollMax() (ev, rest int) {
	L := m.cqItemLayout(m.width, m.bodyHeight())
	return maxi(0, len(L.evLines)-L.evH), maxi(0, len(L.restRows)-L.qH)
}

// cqRestEntry is one line of the queue pane: an ask still waiting, or a
// dispatcher getting on with it.
type cqRestEntry struct {
	lead, product, title, repo, want, age string
	titleColor, wantColor                 string
}

// cqRestEntries is the queue behind the head, then everything running.
//
// The running rows are not filler: a tall terminal that would otherwise end in
// black instead shows what is in flight, and they are written in faint so the
// eye still separates "waiting on you" from "getting on with it".
func cqRestEntries(q []cqItem) []cqRestEntry {
	out := make([]cqRestEntry, 0, len(q))
	for i, r := range q[1:] {
		lead := ""
		if i == 0 {
			lead = "then"
		}
		out = append(out, cqRestEntry{
			lead: lead, product: r.product, title: r.title, repo: r.repo,
			want: r.want, age: r.age, titleColor: cMid, wantColor: cDim,
		})
	}
	n := 0
	for _, g := range cqWorking {
		for _, x := range g.rows {
			lead := ""
			if n == 0 {
				lead = "running"
			}
			out = append(out, cqRestEntry{
				lead: lead, product: g.name, title: x.feature, repo: x.repo,
				want: cqRunWant(x), age: x.out, titleColor: cFaint, wantColor: cFaint,
			})
			n++
		}
	}
	return out
}

// cqRunWant says what a running dispatcher is up to, from the clauses that have
// a source: the inferred phase, the turn count, and where its PR's checks
// stand. All three can be absent, and then the column is simply empty.
func cqRunWant(x cqWorkRow) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{x.phase, cqPassLine(x.pass), x.detail} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// cqRestPane is the queue pane's window over the entries.
//
// Unlike the evidence pane, an overflow costs no row here: the lead column is
// blank on every row but the first, so "+N" and the scroll position ride there.
func cqRestPane(w int, entries []cqRestEntry, off, h int) []string {
	if h <= 0 {
		return nil
	}
	off = mini(maxi(0, off), maxi(0, len(entries)-h))
	vis := entries[off:mini(len(entries), off+h)]
	lines := make([]string, 0, h)
	for i, e := range vis {
		if off > 0 && i == 0 {
			e.lead = "↑" + itoa(off)
		}
		if hidden := len(entries) - off - len(vis); hidden > 0 && i == len(vis)-1 {
			e.lead = "+" + itoa(hidden)
		}
		lines = append(lines, cqRestRow(w, e))
	}
	return cqPadLines(lines, h)
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
	chips := make([]string, 0, len(acts)+2)
	for _, a := range acts {
		chips = append(chips, fg(cFg, a.k)+" "+fg(cDim, a.d))
	}
	// The two panes have no scrollbar, so the only thing that says they scroll
	// is this line. Both keys are bound unconditionally in item mode (see
	// updateFloorQueue), so both chips are always true.
	chips = append(chips,
		fg(cFg, "j k")+" "+fg(cDim, "evidence"),
		fg(cFg, "J K")+" "+fg(cDim, "queue"))
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
func cqRestRow(w int, r cqRestEntry) string {
	inner := cqInner(w)
	showRepo := w >= 110
	showProduct := w >= 70

	// lead + age, then the two columns that shed, leaving title and want to
	// share the remainder at the design's 1 : 1.2.
	fixed := 9 + 6
	if showProduct {
		fixed += 13
	}
	if showRepo {
		fixed += 14
	}
	rem := maxi(0, inner-fixed)
	titleW := rem * 10 / 22

	segs := []seg{c("", pad, ""), c(r.lead, 9, cFaint)}
	if showProduct {
		segs = append(segs, c(truncate(cqLabel(r.product), 12), 13, cFaint))
	}
	segs = append(segs, c(truncate(r.title, titleW-2), titleW, r.titleColor))
	if showRepo {
		segs = append(segs, c(truncate(r.repo, 12), 14, cFaint))
	}
	segs = append(segs, flexc(r.want, r.wantColor), cr(r.age, 6, cFaint), c("", pad, ""))
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
	}
	tail := []cqRow{cqFixed(flG(cqWorkFooter(inner)))}
	head, tail = cqShedPair(head, tail, maxi(1, h-1))
	capacity := maxi(0, h-len(head)-len(tail))

	sel := clampCursor(m.cqWorkCursor, cqRunningCount())
	block := cqWorkLines(w, true, sel)
	if len(block) > capacity {
		// The per-group spacer is the cheapest thing on this screen.
		block = cqWorkLines(w, false, sel)
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
func cqWorkLines(w int, gaps bool, sel int) []cqBlockLine {
	inner := cqInner(w)
	var out []cqBlockLine
	i := 0
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
			out = append(out, cqBlockLine{s: cqWorkRowLine(w, x, i == sel), isRow: true})
			i++
		}
	}
	return out
}

// cqWorkFooter is the working view's own footer: the keys that work, and a
// legend for the two columns whose meaning is not self-evident.
//
// The design offers "F follow" and a legend reading "pass = repair rounds".
// Neither survives contact with this repo: nothing here follows a session, and
// the number counts prompts, not repair rounds. Both are stated as they are.
func cqWorkFooter(inner int) string {
	left := "j k or ↑ ↓ move · ⏎ attach · x kill · w or esc back"
	right := "turn = prompts sent · then the pr's own checks"
	if dispWidth(left)+3+dispWidth(right) > inner {
		return fg(cFaint, truncate(left, inner))
	}
	return flSpread(fg(cFaint, left), fg(cFaint, right), inner)
}

// cqWorkRowLine is one running dispatcher: where it is in the loop, how many
// turns it has taken, and where its PR stands.
//
// The design has no responsive story for this row — at 81 columns of fixed
// cells there would be nothing left for the feature name on a standard
// terminal — so the columns shed in reverse order of how much they say about a
// dispatcher nobody is watching. The chain degrades to the lit segment alone
// before it disappears: one word is still true, and it is the only liveness
// signal a narrow row has left.
func cqWorkRowLine(w int, x cqWorkRow, on bool) string {
	// The selected row carries the cursor and the highlight; the leading mark
	// column doubles as the page gutter so nothing else shifts.
	bg, mark, featColor := cTransparent, " ", cMid
	if on {
		bg, mark, featColor = cSel, "▸", cWhite
	}

	var chain []seg
	chainW := 0
	switch {
	case w >= 140:
		// The design's 2ch either side; the left one comes from the feature
		// cell's own right padding, so only the trailing gap is a cell.
		chain, chainW = append(cqChainSegs(x.phase), c("", 2, "")), cqChainWidth()+2
	case w >= 90:
		hex := cChainOff
		if x.phase != "" {
			hex = cWhite
		}
		chain, chainW = []seg{c(cqPhaseWord(x.phase), 9, hex)}, 9
	}
	repoW, passW, detailW := 0, 0, 0
	if w >= 110 {
		repoW, passW = 12, 8
	}
	if w >= 170 {
		detailW = 21
	}

	featW := maxi(1, w-(3+repoW+chainW+passW+detailW+8+pad))
	out, outHex := cqOutCell(x.out)

	segs := []seg{
		c(mark, 3, cFg),
		c(truncate(x.feature, featW-2), featW, featColor),
	}
	if repoW > 0 {
		segs = append(segs, c(truncate(x.repo, repoW-2), repoW, cFaint))
	}
	segs = append(segs, chain...)
	if passW > 0 {
		segs = append(segs, cr(cqPassLine(x.pass), passW, cFaint))
	}
	if detailW > 0 {
		segs = append(segs, cr(x.detail, detailW, cqTrendColor(x)))
	}
	segs = append(segs, cr(out, 8, outHex), c("", pad, ""))
	return row(w, bg, segs...)
}

// cqTrendColor lifts the one dispatcher that wants a look: green checks on a PR
// nobody has merged. The design's other amber trigger is thrash, which needs a
// check result sampled twice over time — until something samples it, no row is
// ambered for a trend nobody measured.
func cqTrendColor(x cqWorkRow) string {
	switch {
	case x.stalled:
		return cAmber
	case x.detail == "":
		return cFaint
	}
	return cDim
}

// ---- mode: empty / dispatch form --------------------------------------------

// cqViewEmpty is the dispatch form: what you see when the queue is clear, and
// what `d` opens over a queue that is not. The form itself lives in
// dispatchx.go, which replaced the freeform draft this mode used to render.
func (m model) cqViewEmpty(w, h int) string { return m.dxView(w, h) }

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

// ---- footer -----------------------------------------------------------------

// cqFooterHelp is the one chrome row this lens drives: the keys that work right
// now, which differ per mode. footerHelp() delegates here for the floor lens.
func (m model) cqFooterHelp() string {
	q := m.cqQueue()
	switch {
	case m.cqWork:
		return "j/k move · enter attach · x kill · w or esc back"
	case m.cqDispatch || len(q) == 0:
		return m.dxFooterHelp()
	}
	parts := make([]string, 0, len(q[0].acts)+4)
	for _, a := range q[0].acts {
		parts = append(parts, a.k+" "+a.d)
	}
	parts = append(parts, "d dispatch", "w running", "u undo", "? keys")
	return strings.Join(parts, " · ")
}
