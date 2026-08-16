package cockpit

// cqrows.go is the triage lens's shared drawing kit — the part both the fleet
// table (fleet_view.go) and the dispatch form (dispatchx.go) sit on.
//
// Its core is the row list. The design is a flexbox column: fixed pixel spacers
// rounded to rows, then one or more `flex:1` regions that absorb whatever
// height is left. A cqRow carries which of those it is, and cqRender turns the
// list into exactly h lines. Every spacer carries a shed order, so a short
// terminal loses whitespace before it loses a sentence.
//
// The rest is the cells more than one mode needs: the product label, the
// locator line, the plan → act → observe → ship chain, the goal row and the two
// lines that close the dispatch form. Nothing is invented here; where a field
// is empty the row is dropped rather than padded out.

import (
	"strings"

	"claude-dispatcher/internal/effort"
)

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

// ---- shared cells -------------------------------------------------------------

// cqLeadColor maps a row's tone to the colour its sentence is written in. A
// normal tone is dim, not foreground: the sentence is the quiet explanation
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

// cqWhere is the selected row's locator. fleetRow carries the facts separately
// so the view can drop the parts that are missing — a dispatch that never
// branched has no ref, and closing the gap beats printing "· ·".
//
// The PR reference rides here and nowhere else on this screen. The v3 item
// header that used to carry it is gone with the one-at-a-time queue, and a PR
// number is the handle you would actually type into gh.
// The panel has room for words the table's two age columns do not, so it spends
// them: bare "4s · 3h" here would be two numbers with nothing saying which is
// which, where the table has LAST and AGE standing over them.
func cqWhere(r fleetRow) string {
	parts := make([]string, 0, 5)
	for _, p := range []string{cqLabel(r.product), r.repo, r.ref} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if a := cqAge(r.moved); a != "" {
		parts = append(parts, "moved "+a+" ago")
	}
	if a := cqAge(r.started); a != "" {
		parts = append(parts, a+" old")
	}
	return strings.Join(parts, " · ")
}

// ---- the chain and the status strip -----------------------------------------

// cqChainSegs is plan → act → observe → ship as row() cells, with `phase` lit
// and every other segment — and all three arrows — inert.
//
// They are cells rather than one pre-coloured string so a selection highlight
// can run under them: a coloured string's own resets would punch holes in the
// background, while row() paints both layers in one pass.
//
// An empty phase lights nothing, which is the honest picture of a dispatcher we
// have read no transcript for: the chain is our inference (cqPhase), not a state
// Claude Code reports, so it must be able to say "we do not know".
func cqChainSegs(phase string) []seg {
	arrow := dispWidth("→")
	col := func(name string) string {
		if name == phase {
			return cWhite
		}
		return cFaint
	}
	return []seg{
		c("plan", 4, col("plan")), c("→", arrow, cChainArrow),
		c("act", 3, col("act")), c("→", arrow, cChainArrow),
		c("observe", 7, col("observe")), c("→", arrow, cChainArrow),
		c("ship", 4, col("ship")),
	}
}

// cqPhaseWord is the chain reduced to the segment that is lit, for the STAGE
// column. A dash, not a guess, when nothing is lit.
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
// a 1M window depending on how the session was started — so the design's
// "of 200k" would be a claim about a window nobody measured. The count alone is
// true.
func cqCtxLine(r fleetRow) string {
	if !r.ctxKnown || r.ctxTokens <= 0 {
		return ""
	}
	s := cqTokens(r.ctxTokens) + " context"
	if r.model != "" {
		s += " · " + r.model
	}
	return s
}

// cqCodedLine is the row's hand-coding equivalent: how long a senior developer
// would have taken to write this branch's diff by hand.
//
// It is the only clause in the status tail that is a model rather than a
// reading, so it always carries the "≈" that says so, and the verb says what
// the number counts — hands on a keyboard, not what the dispatcher spent. See
// internal/effort for the model itself.
//
// Two different silences, both correct: a dispatcher whose diff could not be
// read has no clause, and one that has committed nothing yet has none either.
// "≈0m to hand-code" is true and tells the reader nothing.
func cqCodedLine(r fleetRow) string {
	if !r.codedKnown || r.coded <= 0 {
		return ""
	}
	return "≈" + effort.Human(r.coded) + " to hand-code"
}

func cqTokens(n int) string {
	if n >= 1000 {
		return itoa(n/1000) + "k"
	}
	return itoa(n)
}

// cqGoalLabelW is the goal row's label column: the widest label it uses
// ("prompt") plus the design's two-column gap.
const cqGoalLabelW = 8

// cqGoalRow is what the dispatcher is working towards. The label names what the
// text actually is (see cqGoal), so a prompt is never presented as a
// completion criterion; with neither recorded the row says so plainly.
func cqGoalRow(w int, r fleetRow) string {
	label, text, color := r.goalLabel, r.goal, cMid
	if text == "" {
		label, text, color = "goal", "no goal set", cFaint
	}
	return row(w, "",
		c("", pad, ""),
		c(label, cqGoalLabelW, cFaint),
		flexc(text, color),
		c("", pad, ""))
}

// ---- the lines that close the dispatch form ----------------------------------

// cqUnattendedLine is what is getting on with it while you work the table. The
// last-output clause is dropped when no session has a transcript we could read,
// rather than answered with a different number.
func cqUnattendedLine() string {
	n := fleetRunning()
	if n == 0 {
		return "nothing running unattended"
	}
	s := itoa(n) + " running unattended"
	if a := cqAge(cqLastOutput); a != "" {
		s += " · last output " + a + " ago"
	}
	// `w` no longer opens a view of its own — it narrows the table to the
	// running rows — so the sentence advertises what the key now does.
	// `f` reaches the running filter; `w` used to be a shortcut to it and is
	// gone, so this no longer names a key that does nothing.
	return s + " · f filters to them"
}

// cqViewEmpty is the dispatch form: what you see when nothing is in flight, and
// what `d` opens over a fleet that is not empty. The form itself lives in
// dispatchx.go.
func (m model) cqViewEmpty(w, h int) string { return m.dxView(w, h) }

// cqPromptLead states where the fleet stands, so the form never implies nothing
// is waiting when `d` was pressed over a table full of asks — and never claims
// the fleet is clear before the first snapshot has been read.
func (m model) cqPromptLead() string {
	n := 0
	for _, r := range m.fleetAll() {
		if r.kind == "queue" {
			n++
		}
	}
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
