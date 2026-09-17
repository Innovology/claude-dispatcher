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
		{"✓", "finished · it ended and is waiting for you to read it · x clears"},
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

// helpNotes are lines that belong to a generated section but are not bindings:
// a key whose meaning depends on the row it is pressed on, and the sentence
// that says what happens after. They are appended to their section, so nothing
// the hand-written sheet used to say was lost when it started being built.
var helpNotes = map[string][]helpRow{
	"work the fleet": {
		{"x", "on a ✓ row: dismiss it · nothing to kill, it goes to history"},
	},
	"what has finished": {
		{"", "a dispatcher that ends keeps its row until you dismiss it"},
		{"⏎","resume it · its own transcript, in its own worktree"},
		{"", "a resumed dispatcher rejoins the fleet and you land in its session"},
	},
	"products · lens 2": {
		{"esc", "close the panel · again closes the assignment editor"},
	},
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
		out = append(out, helpSection{section: name, keys: append(rows[name], helpNotes[name]...)})
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

const (
	// helpKeyCol is the width of the sheet's key column. The widest chord it
	// has to hold is "u / ctrl+u".
	helpKeyCol = 10
	// helpMaxWidth caps the sheet so its prose stays readable — a sentence run
	// across a 200-column terminal is one the eye loses its place in.
	//
	// It was 100, which is narrower than the longest line the sheet has to
	// print: split into two columns that left 36 characters for a description,
	// and every description longer than that was cut. On a wide terminal the
	// result was a sheet of half-sentences with most of the screen empty. 160
	// is two columns of about 66, which is the longest description there is.
	helpMaxWidth = 160
)

// viewHelp renders the "keys" sheet: title + subtitle, then the sections laid
// out in two columns when width allows, one column otherwise.
func (m model) viewHelp(w, h int) string {
	inner := w - 2*pad
	if inner < 10 {
		inner = w
	}
	contentW := mini(inner, helpMaxWidth)

	const colGap = 5
	twoCol := contentW >= 80
	colW := contentW
	if twoCol {
		colW = (contentW - colGap) / 2
	}

	// Each section becomes a block: heading, an underline rule, then k/d rows.
	//
	// A description WRAPS onto a second line rather than being cut. It is the
	// only explanation a key has, and the half a truncation keeps is the half
	// that says least — "upgrade to the published build — be…" is the sentence
	// with its point removed. The continuation lines sit under the description
	// column, so the key column still reads as a column.
	renderSection := func(sec helpSection) string {
		lines := []string{
			fg(cDim, padTo(sec.section, colW, alignLeft)),
			fg(cRule, strings.Repeat("─", colW)),
		}
		dw := maxi(colW-helpKeyCol-2, 1) // key column + a 2ch gap
		for _, kr := range sec.keys {
			for i, part := range productsWrap(kr.d, dw) {
				key := ""
				if i == 0 {
					key = kr.k
				}
				lines = append(lines, fg(cWhite, padTo(key, helpKeyCol, alignLeft))+"  "+fg(cMid, part))
			}
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
