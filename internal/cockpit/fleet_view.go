package cockpit

// fleet_view.go draws the v4 triage lens: one table of everything in flight,
// with a detail panel under it for the row the cursor is on. Two mutually
// exclusive modes:
//
//	fleet  something is in flight and we are not drafting — the table
//	empty  nothing is in flight, or `d` opened the form — the dispatch form
//
// The v3 lens showed one ask at a time with an evidence excerpt; that is gone.
// The table answers "what is running" in one screen and the panel answers
// "what about this one", which is the trade the design makes: less about the
// head of the queue, everything about the fleet.
//
// The layout is the design's flexbox column rounded to rows (see cqrows.go):
// header, column header, rule, then the table as the one `flex:1` region, then
// a rule and the panel. Only the two spacers shed, and shedding stops short of
// fleetMinRows — whitespace is the shape of the table, a hidden dispatcher is
// something the human never sees.
//
// All content comes from fleet.go and cq.go, which leave out any clause they
// have no source for. Nothing is composed here that is not already true.

import (
	"strings"
	"time"

	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/effort"
)

// ---- columns ------------------------------------------------------------------

// The nine cells of a table line, in draw order. The column header and every
// data row go through the same indices, which is the only way the two stay
// column-exact.
//
// LAST and AGE are two columns rather than one composed cell ("4s·3h") because
// the numbers are only worth having if they can be scanned down the table, and
// a composite right-aligned against a ragged left-hand number cannot be. Two
// right-aligned cells put every last-seen under every other last-seen.
const (
	flGlyph = iota
	flProduct
	flFeature
	flRepo
	flStage
	flTurn
	flSignal
	flSeen
	flAge
	flCells
)

// fleetCols is one terminal width's column widths. A zero width means the
// column is shed at this size.
type fleetCols struct {
	glyph, product, feature, repo, stage, turn, sigPad, seen, age int
}

// fleetProductMin is the design's PRODUCT width, kept as the floor so a table
// of short names is laid out exactly as it was. fleetProductShare caps how much
// of the writable width the cell may claim when it grows past that: a quarter,
// so the three columns that answer what/why/how-long always keep the other
// three quarters between them.
const (
	fleetProductMin   = 12
	fleetProductShare = 4
)

// fleetProductWidth is the PRODUCT cell the rows in front of the human would
// need to print their labels in full — the longest of them plus the 1ch gap the
// cell reserves. fleetColumns takes it as a want, not an instruction.
//
// It is sized from the labels on the table rather than left at the design's
// fixed 12 because a product name is the one cell on a row with no shorter true
// form: a feature or a signal reads on into the detail panel, but "EQUESTRIAN
// PASSPORT" clipped to "EQUESTRIAN…" is clipped on every row, on every poll,
// forever — and two products that share a first eleven characters are then the
// same cell.
func fleetProductWidth(rows []fleetRow) int {
	nat := 0
	for _, r := range rows {
		nat = maxi(nat, dispWidth(cqLabel(r.product)))
	}
	return nat + 1
}

// fleetColumns sizes the table for a terminal width. want is the PRODUCT width
// the rows would like (fleetProductWidth); it is granted only as far as the
// width allows.
//
// The design has no responsive story — its eight cells assume a browser window
// — so the columns shed on the repo's own fit() breakpoints rather than a third
// set invented here. Below 110 the repo name, the stage and the turn count go
// (cqRestRow and the v3 working row both gate the repo at 110, and the panel
// still draws the full chain for the selected row); below 70 the product label
// goes too, because a 12ch label is a fifth of a 60-column screen.
//
// Five columns never shed: the glyph, the feature, the signal and both ages.
// They are how bad, what, why and how long — the reason the table exists. The
// two ages hold together at every width because they are read as a pair: "how
// long since it moved" is a different fact with and without "how long it has
// been going", and the narrow tier is exactly where a human is triaging on the
// table alone rather than opening the panel.
//
// row() splits flex width evenly and has no ratio support, so the design's
// 1.3 : 1 : 1.4 is computed into fixed widths and exactly one column is left
// flex. With a single flex cell it absorbs the remainder exactly and the row
// stays column-exact whatever its position in the seg list.
func fleetColumns(w, want int) fleetCols {
	// seen is 5 and age 6: both hold "365d" with a gap, and the wider one is
	// last so the table's right edge keeps the design's margin.
	cols := fleetCols{glyph: 3, sigPad: 3, seen: 5, age: 6}
	showRepo := w >= 110
	if w >= 70 {
		roof := maxi(fleetProductMin, cqInner(w)/fleetProductShare)
		cols.product = mini(maxi(want, fleetProductMin), roof)
	}
	if showRepo {
		cols.stage, cols.turn = 9, 4
	}
	fixed := cols.glyph + cols.product + cols.stage + cols.turn + cols.sigPad + cols.seen + cols.age
	rem := maxi(0, cqInner(w)-fixed)

	share := 13 + 14 // feature : signal
	if showRepo {
		share += 10
	}
	cols.feature = maxi(1, rem*13/share)
	if showRepo {
		cols.repo = maxi(1, rem*10/share)
	}
	return cols
}

// fleetLine composes one table line. `*{box-sizing:border-box}` is set globally
// in the design, so the padding on the feature and repo cells eats into the
// cell rather than adding to it — hence text truncated to W-2 instead of an
// extra gap cell.
func fleetLine(w int, cols fleetCols, bg string, v, hex [flCells]string) string {
	segs := []seg{c("", pad, ""), c(v[flGlyph], cols.glyph, hex[flGlyph])}
	if cols.product > 0 {
		segs = append(segs, c(truncate(v[flProduct], cols.product-1), cols.product, hex[flProduct]))
	}
	segs = append(segs, c(truncate(v[flFeature], cols.feature-2), cols.feature, hex[flFeature]))
	if cols.repo > 0 {
		segs = append(segs, c(truncate(v[flRepo], cols.repo-2), cols.repo, hex[flRepo]))
	}
	if cols.stage > 0 {
		segs = append(segs, c(v[flStage], cols.stage, hex[flStage]))
	}
	if cols.turn > 0 {
		segs = append(segs, cr(v[flTurn], cols.turn, hex[flTurn]))
	}
	segs = append(segs,
		c("", cols.sigPad, ""),
		flexc(v[flSignal], hex[flSignal]),
		cr(v[flSeen], cols.seen, hex[flSeen]),
		cr(v[flAge], cols.age, hex[flAge]),
		c("", pad, ""))
	return row(w, bg, segs...)
}

// fleetHeaderLine is the column header. It is the same seg list as a data row
// with header text and one colour, so the two can never drift apart.
//
// TURN, not the design's P: the number counts prompts submitted, not repair
// rounds (see cqPassCounts), and a one-letter header next to a design that
// calls it "pass" would re-advertise a meaning it does not have. It is four
// characters, which is the design's own cell width.
func fleetHeaderLine(w int, cols fleetCols) string {
	// LAST is the last thing this dispatcher was seen doing; AGE is how long it
	// has been alive. Neither header is "AGE" alone any more, because with two
	// numbers on the row that one word would name whichever the reader assumed.
	v := [flCells]string{
		flProduct: "PRODUCT", flFeature: "FEATURE", flRepo: "REPO",
		flStage: "STAGE", flTurn: "TURN", flSignal: "SIGNAL",
		flSeen: "LAST", flAge: "AGE",
	}
	var hex [flCells]string
	for i := range hex {
		hex[i] = cFaint
	}
	return fleetLine(w, cols, cTransparent, v, hex)
}

// fleetGlyph is the rank legend the help sheet documents. These are the table's
// own glyphs, not stateMetaBy's: they say how much a row wants you, which is
// not the same question as what status the record is in.
func fleetGlyph(rank int) string {
	switch rank {
	case 0:
		return "●"
	case 1:
		return "○"
	case fleetParkedRank:
		// The pause bars: shelved by the human, waiting for later.
		return "‖"
	}
	return "·"
}

// fleetRankColor colours the glyph and the signal. Both ● and ○ are red: the
// tone decides which glyph, the rank decides the colour.
func fleetRankColor(rank int) string {
	if rank <= 1 {
		return cRed
	}
	return cFaint
}

// fleetDataLine is one dispatcher.
func fleetDataLine(w int, cols fleetCols, r fleetRow, on bool) string {
	bg, featHex, stageHex := cTransparent, cDim, cFaint
	if r.kind == "queue" {
		featHex, stageHex = cMid, cMid
	}
	if on {
		bg, featHex = cSel, cWhite
	}
	// A dispatcher launched without CLAUDE_DISPATCHER_ID has no attributed
	// events; the cell is empty rather than a zero, which would read as a
	// measured count of none.
	turn := ""
	if r.pass > 0 {
		turn = itoa(r.pass)
	}
	rank := fleetRankColor(r.rank)

	v := [flCells]string{
		flGlyph:   fleetGlyph(r.rank),
		flProduct: cqLabel(r.product),
		flFeature: r.feature,
		flRepo:    fleetRepo(r.repo, r.product),
		flStage:   cqPhaseWord(r.stage),
		flTurn:    turn,
		flSignal:  r.signal,
		flSeen:    cqAge(r.moved),
		flAge:     cqAge(r.started),
	}
	hex := [flCells]string{
		flGlyph:   rank,
		flProduct: cFaint,
		flFeature: featHex,
		flRepo:    cFaint,
		flStage:   stageHex,
		flTurn:    cFaint,
		flSignal:  rank,
		flSeen:    cFaint,
		flAge:     cFaint,
	}
	return fleetLine(w, cols, bg, v, hex)
}

// ---- the header line ------------------------------------------------------------

// fleetHeadline is the sentence above the table: how much is in flight, how
// much of it wants you, how much of it is fine, and how big all of it is.
//
// Every figure is over the FILTERED rows, because the line sits on top of the
// table and describes what is on it.
//
// The hand-coding total is the one figure here that is not a count of rows, and
// it earns its place because the counts cannot answer "how much work is this".
// Four dispatchers can be four typo fixes or four rewritten services; the row
// count reads identically and the hours do not. It goes last, is faint, and
// keeps its "≈": it is context for the counts, never a rival to the red one.
func (m model) fleetHeadline(inner int, rows []fleetRow) string {
	f := m.fleetFilter()
	if f == fleetHistory {
		return m.fleetHistoryHeadline(inner, rows)
	}
	wants, parked, clean := fleetCount(rows)

	title, right := itoa(len(rows))+" in flight", "f filters · h history · sorted by urgency"
	if f != fleetFilters[0] {
		title, right = itoa(len(rows))+" · "+f, "showing "+f+" · f cycles"
	}
	blockHex := cFaint
	if wants > 0 {
		blockHex = cRed
	}
	// The design's gap:3ch between `flex:none` cells, then the filter line
	// pinned right; flSpread drops the right side when they collide, which is
	// the terminal answer to its text-overflow:ellipsis.
	left := fg(cFg, title) + "   " +
		fg(blockHex, itoa(wants)+" want you") + "   " +
		fg(cFaint, itoa(clean)+" running clean")
	// The shelf only earns a clause when something is on it: "0 parked" would
	// advertise a group the table is not showing.
	if parked > 0 {
		left += "   " + fg(cFaint, itoa(parked)+" parked")
	}
	// Appended only when the whole cell fits. flSpread's overflow answer is to
	// truncate the left side, which would leave "≈10h t…" hanging off the end of
	// a narrow terminal — a clause half-said is worse than one not said, and it
	// would be eating the counts it is meant to qualify.
	//
	// The right-hand hint counts as occupied width, so the estimate is never
	// what pushes it off. That hint is the only place `h history` is advertised,
	// and trading a discoverable key for a figure is not a trade worth making.
	left = fleetAppendCoded(left, right, rows, inner)
	return flG(flSpread(left, fg(cFaint, right), inner))
}

// fleetAppendCoded adds the hand-coding total to a headline's left side, but
// only when it fits alongside everything already there — the cells on the left
// and the hint pinned right. Shared by the two headlines so they cannot drift
// into two different answers about when the figure is worth its width.
func fleetAppendCoded(left, right string, rows []fleetRow, inner int) string {
	coded := fleetCoded(rows)
	if coded <= 0 {
		return left
	}
	cell := "≈" + effort.Human(coded) + " to hand-code"
	if dispWidth(left)+3+dispWidth(cell)+1+dispWidth(right) > inner {
		return left
	}
	return left + "   " + fg(cFaint, cell)
}

// fleetCoded totals the hand-coding equivalent across rows, skipping the ones
// whose diff could not be read.
//
// A total over a partial set is still the right thing to show — it is a floor,
// and the alternative is showing nothing because one branch was tidied away —
// but it is only ever the sum of what was measurable, which is why every row
// that contributes had its own diff read this same load.
func fleetCoded(rows []fleetRow) time.Duration {
	var total time.Duration
	for _, r := range rows {
		if r.codedKnown {
			total += r.coded
		}
	}
	return total
}

// fleetHistoryHeadline is the line above the history table. It answers the two
// questions history is for — how much of it there is, how much of it actually
// shipped, and what all of it amounted to — and says the way back, because `h`
// got you here.
//
// The hand-coding total earns its place here more than anywhere: history is the
// one screen where every row is finished, so the hours are the whole of what
// the work turned out to be rather than a running count. Same rule as the
// fleet's — appended only when it fits whole.
func (m model) fleetHistoryHeadline(inner int, rows []fleetRow) string {
	shipped := 0
	for _, r := range rows {
		if r.signal != "stopped" {
			shipped++
		}
	}
	left := fg(cFg, itoa(len(rows))+" finished") + "   " +
		fg(cFaint, itoa(shipped)+" shipped") + "   " +
		fg(cFaint, itoa(len(rows)-shipped)+" stopped")
	right := "⏎ resumes one · h back to the fleet"
	left = fleetAppendCoded(left, right, rows, inner)
	return flG(flSpread(left, fg(cFaint, right), inner))
}

// ---- the detail panel -----------------------------------------------------------

// fleetWhyLines caps the wrapped sentence. The design wraps without an
// ellipsis; two lines is what the panel can spare before it starts eating the
// table.
const fleetWhyLines = 2

// fleetMeta is the panel's status tail: how many turns it has taken, how full
// its context was on the last assistant turn, which model ran it, the
// permission mode it was dispatched in, and what its diff would have cost a
// senior developer to write by hand. Each clause is dropped whole when its
// source said nothing.
//
// The mode used to be in the list of things this line must never claim, because
// nothing persisted it — it was a form field and not a record field, so any
// "auto" here would have been the form's intention rather than the session's
// configuration. It is on the record now, and it is a fact about the dispatcher
// the human otherwise has no way to see once the form is closed. A record
// written before the mode was a choice still carries none, and still says
// nothing rather than the default.
//
// The hand-coding clause goes last on purpose. Everything before it is a
// reading and it alone is an estimate, so it sits after them rather than among
// them, and carries its own "≈" (cqCodedLine) rather than borrowing their
// authority.
//
// Absent, and to stay absent: "of 200k context" (the denominator is not
// knowable from a model id) and the design's check trend (one sample cannot
// make a trend).
func fleetMeta(r fleetRow) string {
	parts := make([]string, 0, 6)
	for _, p := range []string{
		cqPassLine(r.pass), cqCtxLine(r), fleetModeLine(r.mode),
		fleetFanLine(r.fanOut), cqAgentsLine(r), cqCodedLine(r),
	} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// fleetModeLine names the permission mode a dispatcher was launched in, and
// says nothing at all for a record that never recorded one.
func fleetModeLine(mode string) string {
	if !dispatchpkg.Mode(mode).Known() {
		return ""
	}
	return mode
}

// fleetFanLine says the dispatch went out with the FAN OUT switch on. It sits
// with the mode because it is the same kind of fact — how the dispatch was
// configured, not what the session has done; the subagent clause beside it
// (cqAgentsLine) is the measurement. A record from before the switch existed
// says nothing.
func fleetFanLine(fanOut bool) string {
	if !fanOut {
		return ""
	}
	return "fan-out"
}

// fleetDetail is everything under the table about the selected row: what it is
// and where, why it is on this screen, what it was asked to do, and where it
// has got to.
//
// A running row usually has no sentence at all — it is not asking for anything
// — and then the row is dropped rather than filled with the design's invented
// prose ("Working through its loop", "Worth a look before it burns more
// context"). See fleetRunRow.
func (m model) fleetDetail(w int, r fleetRow) []cqRow {
	inner := cqInner(w)
	out := make([]cqRow, 0, 5)
	out = append(out, cqFixed(flG(flSpread(
		fg(cFg, r.feature), fg(cFaint, cqWhere(r)), inner))))

	if r.why != "" {
		lines := backlogWrap(r.why, inner)
		if len(lines) > fleetWhyLines {
			// Say that it was cut. Dropping the rest silently would make a
			// half-sentence read as the whole thing.
			lines = lines[:fleetWhyLines]
			lines[fleetWhyLines-1] = truncate(lines[fleetWhyLines-1]+" …", inner)
		}
		for _, ln := range lines {
			out = append(out, cqFixed(flG(fg(cqLeadColor(r.tone), ln))))
		}
	}
	out = append(out, cqFixed(cqGoalRow(w, r)))

	// The chain rides in a row() rather than a pre-coloured string so the panel
	// can carry a background later without punching holes in it.
	segs := []seg{c("", pad, "")}
	segs = append(segs, cqChainSegs(r.stage)...)
	segs = append(segs, c("", 2, ""), flexc(fleetMeta(r), cFaint), c("", pad, ""))
	out = append(out, cqFixed(row(w, "", segs...)))

	// The fan-out by name — "seeing them", where the meta clause above only
	// counts them. One line, present only while there is a fan-out to name.
	if ag := cqAgentsDetail(r); ag != "" {
		out = append(out, cqFixed(flG(fg(cFaint,
			truncate("subagents · "+ag, inner)))))
	}

	if m.cqFlash != "" {
		// A keep act (attach) did not clear anything, so it reports in mid grey
		// rather than the green that means "one fewer thing wants you".
		col := cGreen
		if m.cqFlashKeep {
			col = cMid
		}
		out = append(out, cqFixed(flG(fg(col, truncate(m.cqFlash, inner)))))
	}
	return out
}

// ---- entry ------------------------------------------------------------------

// viewCQ picks the mode and renders it. lensBody calls it for the triage lens.
func (m model) viewCQ(w, h int) string {
	// The gate is the UNFILTERED fleet. The design tests its filtered row count,
	// so narrowing to `running` with nothing running dropped the human into the
	// dispatch form and its own "nothing matches this filter" line could never
	// render. cqFormOn also holds the form back on the history filter, which is
	// a table of its own rather than a narrowing of the fleet.
	if m.cqFormOn() {
		return m.cqViewEmpty(w, h)
	}
	return m.viewFleet(w, h)
}

// fleetMinRows is the height the table is guaranteed before any spacer sheds.
//
// This inverts cqShed's usual contract on purpose. Everywhere else the flex
// region is whitespace and collapses first; here it is the content, and a table
// squeezed to nothing so a blank row could survive would be exactly backwards.
const fleetMinRows = 3

// viewFleet is the table and the panel.
func (m model) viewFleet(w, h int) string {
	inner := cqInner(w)
	rows := m.fleetRows()
	// Sized from the filtered rows — the ones actually on the table. The header
	// goes through the same cols, so the two cannot drift.
	cols := fleetColumns(w, fleetProductWidth(rows))
	sel := clampCursor(m.fleetCursor, len(rows))
	rule := flG(fg(cRule, strings.Repeat("─", inner)))

	layout := []cqRow{
		cqFixed(m.fleetHeadline(inner, rows)),
		cqGap(1),
		cqFixed(fleetHeaderLine(w, cols)),
		cqFixed(rule),
		cqFill(),
	}
	// With no selected row there is nothing to put under the rule, so the rule
	// and its spacer go too rather than closing off an empty panel.
	if r, ok := m.fleetSel(); ok {
		layout = append(layout, cqFixed(rule), cqGap(2))
		layout = append(layout, m.fleetDetail(w, r)...)
	}
	layout = cqShed(layout, maxi(1, h-fleetMinRows))

	empty := "nothing matches this filter"
	if m.fleetFilter() == fleetHistory {
		empty = "no dispatcher has finished yet · h goes back to the fleet"
	}
	body := fleetBody(w, cols, rows, sel, maxi(0, h-cqSolid(layout)), empty)
	out := make([]cqRow, 0, len(layout)+len(body))
	for _, r := range layout {
		if !r.fill {
			out = append(out, r)
			continue
		}
		for _, ln := range body {
			out = append(out, cqFixed(ln))
		}
	}
	return cqRender(out, h)
}

// fleetBody is the table's own rows, windowed on the cursor.
//
// There is no "↑ N more" / "↓ N more" sacrificial row: window() keeps the
// cursor on screen and j/k move the cursor rather than a scroll offset, so no
// row is unreachable and none of the height needs spending to say so.
//
// The parked group gets the one header the table has: a divider line naming
// the shelf, drawn above the first parked row. It is a display line, never a
// row — the cursor cannot land on it — so the window is computed over lines
// and the selection mapped across the divider. fleetSort and fleetAll keep the
// parked rows contiguous at the bottom, which is what lets one divider be the
// whole boundary.
func fleetBody(w int, cols fleetCols, rows []fleetRow, sel, h int, empty string) []string {
	if h <= 0 {
		return nil
	}
	out := make([]string, 0, h)
	if len(rows) == 0 {
		// viewCQ would have shown the dispatch form if the fleet were empty, so
		// this is the filter — and the caller's line says which, rather than
		// reading as "nothing is running".
		out = append(out, flG(fg(cFaint, empty)))
	}
	div := -1
	for i, r := range rows {
		if r.kind == "parked" {
			div = i
			break
		}
	}
	lines, selLine := len(rows), sel
	if div >= 0 {
		lines++
		if sel >= div {
			selLine++
		}
	}
	start, end := window(selLine, lines, h)
	for ln := start; ln < end; ln++ {
		i := ln
		if div >= 0 {
			if ln == div {
				out = append(out, fleetParkedDivider(w))
				continue
			}
			if ln > div {
				i = ln - 1
			}
		}
		out = append(out, fleetDataLine(w, cols, rows[i], i == sel))
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
}

// fleetParkedDivider is the line above the parked group: the group's name, set
// into a rule so it reads as a boundary rather than as a row.
func fleetParkedDivider(w int) string {
	lead, label := "── ", "parked "
	rest := maxi(0, cqInner(w)-dispWidth(lead)-dispWidth(label))
	return flG(fg(cRule, lead) + fg(cFaint, label) + fg(cRule, strings.Repeat("─", rest)))
}

// ---- footer -----------------------------------------------------------------

// cqFooterHelp is the one chrome row this lens drives: the keys that work right
// now. The row verbs come from the selected row, so they change with the
// cursor — a running dispatcher has nothing to approve and nothing to skip.
//
// The design also offers "F follow". Nothing in this repo tails a session
// without attaching to it, so it is not advertised here, in the help sheet or
// in the key handler.
func (m model) cqFooterHelp() string {
	if m.parkOpen {
		return "type the reason · enter parks it · esc cancels"
	}
	if m.cqFormOn() {
		return m.dxFooterHelp()
	}
	parts := make([]string, 0, 8)
	if r, ok := m.fleetSel(); ok {
		for _, a := range r.acts {
			parts = append(parts, a.k+" "+a.d)
		}
	}
	if m.fleetFilter() == fleetHistory {
		// No f, no d, no undo: none of them act on a finished dispatcher, and the
		// footer only names keys that work right now.
		return strings.Join(append(parts, "j/k move", "h back to the fleet", "? keys"), " · ")
	}
	parts = append(parts, "j/k move", "f filter", "h history", "d dispatch", "ctrl+z undo", "? keys")
	return strings.Join(parts, " · ")
}
