package cockpit

// boot_view.go draws the opening screen: a block wordmark over a POST list of
// the load's real stages, a progress meter and the way past it.
//
// The reference is a 90s console boot — chunky letters, a colour sweep, a
// diagnostic list ticking off with times beside it, "press any key". What keeps
// it honest is that the list is loadSnapshot's actual stages reporting as they
// run, and the times are measured, not scripted.

import (
	"strconv"
	"strings"

	"claude-dispatcher/internal/version"
)

// bootLeader is the dotted rail between a step's label and its figure. It has
// to read as texture at a glance and never compete with the figure itself, so
// it sits between cRule (a border, invisible as type) and cFaint (which the
// footer already uses for real words).
const bootLeader = "#243244"

// bootRamp is the wordmark's vertical gradient, violet at the top through to
// the cyan the cockpit uses for anything in flight. While the load is running
// the ramp is offset by the frame so the colour sweeps down the letters; on
// READY it settles.
var bootRamp = []string{"#a78bfa", "#8ba3fb", "#6ebcf5", "#4ac8f2", "#22d3ee"}

// bootFont is a 5-row block face, 6 columns per letter with 2-column stems, for
// the eight letters the wordmark needs. Wider stems are what make it read as a
// console logo rather than as ASCII art.
var bootFont = map[rune][5]string{
	'D': {"█████ ", "██  ██", "██  ██", "██  ██", "█████ "},
	'I': {"██████", "  ██  ", "  ██  ", "  ██  ", "██████"},
	'S': {"██████", "██    ", "██████", "    ██", "██████"},
	'P': {"██████", "██  ██", "██████", "██    ", "██    "},
	'A': {"██████", "██  ██", "██████", "██  ██", "██  ██"},
	'T': {"██████", "  ██  ", "  ██  ", "  ██  ", "  ██  "},
	'C': {"██████", "██    ", "██    ", "██    ", "██████"},
	'H': {"██  ██", "██  ██", "██████", "██  ██", "██  ██"},
}

const (
	bootWord      = "DISPATCH"
	bootFontRows  = 5
	bootWordWidth = len(bootWord)*6 + (len(bootWord) - 1) // letters plus 1-col gaps
	bootMaxWidth  = 84                                    // the block never sprawls on a wide terminal
	bootLabelW    = 18                                    // where the dotted rail starts
	bootMeterW    = 56                                    // the meter is a gauge, not a horizon

	// bootMinSteps is how much of the sequence has to stay visible for the big
	// wordmark to be worth its five rows. Below it the compact mark takes over:
	// the list is the part carrying information, and a logo that squeezes it to
	// three lines has the priority backwards.
	bootMinSteps = 5
)

// viewBoot renders the whole opening screen into w×h.
func (m model) viewBoot(w, h int) string {
	b := m.boot
	if b == nil {
		return ""
	}
	inner := mini(maxi(w-2*pad, 1), bootMaxWidth)
	foot := bootFoot(b, inner)

	// The big wordmark needs the columns to hold it and enough rows left over
	// for the sequence to still be worth reading. Everything below the list is
	// fixed, so whatever the head and foot leave is the list's — and when that
	// is less than the whole sequence the list scrolls to keep the step being
	// worked on in view.
	head := bootHead(b, inner, inner >= bootWordWidth)
	budget := h - len(head) - len(foot)
	if budget < bootMinSteps {
		head = bootHead(b, inner, false)
		budget = h - len(head) - len(foot)
	}
	steps := bootStepLines(b, inner, maxi(budget, 1))

	body := append(append(head, steps...), foot...)

	// Centre the block vertically, then place it at a common left margin so the
	// rule, the rail and the meter all share one edge.
	left := strings.Repeat(" ", maxi((w-inner)/2, 0))
	out := make([]string, 0, h)
	for i := 0; i < maxi((h-len(body))/2, 0); i++ {
		out = append(out, "")
	}
	for _, ln := range body {
		if ln == "" {
			out = append(out, "") // no line is padded out to trailing whitespace
			continue
		}
		out = append(out, left+ln)
	}
	if len(out) > h {
		out = out[:h]
	}
	return strings.Join(out, "\n")
}

// bootHead is the wordmark, the product line and the rule above the sequence.
func bootHead(b *bootState, inner int, big bool) []string {
	var out []string
	if big {
		for _, r := range bootWordmark(b) {
			out = append(out, bootCentre(r, inner))
		}
		out = append(out, "")
	} else {
		out = append(out, bootCentre(fg(bootRamp[0], "⚡ D I S P A T C H"), inner), "")
	}

	// The build is named here rather than in the sequence: which supervisor and
	// which forge are findings the load reports, but which binary is running is
	// known before a single step has run.
	out = append(out, bootCentre(fg(cDim, "CLAUDE DISPATCHER ")+fg(cFaint, "· "+version.Display()), inner))
	out = append(out, "", fg(cRule, strings.Repeat("─", inner)), "")
	return out
}

// bootWordmark draws the block letters, one colour per row, sweeping while the
// load runs and settled once it is done.
func bootWordmark(b *bootState) []string {
	rows := make([]string, bootFontRows)
	for r := 0; r < bootFontRows; r++ {
		var line strings.Builder
		for i, ch := range bootWord {
			if i > 0 {
				line.WriteString(" ")
			}
			g, ok := bootFont[ch]
			if !ok {
				continue
			}
			line.WriteString(g[r])
		}
		shift := 0
		if !b.ready {
			shift = b.frame / 3
		}
		rows[r] = fg(bootRamp[(r+shift)%len(bootRamp)], line.String())
	}
	return rows
}

// bootStepLines renders the sequence, scrolled so the active step stays in
// view when the terminal cannot hold all of it.
func bootStepLines(b *bootState, w, budget int) []string {
	first := 0
	if budget < len(b.steps) {
		// Keep the step being worked on near the bottom, with the next couple
		// still visible ahead of it — a POST list scrolling under the cursor.
		first = clampCursor(b.active()-budget+2, len(b.steps)-budget+1)
	}
	last := mini(first+budget, len(b.steps))
	out := make([]string, 0, last-first)
	for _, s := range b.steps[first:last] {
		out = append(out, bootStepLine(s, b.frame, w))
	}
	return out
}

// bootSpinner is the running glyph: a quartered block turning once a beat. It
// is deliberately not the braille spinner the rest of the terminal world uses —
// this screen is drawn in blocks.
var bootSpinner = []string{"◐", "◓", "◑", "◒"}

// bootStepLine draws one step: glyph, label, dotted rail, figure, and — once it
// has finished — how long it took.
func bootStepLine(s bootStep, frame, w int) string {
	glyph, lc, dc := fg(cFaint, "·"), cFaint, cFaint
	switch s.state {
	case bootRunning:
		glyph, lc, dc = fg(cBlue, bootSpinner[(frame/2)%len(bootSpinner)]), cWhite, cDim
	case bootDone:
		glyph, lc, dc = fg(cGreen, "✓"), cMid, cFg
		if s.warn {
			// The step ran and reported an absence. It is not a failure — the
			// cockpit degrades to fewer signals — but it is the line the human
			// wants to find later when a lens looks emptier than expected.
			glyph, lc, dc = fg(cAmber, "!"), cMid, cAmber
		}
	}

	lead := maxi(bootLabelW-dispWidth(s.label), 1)
	left := glyph + " " + fg(lc, s.label) + " " + fg(bootLeader, strings.Repeat("·", lead)) + " " + fg(dc, s.detail)

	left = truncateAnsi(left, w)
	if s.state != bootDone || s.elapsed <= 0 {
		return left // nothing to hang on the right edge, so no padding out to it
	}
	return flSpread(left, fg(cFaint, bootSecs(s.elapsed.Seconds())), w)
}

// bootSecs formats a measured duration the way a POST screen does.
func bootSecs(sec float64) string {
	return strconv.FormatFloat(sec, 'f', 2, 64) + "s"
}

// bootFoot is the progress meter and the way past the screen.
func bootFoot(b *bootState, inner int) []string {
	done, total := b.progress()

	count := itoa(done) + "/" + itoa(total)
	barW := mini(maxi(inner-dispWidth(count)-4, 8), bootMeterW)

	fill := cBlue
	if b.ready {
		fill = cGreen
	}
	meter := bootCentre(fg(cRule, "[")+fg(fill, bar(pct(done, total), barW))+fg(cRule, "]")+"  "+fg(cDim, count), inner)

	// A blink on READY, the way the screen it is quoting did. While the load
	// runs the hint is steady — it is an offer, not a prompt.
	hint := fg(cFaint, "press any key to skip")
	if b.ready {
		hint = ""
		if b.frame/5%2 == 0 {
			hint = fg(cGreen, "▶ ") + fg(cWhite, "PRESS ANY KEY")
		}
	}
	return []string{"", meter, "", bootCentre(hint, inner)}
}

// bootCentre pads an already-coloured line to sit centred in w columns.
func bootCentre(s string, w int) string {
	if d := dispWidth(s); d < w {
		return strings.Repeat(" ", (w-d)/2) + s
	}
	return s
}
