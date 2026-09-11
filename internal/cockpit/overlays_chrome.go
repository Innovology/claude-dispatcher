package cockpit

import (
	"strings"

	"claude-dispatcher/internal/keymap"
)

// overlays_chrome.go renders the two "chrome" overlays — the HELP sheet and the
// command PALETTE. Both are full-screen modals handed the whole body area; they
// centre their content within a capped max width, mirroring the design's
// max-width:1000px help sheet and 720px palette box.

type helpRow struct{ k, d string }

type helpSection struct {
	section string
	keys    []helpRow
}

// helpLegend is the part of the sheet that is not keys: what the glyphs mean,
// and the few things the cockpit does that no single action names. It stays
// hand-written because none of it is a binding.
var helpLegend = []helpSection{
	{section: "the fleet · lens 1", keys: []helpRow{
		{"●", "blocking — it cannot move until you answer"},
		{"○", "waiting on you, not blocking anything else"},
		{"·", "running · nothing there needs you yet"},
		{"‖", "parked · you shelved it, its row keeps your reason"},
		{"", "sorted by what needs you, not by product — the top row is next"},
	}},
	{section: "move", keys: []helpRow{
		{"1…6", "triage · products · backlog · usage · decisions · velocity"},
		{"esc", "leave the dispatch form"},
		{"O R T S H", "in the product panel: overview · review · team · shipped · history"},
		{"→ / ←", "into an ADR's body and back (decisions)"},
	}},
}

// helpOrder is the order the generated sections are printed in. A section a
// binding names and this does not is still printed, at the end — a new action
// must never be invisible because someone forgot a list.
var helpOrder = []string{
	"work the fleet", "what has finished", "products · lens 2",
	"search", "the other lenses", "anywhere",
}

// helpSections is the key sheet, BUILT from the live keymap rather than written
// beside it.
//
// Every key here is the one that action currently answers to, so a rebind
// appears in `?` the moment it takes effect. The sheet used to be a literal
// table maintained by hand, which was merely duplicated effort while the keys
// were fixed and becomes a lie the first time anybody edits `[keys]` — and the
// help sheet is exactly where someone goes to find out what their keys are.
func (m model) helpSections() []helpSection {
	rows := map[string][]helpRow{}
	var order []string
	for _, b := range m.keys.Bindings() {
		if b.Help == "" {
			continue // a motion key the sheet describes once, beside its pair
		}
		if _, seen := rows[b.Section]; !seen {
			order = append(order, b.Section)
		}
		rows[b.Section] = append(rows[b.Section], helpRow{k: helpKeyLabel(m, b), d: b.Help})
	}

	out := append([]helpSection{}, helpLegend...)
	printed := map[string]bool{}
	add := func(name string) {
		if printed[name] || len(rows[name]) == 0 {
			return
		}
		printed[name] = true
		out = append(out, helpSection{section: name, keys: rows[name]})
	}
	for _, name := range helpOrder {
		add(name)
	}
	for _, name := range order {
		add(name)
	}
	return out
}

// helpKeyLabel pairs the up/down keys of a list onto one row, the way the sheet
// has always read: "j / k" rather than two lines for one idea. The pair is
// found by id — an action ending ".down" is printed with its ".up".
func helpKeyLabel(m model, b keymap.Binding) string {
	label := prettyKey(b.Key)
	if mate, ok := strings.CutSuffix(b.Action, ".down"); ok {
		if up := m.keys.KeyFor(mate + ".up"); up != "" {
			return label + " / " + prettyKey(up)
		}
	}
	if mate, ok := strings.CutSuffix(b.Action, ".first"); ok {
		if last := m.keys.KeyFor(mate + ".last"); last != "" {
			return label + " / " + prettyKey(last)
		}
	}
	return label
}

func prettyKey(k string) string {
	if k == "enter" {
		return "⏎"
	}
	return k
}

// viewHelp renders the "keys" sheet: title + subtitle, then the sections laid
// out in two columns when width allows, one column otherwise.
func (m model) viewHelp(w, h int) string {
	inner := w - 2*pad
	if inner < 10 {
		inner = w
	}
	contentW := inner
	if contentW > 100 {
		contentW = 100
	}

	const colGap = 5
	twoCol := contentW >= 80
	colW := contentW
	if twoCol {
		colW = (contentW - colGap) / 2
	}

	// Each section becomes a block: heading, an underline rule, then k/d rows.
	renderSection := func(sec helpSection) string {
		lines := []string{
			fg(cDim, padTo(sec.section, colW, alignLeft)),
			fg(cRule, strings.Repeat("─", colW)),
		}
		dw := colW - 11 // 9ch key + 2ch gap
		if dw < 1 {
			dw = 1
		}
		for _, kr := range sec.keys {
			k := fg(cWhite, padTo(kr.k, 9, alignLeft))
			d := fg(cMid, truncate(kr.d, dw))
			lines = append(lines, k+"  "+d)
		}
		return vjoin(lines...)
	}

	sections := m.helpSections()
	blocks := make([]string, len(sections))
	for i, sec := range sections {
		blocks[i] = renderSection(sec)
	}

	var body []string
	if twoCol {
		gap := strings.Repeat(" ", colGap)
		for i := 0; i < len(blocks); i += 2 {
			if i+1 < len(blocks) {
				lh := lineCount(blocks[i])
				rh := lineCount(blocks[i+1])
				hh := maxi(lh, rh)
				left := padBlockTo(blocks[i], hh)
				right := padBlockTo(blocks[i+1], hh)
				body = append(body, hjoin(left, gap, right))
			} else {
				body = append(body, blocks[i])
			}
			body = append(body, "")
		}
	} else {
		for _, b := range blocks {
			body = append(body, b, "")
		}
	}

	out := []string{
		fg(cWhite, "keys"),
		fg(cDim, "every action is one key · esc closes this"),
		"",
	}
	out = append(out, body...)

	// Centre the capped content within the body area.
	left := pad + (inner-contentW)/2
	if left < pad {
		left = pad
	}
	return clampLines(gutter(vjoin(out...), left), h)
}

// viewPalette renders the ":" command box: the prompt line with the current
// query and a cursor, then the filtered command list with a marker on the
// selected row.
func (m model) viewPalette(w, h int) string {
	inner := w - 2*pad
	if inner < 10 {
		inner = w
	}
	boxW := 72
	if boxW > inner {
		boxW = inner
	}

	// Prompt line: ":" + query + cursor bar, with "esc" hint pinned right.
	promptLeft := fg(cAmber, ":") + " " + fg(cWhite, m.paletteText) + fg(cWhite, "▏")
	escHint := fg(cFaint, "esc")
	gap := boxW - dispWidth(promptLeft) - dispWidth(escHint)
	if gap < 1 {
		gap = 1
	}
	prompt := promptLeft + strings.Repeat(" ", gap) + escHint

	lines := []string{
		prompt,
		fg(cRule, strings.Repeat("─", boxW)),
	}

	cmds := m.filteredCommands()
	sel := clampCursor(m.paletteCursor, len(cmds))
	for i, cmd := range cmds {
		on := i == sel
		bg := cTransparent
		nameColor := cFg
		marker := " "
		if on {
			bg = cSel
			nameColor = cWhite
			marker = "▸"
		}
		segs := []seg{
			c(marker, 2, cMid),
			c(cmd.name, 26, nameColor),
			flexc(cmd.hint, cDim),
		}
		lines = append(lines, row(boxW, bg, segs...))
	}

	// Centre the box within the body area.
	left := pad + (inner-boxW)/2
	if left < pad {
		left = pad
	}
	return clampLines(gutter(vjoin(lines...), left), h)
}

// lineCount returns the number of lines in s (used to balance side-by-side
// section blocks before hjoin).
func lineCount(s string) int { return strings.Count(s, "\n") + 1 }
