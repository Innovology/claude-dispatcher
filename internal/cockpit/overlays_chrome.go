package cockpit

import "strings"

// overlays_chrome.go renders the two "chrome" overlays — the HELP sheet and the
// command PALETTE. Both are full-screen modals handed the whole body area; they
// centre their content within a capped max width, mirroring the design's
// max-width:1000px help sheet and 720px palette box.

// helpSections mirrors HELP: five sections, each a heading and its k/d rows.
// "every action is one key" — the k column is the chord, the d column what it does.
var helpSections = []struct {
	section string
	keys    []struct{ k, d string }
}{
	{section: "move", keys: []struct{ k, d string }{
		{"j / k", "up and down the list"},
		{"→ / ←", "into the detail pane and back"},
		{"g / G", "top and bottom"},
		{"tab", "swap panes on a narrow terminal"},
		{"1…7", "triage · products · product · queue · backlog · usage · decisions"},
	}},
	{section: "find", keys: []struct{ k, d string }{
		{"/", "filter the list by feature, repo or product"},
		{"t", "group by what it wants / product / repo / forge"},
		{"w", "show the 13 working instead"},
		{"?", "this help"},
	}},
	{section: "act", keys: []struct{ k, d string }{
		{"y", "ship or accept — asks first"},
		{"n", "reject and reopen"},
		{"r", "reply without attaching"},
		{"enter", "attach the tmux session"},
		{"x", "kill — asks first"},
		{"u", "undo the last action"},
	}},
	{section: "inspect", keys: []struct{ k, d string }{
		{"D", "diff of everything it changed"},
		{"F", "follow the live output"},
		{"M", "change model for the next turn"},
		{"p", "change permission mode"},
	}},
	{section: "batch", keys: []struct{ k, d string }{
		{"space", "mark a dispatcher"},
		{"ctrl+d", "dispatch every picked ticket"},
		{":", "command palette"},
	}},
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
	renderSection := func(sec struct {
		section string
		keys    []struct{ k, d string }
	}) string {
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

	blocks := make([]string, len(helpSections))
	for i, sec := range helpSections {
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
