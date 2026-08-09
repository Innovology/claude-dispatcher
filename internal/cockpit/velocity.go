package cockpit

import (
	"sort"
	"strings"
)

// velocity.go is lens 8: the velocity lens. It answers one question — what
// actually reached production — beside the DORA and factory metrics that
// explain why. Display-only, no key handling. Mirrors the design's OUTPUT,
// DORA and NOT_VELOCITY blocks and the isVelocity template.
//
// Two panes: the left (~46%) is "output · the only thing that counts as
// velocity" — the live-features headline, the shipped-of-dispatched table,
// where a feature's time actually goes, and the busy-not-velocity list. The
// right is "delivery · dora" — org DORA tiles, the factory's own metrics, and a
// by-week table whose columns are chosen to fit the width.

// ---- view-model types -------------------------------------------------------

// doraMetric is one metric tile. org metrics carry a delta and an up flag
// (green when up, amber when not); factory metrics leave delta "" and up false.
type doraMetric struct {
	key, v, unit, band, delta, spark, note string
	up                                     bool
}

// splitPart is one slice of where a feature's lead time goes.
type splitPart struct {
	label string
	pct   int
	color string
}

// doraWeek is one row of the by-week DORA table. best marks the current week.
type doraWeek struct {
	w                                string
	deploys                          int
	lead, fail, restore, first, wait string
	best                             bool
}

// outWeek is one row of the shipped-of-dispatched table.
type outWeek struct {
	w                string
	live, dispatched int
}

// ---- seed data --------------------------------------------------------------

// Filled by collectVelocity. See data.go.
var (
	outputHeadline string
	outputUnit     string
	outputDelta    string
	outputSpark    string
)

type notVelocityRow struct{ k, v, why string }

// velCol is one column of the by-week table, with a rank the design uses to
// pick which columns survive when width is short (lower rank = kept first).
type velCol struct {
	key, label string
	rank       int
}

var velColsAll = []velCol{
	{"deploys", "DEPLOYS", 6},
	{"lead", "LEAD", 2},
	{"fail", "FAIL", 4},
	{"restore", "RESTORE", 7},
	{"first", "1st PASS", 5},
	{"wait", "WAIT ON YOU", 1},
}

// ---- view -------------------------------------------------------------------

// viewVelocity renders the two-pane velocity lens. On a narrow terminal only
// the left (output) pane is shown, matching the design's fit collapse.
func (m model) viewVelocity(w, h int) string {
	if !m.fit().showDetail {
		left := gutter(vjoin(m.velLeft(w-pad)...), pad)
		return clampLines(left, h)
	}

	leftW := w * 46 / 100
	rightW := w - leftW - 1 // 1 col for the vrule

	left := gutter(vjoin(m.velLeft(leftW-pad)...), pad)
	right := gutter(vjoin(m.velRight(rightW-pad)...), pad)

	out := hjoin(padBlockTo(left, h), vrule(h, cRule), padBlockTo(right, h))
	return clampLines(out, h)
}

// velLeft builds the output pane content lines to inner width iw.
func (m model) velLeft(iw int) []string {
	var out []string

	out = append(out, line("output · the only thing that counts as velocity", iw, cDim, ""))
	out = append(out, row(iw, "",
		c(outputHeadline+"  ", dispWidth(outputHeadline)+2, cWhite),
		flexc(outputUnit, cMid),
		cr(outputSpark, dispWidth(outputSpark), cGreen),
	))
	out = append(out, line(outputDelta, iw, cGreen, ""))
	out = append(out, "")

	// shipped-of-dispatched table.
	out = append(out, row(iw, "",
		c("WEEK", 11, cFaint),
		cr("LIVE", 7, cFaint),
		flexc("  SHIPPED OF DISPATCHED", cFaint),
		cr("RATE", 7, cFaint),
	))
	for n, wk := range outputWeeks {
		rate := pct(wk.live, wk.dispatched)
		rowColor, barColor := cFg, cFillGreen
		if n == 0 {
			rowColor, barColor = cWhite, cGreen
		}
		rateColor := cAmber
		if rate >= 50 {
			rateColor = cGreen
		} else if rate >= 40 {
			rateColor = cMid
		}
		out = append(out, row(iw, "",
			c(wk.w, 11, rowColor),
			cr(itoa(wk.live), 7, rowColor),
			flexc("  "+bar(rate, 16), barColor),
			cr(itoa(rate)+"%", 7, rateColor),
		))
	}
	out = append(out, "")

	// where a feature's time actually goes.
	out = append(out, line("where a feature's time actually goes", iw, cDim, ""))
	for _, sp := range doraSplit {
		out = append(out, row(iw, "",
			c(sp.label, 16, cMid),
			flexc("  "+bar(sp.pct*2, 18), sp.color),
			cr(itoa(sp.pct)+"%", 5, sp.color),
		))
	}
	for _, l := range velWrap("A third of every feature's life is spent waiting for you. Nothing you change about the agents moves that number.", iw, 3) {
		out = append(out, line(l, iw, cAmber, ""))
	}
	out = append(out, "")

	// busy, not velocity.
	out = append(out, line("busy, not velocity", iw, cDim, ""))
	for _, nv := range notVelocity {
		out = append(out, row(iw, "",
			c(nv.k, 19, cFaint),
			c(nv.v, 15, cDim),
			flexc(nv.why, cFaint),
		))
	}
	return out
}

// velRight builds the delivery/DORA pane content lines to inner width iw.
func (m model) velRight(iw int) []string {
	var out []string

	out = append(out, row(iw, "",
		flexc("delivery · dora", cDim),
		cr("twelve weeks", 12, cFaint),
	))
	out = append(out, "")
	out = append(out, velTilesBlock(doraOrg, iw, true)...)

	out = append(out, velLaggardBlock(iw)...)

	out = append(out, line("the factory's own metrics", iw, cDim, ""))
	out = append(out, "")
	out = append(out, velTilesBlock(doraFactory, iw, false)...)

	out = append(out, line("by week", iw, cDim, ""))
	cols := velColumns(iw, m.fit().vel)

	hdr := []seg{flexc("WEEK", cFaint)}
	for _, col := range cols {
		hdr = append(hdr, cr(col.label, 11, cFaint))
	}
	out = append(out, row(iw, "", hdr...))

	for n, wk := range doraWeeks {
		rc := cFg
		if n == 0 {
			rc = cWhite
		}
		cells := []seg{flexc(wk.w, rc)}
		for _, col := range cols {
			cc := rc
			if col.key == "wait" {
				cc = cAmber
			}
			cells = append(cells, cr(velCellValue(wk, col.key), 11, cc))
		}
		out = append(out, row(iw, "", cells...))
	}
	return out
}

// velLaggardBlock is the "slowest product" read-out: which product is slowest
// from dispatch to live, named against the fastest so the figure is a
// comparison rather than a bare number — a ratio against a one-repo product is
// too fragile to state.
//
// Only products that can be ranked take part: one with no repos has no lead
// time to have, and one where nothing has reached live has none yet. When
// nothing survives that filter the block says which of the two it is instead of
// picking a winner out of an empty set.
func velLaggardBlock(iw int) []string {
	out := []string{"", fg(cRule, strings.Repeat("─", iw)), ""}

	var ranked []product
	for _, p := range products {
		if p.repos == prodRepoCount(0) || p.leadDur <= 0 {
			continue
		}
		ranked = append(ranked, p)
	}

	if len(ranked) == 0 {
		out = append(out, line("velocity per product", iw, cDim, ""))
		msg := "No products yet — assign repos to products on 2 and this measures each one."
		if len(products) > 0 {
			msg = "No product has had a feature reach live yet — this measures dispatch → live, per product."
		}
		for _, l := range velWrap(msg, iw, 3) {
			out = append(out, line(l, iw, cFaint, ""))
		}
		return append(out, "")
	}

	worst, best := ranked[0], ranked[0]
	for _, p := range ranked[1:] {
		if p.leadDur > worst.leadDur {
			worst = p
		}
		if p.leadDur < best.leadDur {
			best = p
		}
	}

	slow := worst.name + " · lead " + worst.lead
	if len(ranked) == 1 {
		// With one product there is nothing to be slowest of; say so rather
		// than compare it with itself.
		slow += " · the only product with a lead time yet"
	} else {
		slow += " · slowest of " + itoa(len(ranked)) + " products · " + best.name + " ships in " + best.lead
	}
	out = append(out, line("slowest product", iw, cDim, ""))
	out = append(out, line(slow, iw, cAmber, ""))
	return append(out, "")
}

// velTilesBlock lays out metric tiles two-across (one-across when the column
// would be too narrow), each with a band-coloured top rule. org tiles carry a
// delta line; factory tiles do not.
func velTilesBlock(tiles []doraMetric, iw int, org bool) []string {
	cols := 2
	colW := iw / 2
	if colW < 22 {
		cols, colW = 1, iw
	}

	var out []string
	for i := 0; i < len(tiles); i += cols {
		if cols == 1 {
			out = append(out, velTileLines(tiles[i], colW, org)...)
			out = append(out, "")
			continue
		}
		left := velTileLines(tiles[i], colW, org)
		var right []string
		if i+1 < len(tiles) {
			right = velTileLines(tiles[i+1], iw-colW, org)
		}
		n := maxi(len(left), len(right))
		for j := 0; j < n; j++ {
			l, r := "", ""
			if j < len(left) {
				l = left[j]
			}
			if j < len(right) {
				r = right[j]
			}
			out = append(out, padTo(l, colW, alignLeft)+r)
		}
		out = append(out, "")
	}
	return out
}

// velTileLines renders one metric tile to inner content width cw-3 (leaving the
// design's 3ch right gutter): band top rule, key, value+unit+spark, an optional
// delta, and a two-line note.
func velTileLines(mtr doraMetric, cw int, org bool) []string {
	inner := cw - 3
	if inner < 1 {
		inner = cw
	}
	bc := bandColor[mtr.band]

	var out []string
	out = append(out, fg(bc, strings.Repeat("▔", inner)))
	out = append(out, line(mtr.key, inner, cFaint, ""))
	out = append(out, row(inner, "",
		c(mtr.v+" ", dispWidth(mtr.v)+1, cWhite),
		flexc(mtr.unit, cMid),
		cr(mtr.spark, dispWidth(mtr.spark), bc),
	))
	if org {
		dc := cAmber
		if mtr.up {
			dc = cGreen
		}
		out = append(out, line(mtr.delta, inner, dc, ""))
	}
	nl := velWrap(mtr.note, inner, 2)
	for len(nl) < 2 {
		nl = append(nl, "")
	}
	for _, l := range nl {
		out = append(out, line(l, inner, cFaint, ""))
	}
	return out
}

// velColumns picks the by-week columns for width iw and fit tier velFit: the
// design keeps max(3, velFit-1) columns ranked by importance, then drops any
// that would not fit, and finally restores the original column order.
func velColumns(iw, velFit int) []velCol {
	n := maxi(3, velFit-1)
	if byWidth := (iw - 10) / 11; byWidth < n {
		n = byWidth
	}
	if n < 1 {
		n = 1
	}
	if n > len(velColsAll) {
		n = len(velColsAll)
	}

	ranked := make([]velCol, len(velColsAll))
	copy(ranked, velColsAll)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].rank < ranked[j].rank })
	pick := ranked[:n]

	order := map[string]int{}
	for i, vc := range velColsAll {
		order[vc.key] = i
	}
	sort.SliceStable(pick, func(i, j int) bool { return order[pick[i].key] < order[pick[j].key] })
	return pick
}

// velCellValue reads the by-week cell for column key from a week row.
func velCellValue(w doraWeek, key string) string {
	switch key {
	case "deploys":
		return itoa(w.deploys)
	case "lead":
		return w.lead
	case "fail":
		return w.fail
	case "restore":
		return w.restore
	case "first":
		return w.first
	case "wait":
		return w.wait
	}
	return ""
}

// velWrap greedily word-wraps s to width w over at most maxLines lines,
// ellipsising the last line when the text overruns. Lens-local twin of the
// design's text-wrap:pretty clamp.
func velWrap(s string, w, maxLines int) []string {
	if w <= 0 {
		return []string{""}
	}
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, word := range words {
		cand := word
		if cur != "" {
			cand = cur + " " + word
		}
		if dispWidth(cand) <= w {
			cur = cand
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		cur = word
		if len(lines) == maxLines-1 {
			break
		}
	}
	if cur != "" && len(lines) < maxLines {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	if dispWidth(strings.Join(lines, " ")) < dispWidth(s) {
		last := lines[len(lines)-1]
		lines[len(lines)-1] = truncate(last+" …", w)
	}
	return lines
}
