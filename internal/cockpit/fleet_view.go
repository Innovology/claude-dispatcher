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

import "strings"

// ---- columns ------------------------------------------------------------------

// The eight cells of a table line, in draw order. The column header and every
// data row go through the same indices, which is the only way the two stay
// column-exact.
const (
	flGlyph = iota
	flProduct
	flFeature
	flRepo
	flStage
	flTurn
	flSignal
	flAge
	flCells
)

// fleetCols is one terminal width's column widths. A zero width means the
// column is shed at this size.
type fleetCols struct {
	glyph, product, feature, repo, stage, turn, sigPad, age int
}

// fleetColumns sizes the table for a terminal width.
//
// The design has no responsive story — its eight cells assume a browser window
// — so the columns shed on the repo's own fit() breakpoints rather than a third
// set invented here. Below 110 the repo name, the stage and the turn count go
// (cqRestRow and the v3 working row both gate the repo at 110, and the panel
// still draws the full chain for the selected row); below 70 the product label
// goes too, because a 12ch label is a fifth of a 60-column screen.
//
// Four columns never shed: the glyph, the feature, the signal and the age.
// They are how bad, what, why and how long — the reason the table exists.
//
// row() splits flex width evenly and has no ratio support, so the design's
// 1.3 : 1 : 1.4 is computed into fixed widths and exactly one column is left
// flex. With a single flex cell it absorbs the remainder exactly and the row
// stays column-exact whatever its position in the seg list.
func fleetColumns(w int) fleetCols {
	cols := fleetCols{glyph: 3, sigPad: 3, age: 6}
	showRepo := w >= 110
	if w >= 70 {
		cols.product = 12
	}
	if showRepo {
		cols.stage, cols.turn = 9, 4
	}
	fixed := cols.glyph + cols.product + cols.stage + cols.turn + cols.sigPad + cols.age
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
	v := [flCells]string{
		flProduct: "PRODUCT", flFeature: "FEATURE", flRepo: "REPO",
		flStage: "STAGE", flTurn: "TURN", flSignal: "SIGNAL", flAge: "AGE",
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
	case 2:
		return "◆"
	}
	return "·"
}

// fleetRankColor colours the glyph and the signal. Both ● and ○ are red: the
// tone decides which glyph, the rank decides the colour.
func fleetRankColor(rank int) string {
	switch {
	case rank <= 1:
		return cRed
	case rank == 2:
		return cAmber
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
	turnHex := cFaint
	if r.rank == 2 {
		turnHex = cAmber
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
		flAge:     cqAge(r.moved),
	}
	hex := [flCells]string{
		flGlyph:   rank,
		flProduct: cFaint,
		flFeature: featHex,
		flRepo:    cFaint,
		flStage:   stageHex,
		flTurn:    turnHex,
		flSignal:  rank,
		flAge:     cFaint,
	}
	return fleetLine(w, cols, bg, v, hex)
}

// ---- the header line ------------------------------------------------------------

// fleetHeadline is the sentence above the table: how much is in flight, how
// much of it wants you, and what `f` is doing.
//
// Every count is over the FILTERED rows, because the line sits on top of the
// table and describes what is on it.
func (m model) fleetHeadline(inner int, rows []fleetRow) string {
	wants, warn, clean := fleetCount(rows)
	f := m.fleetFilter()

	title, right := itoa(len(rows))+" in flight", "f filters · sorted by urgency"
	if f != fleetFilters[0] {
		title, right = itoa(len(rows))+" · "+f, "showing "+f+" · f cycles"
	}
	blockHex, warnHex := cFaint, cFaint
	if wants > 0 {
		blockHex = cRed
	}
	if warn > 0 {
		warnHex = cAmber
	}
	// The design's gap:3ch between four `flex:none` cells, then the filter line
	// pinned right; flSpread drops the right side when they collide, which is
	// the terminal answer to its text-overflow:ellipsis.
	left := fg(cFg, title) + "   " +
		fg(blockHex, itoa(wants)+" want you") + "   " +
		fg(warnHex, itoa(warn)+" need a look") + "   " +
		fg(cFaint, itoa(clean)+" running clean")
	return flG(flSpread(left, fg(cFaint, right), inner))
}

// ---- the detail panel -----------------------------------------------------------

// fleetWhyLines caps the wrapped sentence. The design wraps without an
// ellipsis; two lines is what the panel can spare before it starts eating the
// table.
const fleetWhyLines = 2

// fleetMeta is the panel's status tail: how many turns it has taken, how full
// its context was on the last assistant turn, and which model ran it. Each
// clause is dropped whole when its source said nothing.
//
// Absent, and to stay absent: "of 200k context" (the denominator is not
// knowable from a model id), "· auto" (nothing persists the mode a session was
// launched under — dxAuto is a form field, not a record field), and the
// design's check trend (one sample cannot make a trend).
func fleetMeta(r fleetRow) string {
	parts := make([]string, 0, 2)
	for _, p := range []string{cqPassLine(r.pass), cqCtxLine(r)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
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
	// render.
	if m.cqDispatch || len(m.fleetAll()) == 0 {
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
	cols := fleetColumns(w)
	rows := m.fleetRows()
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

	body := fleetBody(w, cols, rows, sel, maxi(0, h-cqSolid(layout)))
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
func fleetBody(w int, cols fleetCols, rows []fleetRow, sel, h int) []string {
	if h <= 0 {
		return nil
	}
	out := make([]string, 0, h)
	if len(rows) == 0 {
		// viewCQ would have shown the dispatch form if the fleet were empty, so
		// this is the filter — and it says which, rather than reading as
		// "nothing is running".
		out = append(out, flG(fg(cFaint, "nothing matches this filter")))
	}
	start, end := window(sel, len(rows), h)
	for i := start; i < end; i++ {
		out = append(out, fleetDataLine(w, cols, rows[i], i == sel))
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out[:h]
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
	if m.cqDispatch || len(m.fleetAll()) == 0 {
		return m.dxFooterHelp()
	}
	parts := make([]string, 0, 8)
	if r, ok := m.fleetSel(); ok {
		for _, a := range r.acts {
			parts = append(parts, a.k+" "+a.d)
		}
	}
	parts = append(parts, "j/k move", "f filter", "d dispatch", "u undo", "? keys")
	return strings.Join(parts, " · ")
}
