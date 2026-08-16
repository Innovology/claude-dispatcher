package cockpit

// decisions.go is the DECISIONS lens (lens 5): a per-repo view of the
// architecture decision records — ADRs and decision trees — that each repo's
// plugin renders. The left column is "where decisions live" (the repos plus the
// installed plugins), the middle is the selected repo's records, and the right
// is the selected record in full. A faithful port of the design's DECISIONS.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// decision is one architecture decision record. Fields track the mock's
// DECISIONS entries: an id, a one-line title, a lifecycle status, a relative
// timestamp, a provenance line, and the three ADR prose sections.
type decision struct {
	id, title, status, at, by, context, decision, consequences string
}

// decisionStatusMeta mirrors DECISION_STATUS: the colour, glyph and one-line
// note for each record lifecycle state.
var decisionStatusMeta = map[string]struct{ color, glyph, note string }{
	"proposed":   {cAmber, "◆", "claude decided this and nobody has agreed yet"},
	"accepted":   {cGreen, "✓", "you accepted it"},
	"superseded": {cDim, "·", "replaced by a later record"},
}

// plugin is a decision-record tool wired into one or more repos. Mirrors PLUGINS.
type plugin struct {
	id, name, host, kind, note string
	repos                      []string
}

// pluginForRepo returns the first plugin wired to repo, else the builtin
// fallback ("cockpit records"), else the zero plugin.
//
// The design's fallback is PLUGINS[3] — its mock ships exactly four. The
// collector ships one or two, so the literal port indexed past the end of the
// slice; it only ever survived because collectDecisions puts every repo without
// an ADR directory in the builtin's repo list, so the search never actually
// missed. Look the fallback up by id instead of by position.
func pluginForRepo(repo string) plugin {
	for _, p := range plugins {
		for _, r := range p.repos {
			if r == repo {
				return p
			}
		}
	}
	return pluginByID(plugins, "builtin")
}

// pluginByID returns the plugin with the given id, or the zero plugin.
func pluginByID(list []plugin, id string) plugin {
	for _, p := range list {
		if p.id == id {
			return p
		}
	}
	return plugin{}
}

// pluginHasRepo reports whether p is wired to repo.
func pluginHasRepo(p plugin, repo string) bool {
	for _, r := range p.repos {
		if r == repo {
			return true
		}
	}
	return false
}

// decWrap word-wraps s into lines no wider than w columns.
func decWrap(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := ""
	for _, wd := range words {
		if cur == "" {
			cur = wd
			continue
		}
		if dispWidth(cur)+1+dispWidth(wd) <= w {
			cur += " " + wd
		} else {
			lines = append(lines, cur)
			cur = wd
		}
	}
	return append(lines, cur)
}

// viewDecisions renders the three-column decisions lens. Which columns show is
// driven by m.fit().dec (3 → all three; 2 → mid+body; 1 → a single column
// chosen by m.pane), mirroring decLeft/decMid/decBody in the mock.
func (m model) viewDecisions(w, h int) string {
	dec := m.fit().dec

	repos := decisionRepoOrder
	if len(repos) == 0 {
		// Naming both places it looked matters: the lens spent its first life
		// reading only doc/adr, said "no decision records found" over repos
		// whose decisions were written down in plain sight, and gave the human
		// no way to tell an empty fleet from a scanner looking in one place.
		msg := vjoin(
			fg(cDim, "where decisions live"),
			"",
			fg(cFaint, "no decision records found in any repo."),
			"",
			fg(cFaint, "the cockpit reads two things:"),
			fg(cFaint, "  · ADRs under doc/adr/, docs/adr/ or docs/decisions/"),
			fg(cFaint, "  · a decisions heading in CLAUDE.md, DECISIONS.md,"),
			fg(cFaint, "    ARCHITECTURE.md or README.md — every bullet under"),
			fg(cFaint, "    it is a record"),
			"",
			fg(cFaint, "write either and it appears here on the next load."),
		)
		return clampLines(gutter(msg, pad), h)
	}
	ri := clampCursor(m.decRepo, len(repos))
	repo := repos[ri]
	list := decisions[repo]
	ci := clampCursor(m.decCursor, len(list))
	decPlugin := pluginForRepo(repo)

	// Single column: mid when not in detail, body when in detail.
	if dec <= 1 {
		cw := maxi(w-pad, 1)
		var block string
		if m.pane == "detail" {
			block = m.decBodyBlock(cw, repo, list, ci, decPlugin)
		} else {
			// The body pane and the left column are both gone here, so the
			// inline block carries the record's prose as well as the repo tail.
			block = vjoin(append([]string{m.decMidBlock(cw, repo, list, ci, decPlugin)},
				decNarrowBlock(cw, list, ci, decPlugin, true)...)...)
		}
		return clampLines(gutter(padBlockTo(block, h), pad), h)
	}

	var blocks []string
	if dec >= 3 {
		lw := maxi(w*26/100, 1)
		mw := maxi(w*34/100, 1)
		bw := maxi(w-2-lw-mw, 1)
		left := gutter(padBlockTo(m.decLeftBlock(maxi(lw-pad, 1), repo, list, decPlugin), h), pad)
		mid := gutter(padBlockTo(m.decMidBlock(maxi(mw-pad, 1), repo, list, ci, decPlugin), h), pad)
		body := gutter(padBlockTo(m.decBodyBlock(maxi(bw-pad, 1), repo, list, ci, decPlugin), h), pad)
		blocks = []string{left, vrule(h, cRule), mid, vrule(h, cRule), body}
	} else { // dec == 2: mid + body, left hidden.
		mw := maxi(w*42/100, 1)
		bw := maxi(w-1-mw, 1)
		mwi := maxi(mw-pad, 1)
		// Only the left column is hidden at this tier, and the body pane is
		// still showing the prose — so the inline block re-homes just what the
		// left column carried, without repeating the record side by side.
		midBlock := vjoin(append([]string{m.decMidBlock(mwi, repo, list, ci, decPlugin)},
			decNarrowBlock(mwi, list, ci, decPlugin, false)...)...)
		mid := gutter(padBlockTo(midBlock, h), pad)
		body := gutter(padBlockTo(m.decBodyBlock(maxi(bw-pad, 1), repo, list, ci, decPlugin), h), pad)
		blocks = []string{mid, vrule(h, cRule), body}
	}
	return clampLines(hjoin(blocks...), h)
}

// decRepoCount is a repo's record tally for the "where decisions live" rows:
// the number still proposed (amber, flagged ◆) when any are, else the total.
// Shared by the left column and the collapsed layouts' inline tail so the two
// can never disagree about a repo's count.
func decRepoCount(repo string) (count, color string) {
	rl := decisions[repo]
	open := 0
	for _, d := range rl {
		if d.status == "proposed" {
			open++
		}
	}
	if open > 0 {
		return itoa(open) + "◆", cAmber
	}
	return itoa(len(rl)), cDim
}

// decNarrowBlock is the design's decNarrow: what the collapsed layouts lose,
// re-homed under the record list. At two columns only the left column is
// hidden, so it carries just the repo tail; at one column the body pane is gone
// too and the selected record's prose comes with it. At three columns nothing
// is missing and this is not rendered at all.
func decNarrowBlock(cw int, list []decision, ci int, decPlugin plugin, prose bool) []string {
	out := []string{"", fg(cRule, strings.Repeat("─", cw))}

	if prose {
		x := decision{id: "—"}
		if len(list) > 0 {
			x = list[ci]
		}
		section := func(label, body, hex string) {
			out = append(out, "")
			out = append(out, line(label, cw, cDim, ""))
			for _, l := range decWrap(body, cw) {
				out = append(out, fg(hex, l))
			}
		}
		section(x.id+" · context", x.context, cMid)
		section("decision", x.decision, cFg)
		section("consequences", x.consequences, cMid)
	}

	out = append(out, "")
	out = append(out, line(decPlugin.name+" · "+decPlugin.host, cw, cFaint, ""))
	out = append(out, "")
	out = append(out, line("where decisions live", cw, cDim, ""))
	for _, r := range decisionRepoOrder {
		count, countColor := decRepoCount(r)
		pl := pluginForRepo(r)
		out = append(out, row(cw, "",
			flexc(r, cMid),
			c(" "+pl.name, dispWidth(pl.name)+1, cFaint),
			cr(count, 5, countColor),
		))
	}
	return out
}

// decLeftBlock builds the "where decisions live" column: the repos (with an
// open-record count) and the installed plugins.
func (m model) decLeftBlock(cw int, selRepo string, _ []decision, _ plugin) string {
	var lines []string
	lines = append(lines, line("where decisions live", cw, cDim, ""))
	for n, r := range decisionRepoOrder {
		on := n == clampCursor(m.decRepo, len(decisionRepoOrder))
		bg := ""
		nameColor := cFg
		marker := ""
		if on {
			bg, nameColor, marker = cSel, cWhite, "▸"
		}
		count, countColor := decRepoCount(r)
		lines = append(lines, row(cw, bg, c(marker, 2, cMid), flexc(r, nameColor), cr(count, 5, countColor)))
		pl := pluginForRepo(r)
		plColor := cMid
		if pl.id == "builtin" {
			plColor = cDim
		}
		lines = append(lines, line("  "+pl.name, cw, plColor, ""))
	}

	lines = append(lines, "")
	lines = append(lines, line("plugins", cw, cDim, ""))
	for n, p := range plugins {
		has := pluginHasRepo(p, selRepo)
		glyph, stateColor := "○", cFaint
		if has {
			glyph, stateColor = "✓", cGreen
		} else if len(p.repos) > 0 {
			glyph = "·"
		}
		nameColor := cFg
		if n == clampCursor(m.pluginCursor, len(plugins)) {
			nameColor = cWhite
		}
		count := "off"
		if len(p.repos) > 0 {
			count = itoa(len(p.repos)) + " repos"
		}
		lines = append(lines, row(cw, "", c(glyph, 2, stateColor), flexc(p.name, nameColor), c(" "+count, dispWidth(count)+1, cFaint)))
		lines = append(lines, line("  "+p.host, cw, cFaint, ""))
	}
	lines = append(lines, "")
	lines = append(lines, line("e · cycle sources", cw, cFaint, ""))
	return vjoin(lines...)
}

// decMidBlock builds the selected repo's record list, with the plugin that
// renders it named on the right of the header.
func (m model) decMidBlock(cw int, repo string, list []decision, ci int, decPlugin plugin) string {
	source := decPlugin.name + " · " + decPlugin.kind
	if decPlugin.id == "builtin" {
		source = "no tool of its own · cockpit records"
	}
	var lines []string
	lines = append(lines, row(cw, "",
		c(repo, dispWidth(repo)+2, cDim),
		seg{text: source, flex: true, align: alignRight, hex: cFaint},
	))
	for n, d := range list {
		on := n == ci
		bg, titleColor, marker := "", cFg, ""
		if on {
			bg, titleColor, marker = cSel, cWhite, "▸"
		}
		st := decisionStatusMeta[d.status]
		lines = append(lines, row(cw, bg,
			c(marker, 2, cMid), c(st.glyph, 2, st.color), c(d.id, 9, cMid), flexc(d.title, titleColor),
		))
		lines = append(lines, row(cw, "",
			c("", 4, ""), c(d.status, dispWidth(d.status)+1, st.color), flexc(d.by, cFaint),
		))
	}
	if len(list) == 0 {
		lines = append(lines, "")
		lines = append(lines, line("nothing recorded here yet", cw, cFaint, ""))
	}
	return vjoin(lines...)
}

// decBodyBlock builds the selected record in full: header, title, provenance,
// status note, the three ADR prose sections, and the rendering plugin.
func (m model) decBodyBlock(cw int, _ string, list []decision, ci int, decPlugin plugin) string {
	x := decision{id: "—", title: "no records", status: "proposed"}
	if len(list) > 0 {
		x = list[ci]
	}
	st := decisionStatusMeta[x.status]
	wrapW := mini(cw, 74)

	var lines []string
	lines = append(lines, row(cw, "",
		c(x.id, dispWidth(x.id)+2, cDim),
		seg{text: st.glyph + " " + x.status + " · " + x.at, flex: true, align: alignRight, hex: st.color},
	))
	for _, l := range decWrap(x.title, cw) {
		lines = append(lines, fg(cWhite, l))
	}
	lines = append(lines, fg(cFaint, x.by))
	lines = append(lines, fg(st.color, st.note))

	section := func(label, body, hex string) {
		lines = append(lines, "")
		lines = append(lines, fg(cDim, label))
		for _, l := range decWrap(body, wrapW) {
			lines = append(lines, fg(hex, l))
		}
	}
	section("context", x.context, cMid)
	section("decision", x.decision, cFg)
	section("consequences", x.consequences, cMid)

	lines = append(lines, "")
	lines = append(lines, fg(cDim, "rendered by"))
	lines = append(lines, fg(cFg, decPlugin.name)+fg(cFaint, " · "+decPlugin.host))
	for _, l := range decWrap(decPlugin.note, wrapW) {
		lines = append(lines, fg(cFaint, l))
	}

	lines = append(lines, "")
	lines = append(lines, fg(cDim, "a status · s supersede · e sources · o where it lives"))
	return vjoin(lines...)
}

// updateDecisions handles the lens's keys, mirroring handleKey's decisions
// branch: j/k move within a repo's records, J/K change repo, a/s/o/e set a
// notice, and →/← toggle the detail pane.
func (m model) updateDecisions(k string) (model, tea.Cmd) {
	repos := decisionRepoOrder
	if len(repos) == 0 {
		return m, nil // no repo has an ADR directory — there is nothing to move through
	}
	repo := repos[clampCursor(m.decRepo, len(repos))]
	list := decisions[repo]
	var dd *decision
	if len(list) > 0 {
		d := list[clampCursor(m.decCursor, len(list))]
		dd = &d
	}

	switch k {
	case "j", "down":
		n := mini(m.decCursor+1, len(list)-1)
		if n < 0 {
			n = 0
		}
		m.decCursor = n
	case "k", "up":
		m.decCursor = maxi(m.decCursor-1, 0)
	case "J":
		m.decRepo = mini(m.decRepo+1, len(repos)-1)
		m.decCursor = 0
	case "K":
		m.decRepo = maxi(m.decRepo-1, 0)
		m.decCursor = 0
	// a/s/o report what the cockpit can actually do, which is read. The lens
	// scans each repo's ADR folder; nothing here writes a record, opens an
	// editor or calls a plugin, and the notices used to say otherwise —
	// "accepted N · recorded in <repo>" for a key that touched no file.
	case "a":
		if dd != nil {
			m.notice = dd.id + " is " + dd.status + " in " + repo + " · the cockpit reads these records, it does not write them"
		} else {
			m.notice = ""
		}
	case "s":
		if dd != nil {
			m.notice = "supersede " + dd.id + " by writing the replacement in " + repo + " · the cockpit picks it up on the next scan"
		} else {
			m.notice = ""
		}
	case "e":
		if len(plugins) == 0 {
			m.notice = ""
			break
		}
		next := (m.pluginCursor + 1) % len(plugins)
		m.pluginCursor = next
		// The cursor picks which tool the pane describes. It does not rewire the
		// repo — which tool renders a repo is decided by what is on disk.
		m.notice = "showing " + plugins[next].name
	case "o":
		if dd != nil {
			m.notice = dd.id + " lives in " + repo + " · no opener wired yet"
		} else {
			m.notice = ""
		}
	case "right", "l":
		m.pane = "detail"
	case "left", "h", "esc":
		m.pane = "list"
	}
	return m, nil
}
