package cockpit

// decisions.go is the DECISIONS lens (lens 7): a per-repo view of the
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

// pluginForRepo returns the first plugin wired to repo, or the builtin fallback
// (plugins[3]) when none is. Mirrors PLUGINS.filter(...)[0] || PLUGINS[3].
func pluginForRepo(repo string) plugin {
	for _, p := range plugins {
		for _, r := range p.repos {
			if r == repo {
				return p
			}
		}
	}
	return plugins[3]
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
		msg := vjoin(
			fg(cDim, "where decisions live"),
			"",
			fg(cFaint, "no decision records found in any repo."),
			fg(cFaint, "add ADRs under doc/adr/ (adr-tools) and they appear here."),
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
			block = m.decMidBlock(cw, repo, list, ci, decPlugin)
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
		mid := gutter(padBlockTo(m.decMidBlock(maxi(mw-pad, 1), repo, list, ci, decPlugin), h), pad)
		body := gutter(padBlockTo(m.decBodyBlock(maxi(bw-pad, 1), repo, list, ci, decPlugin), h), pad)
		blocks = []string{mid, vrule(h, cRule), body}
	}
	return clampLines(hjoin(blocks...), h)
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
		rl := decisions[r]
		open := 0
		for _, d := range rl {
			if d.status == "proposed" {
				open++
			}
		}
		count, countColor := itoa(len(rl)), cDim
		if open > 0 {
			count, countColor = itoa(open)+"◆", cAmber
		}
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
	lines = append(lines, line("e · "+selRepo, cw, cFaint, ""))
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
	lines = append(lines, fg(cDim, "a accept · s supersede · e change tool · o open in "+decPlugin.name))
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
	case "a":
		if dd != nil {
			m.notice = "accepted " + dd.id + " · recorded in " + repo
		} else {
			m.notice = ""
		}
	case "s":
		if dd != nil {
			m.notice = dd.id + " superseded — write the replacement"
		} else {
			m.notice = ""
		}
	case "e":
		next := (m.pluginCursor + 1) % len(plugins)
		m.pluginCursor = next
		m.notice = "decisions for " + repo + " → " + plugins[next].name
	case "o":
		id := ""
		if dd != nil {
			id = dd.id
		}
		m.notice = "opening " + id + " in " + plugins[clampCursor(m.pluginCursor, len(plugins))].host
	case "right", "l":
		m.pane = "detail"
	case "left", "h", "esc":
		m.pane = "list"
	}
	return m, nil
}
