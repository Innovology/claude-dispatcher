package cockpit

// floor.go is the triage lens (lens 1) — the biggest, default view. It renders
// the hero ASK panel, the wants-you list grouped four ways, the dispatcher
// detail pane (or a group-header roll-up), the bottom activity strip, and the
// diff overlay. It is a faithful port of the v2 design's floor.

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ---- grouping model ---------------------------------------------------------

// floorGroup is one band/product/repo/forge group of the list. A dark group has
// nothing in flight and sorts to the bottom.
type floorGroup struct {
	glyph, label, rule, color string
	dark                      bool
	raw                       []dispatch
}

// floorEntry is one row of the flattened list — either a selectable group
// header or a dispatcher.
type floorEntry struct {
	header bool
	g      floorGroup
	x      dispatch
}

// floorDarkLast mirrors DARK: how long since a quiet product was dispatched to.
var floorDarkLast = map[string]string{
	"unassigned": "never dispatched · 46 repos unmapped",
	"kalish":     "last dispatch 3d ago",
	"northwind":  "last dispatch 2d ago",
}

// matches is the list filter: feature, repo, product and signal, case-folded.
func (m model) matches(x dispatch) bool {
	q := strings.TrimSpace(strings.ToLower(m.filter))
	if q == "" {
		return true
	}
	hay := strings.ToLower(x.feature + " " + x.repo + " " + x.product + " " + x.signal)
	return strings.Contains(hay, q)
}

// liveDispatches is the not-yet-shipped, filter-matching floor (the one being
// shipped stays until its animation ends).
func (m model) liveDispatches() []dispatch {
	fx := m.shipFx
	var out []dispatch
	for _, x := range dispatches {
		shipping := fx != nil && fx.feature == x.feature
		if (!m.shipped[x.feature] || shipping) && m.matches(x) {
			out = append(out, x)
		}
	}
	return out
}

// canShip is true for the states where y/m offers a ship confirm.
func (m model) canShip(x dispatch) bool {
	return x.state == "blocked" || x.state == "claimed" || x.state == "review"
}

// stackEntry finds x within its repo's stack (needs at least two entries).
func stackEntryOf(x dispatch) (idx, n int, it stackItem, ok bool) {
	st := stacks[x.repo]
	if len(st) < 2 {
		return 0, 0, stackItem{}, false
	}
	for i, p := range st {
		if p.feature == x.feature {
			return i, len(st), p, true
		}
	}
	return 0, 0, stackItem{}, false
}

func (m model) stackPos(x dispatch) string {
	i, n, it, ok := stackEntryOf(x)
	if !ok {
		return ""
	}
	if it.state == "behind" {
		return "rebase"
	}
	return itoa(i+1) + "/" + itoa(n)
}

func (m model) stackColor(x dispatch) string {
	_, _, it, ok := stackEntryOf(x)
	if !ok {
		return cFaint
	}
	if it.state == "behind" {
		return cAmber
	}
	return cFaint
}

// buildGroups mirrors buildGroups(): the list, grouped by the active mode, with
// dark (nothing-out) groups pushed to the bottom.
func (m model) buildGroups() []floorGroup {
	if m.workingOpen {
		var raw []dispatch
		for _, w := range working {
			prod := w.product
			if prod == "" {
				prod = "—"
			}
			raw = append(raw, dispatch{
				feature: w.feature, repo: w.repo, product: prod, forge: "gh",
				state: "working", age: w.age, branch: branchOf(w.feature),
				why:      "Mid-turn and not asking for anything. It has been going " + w.age + ".",
				prompt:   "—",
				activity: []activity{{"…", "turn in progress", "", cDim}},
				agents:   []agent{{"", "main", "sonnet", "working", "now", "running " + w.age}},
				prs:      []prRef{}, runs: []runRef{},
				chain: []chainStep{
					{"commits", "in progress", "now"},
					{"no pr", "not yet", "idle"},
					{"checks", "—", "idle"},
					{"merge", "—", "idle"},
					{"deploy", "—", "idle"},
				},
			})
		}
		return []floorGroup{{glyph: "●", label: "working · " + itoa(len(working)), rule: "leave them alone", color: cGreen, raw: raw}}
	}

	switch m.groupByMode() {
	case "product":
		live := m.liveDispatches()
		var all []floorGroup
		for _, p := range products {
			var raw []dispatch
			for _, x := range live {
				if x.product == p.name {
					raw = append(raw, x)
				}
			}
			if len(raw) > 0 {
				all = append(all, floorGroup{label: p.name, color: cMid, raw: raw, rule: itoa(len(raw)) + " of " + itoa(p.inflight) + " want you"})
				continue
			}
			last := "idle"
			if v, ok := floorDarkLast[p.name]; ok {
				last = v
			}
			tickets := flCountTickets(func(t ticket) bool { return t.product == p.name && t.taken == "" })
			rule := "nothing out · " + last
			if tickets > 0 {
				rule += " · " + itoa(tickets) + " open tickets"
			}
			all = append(all, floorGroup{glyph: "○", label: p.name, color: cDim, dark: true, rule: rule})
		}
		return flSortDark(all)

	case "repo":
		live := m.liveDispatches()
		var names []string
		seen := map[string]bool{}
		add := func(n string) {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		for _, p := range productOrder {
			for _, r := range reposByProduct[p] {
				add(r.name)
			}
		}
		for _, s := range staleRepos {
			add(s.repo)
		}
		for _, x := range live {
			add(x.repo)
		}
		flSortStrings(names)
		var groups []floorGroup
		for _, n := range names {
			var raw []dispatch
			for _, x := range live {
				if x.repo == n {
					raw = append(raw, x)
				}
			}
			st := stacks[n]
			behind := 0
			for _, p := range st {
				if p.state == "behind" {
					behind++
				}
			}
			var stale *staleRepo
			for i := range staleRepos {
				if staleRepos[i].repo == n {
					stale = &staleRepos[i]
					break
				}
			}
			tickets := flCountTickets(func(t ticket) bool { return t.repo == n && t.taken == "" })
			if len(raw) == 0 {
				rule := "nothing out · "
				if stale != nil {
					rule += itoa(stale.days) + "d untouched · " + stale.note
				} else {
					rule += "idle"
				}
				if tickets > 0 {
					rule += " · " + itoa(tickets) + " tickets"
				}
				groups = append(groups, floorGroup{glyph: "○", label: n, color: cDim, dark: true, rule: rule})
				continue
			}
			var rule string
			if len(st) > 1 {
				rule = itoa(len(st)) + " stacked"
				if behind > 0 {
					rule += " · " + itoa(behind) + " needs rebase"
				} else {
					rule += " · merges bottom-up"
				}
			} else if raw[0].forge == "ado" {
				rule = "azure devops"
			} else {
				rule = "github"
			}
			groups = append(groups, floorGroup{label: n, color: cMid, raw: raw, rule: rule})
		}
		return flSortDark(groups)

	case "forge":
		live := m.liveDispatches()
		var gh, ado []dispatch
		for _, x := range live {
			if x.forge == "ado" {
				ado = append(ado, x)
			} else {
				gh = append(gh, x)
			}
		}
		var groups []floorGroup
		if len(gh) > 0 {
			groups = append(groups, floorGroup{label: "github", rule: "gh cli · actions", color: cMid, raw: gh})
		}
		if len(ado) > 0 {
			groups = append(groups, floorGroup{label: "azure devops", rule: "az cli · pipelines", color: cMid, raw: ado})
		}
		return groups

	default: // band
		live := m.liveDispatches()
		var groups []floorGroup
		for _, b := range bandOrder {
			var raw []dispatch
			for _, x := range live {
				if x.state == b {
					raw = append(raw, x)
				}
			}
			if len(raw) == 0 {
				continue
			}
			sm := stateMetaBy[b]
			groups = append(groups, floorGroup{glyph: sm.glyph, label: sm.label + " · " + itoa(len(raw)), rule: bandRule[b], color: sm.color, raw: raw})
		}
		return groups
	}
}

func flSortDark(gs []floorGroup) []floorGroup {
	var lit, dark []floorGroup
	for _, g := range gs {
		if g.dark {
			dark = append(dark, g)
		} else {
			lit = append(lit, g)
		}
	}
	return append(lit, dark...)
}

func flSortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func flCountTickets(pred func(ticket) bool) int {
	n := 0
	for _, t := range backlogTickets {
		if pred(t) {
			n++
		}
	}
	return n
}

// headersSelectable is true when group headers are their own cursor stops.
func (m model) headersSelectable() bool {
	if m.workingOpen {
		return false
	}
	switch m.groupByMode() {
	case "product", "repo", "forge":
		return true
	}
	return false
}

// seq flattens the groups into cursor entries (headers included when selectable).
func (m model) seq() []floorEntry {
	var out []floorEntry
	headers := m.headersSelectable()
	for _, g := range m.buildGroups() {
		if headers {
			out = append(out, floorEntry{header: true, g: g})
		}
		for _, x := range g.raw {
			out = append(out, floorEntry{header: false, g: g, x: x})
		}
	}
	return out
}

func (m model) entry() floorEntry {
	q := m.seq()
	if len(q) == 0 {
		return floorEntry{x: firstDispatch()}
	}
	return q[clampCursor(m.cursor, len(q))]
}

// firstDispatch is the fallback selection: the first record, or a zero dispatch
// when the floor is empty (real portfolio with nothing in flight).
func firstDispatch() dispatch {
	if len(dispatches) > 0 {
		return dispatches[0]
	}
	return dispatch{feature: "—", repo: "—", state: "working"}
}

func (m model) floorFlat() []dispatch {
	var out []dispatch
	for _, g := range m.buildGroups() {
		out = append(out, g.raw...)
	}
	return out
}

func (m model) selected() dispatch {
	e := m.entry()
	if e.header {
		if len(e.g.raw) > 0 {
			return e.g.raw[0]
		}
		return firstDispatch()
	}
	if e.x.feature == "" {
		return firstDispatch()
	}
	return e.x
}

func (m model) floorSelectedFeature() string { return m.selected().feature }
func (m model) floorEntryIsHeader() bool     { return m.entry().header }

// headerTickets is the backlog slice shown / dispatched under a group header.
func (m model) headerTickets(g floorGroup) []ticket {
	mode := m.groupByMode()
	var out []ticket
	for _, t := range backlogTickets {
		match := false
		switch mode {
		case "product":
			match = t.product == g.label
		case "repo":
			match = t.repo == g.label
		default: // forge
			match = (g.label == "github" && t.src == "gh") || (g.label == "azure devops" && t.src == "ado")
		}
		if match {
			out = append(out, t)
			if len(out) == 7 {
				break
			}
		}
	}
	return out
}

// ---- layout helpers ---------------------------------------------------------

// flG left-pads a rendered line by the page gutter.
func flG(s string) string { return strings.Repeat(" ", pad) + s }

// flPad pads an already-coloured string to w columns (truncating if longer),
// painting the trailing space on bg when one is given.
func flPad(s string, w int, bg string) string {
	d := dispWidth(s)
	if d > w {
		return truncateAnsi(s, w)
	}
	if d == w {
		return s
	}
	fill := strings.Repeat(" ", w-d)
	if bg != "" {
		fill = paint("", bg, fill)
	}
	return s + fill
}

// flSpread places left and right at the two edges of a w-wide line.
func flSpread(left, right string, w int) string {
	lw, rw := dispWidth(left), dispWidth(right)
	if w <= 0 {
		return ""
	}
	if lw+1+rw > w {
		return truncateAnsi(left, w)
	}
	return left + strings.Repeat(" ", w-lw-rw) + right
}

// flWrap word-wraps s to width w, at most maxLines (0 = unlimited).
func flWrap(s string, w, maxLines int) []string {
	if w <= 0 {
		return []string{s}
	}
	var lines []string
	cur := ""
	for _, wd := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = wd
		case dispWidth(cur)+1+dispWidth(wd) <= w:
			cur += " " + wd
		default:
			lines = append(lines, cur)
			cur = wd
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = truncate(lines[maxLines-1]+" …", w)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

// flTwoCol joins two colour-rendered blocks side by side with a gap.
func flTwoCol(left, right []string, leftW, rightW, gap int) []string {
	h := maxi(len(left), len(right))
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, flPad(l, leftW, "")+strings.Repeat(" ", gap)+flPad(r, rightW, ""))
	}
	return out
}

func flIndex(sl []string, v string) int {
	for i, s := range sl {
		if s == v {
			return i
		}
	}
	return 0
}

// ---- view -------------------------------------------------------------------

func (m model) viewFloor(w, h int) string {
	fitv := m.fit()
	hero := m.floorHero(w)
	var strip []string
	if fitv.showStrip {
		strip = m.floorStrip(w)
	}
	paneH := h - len(hero) - len(strip)
	if paneH < 1 {
		paneH = 1
	}

	header := m.entry().header
	var body string
	switch {
	case fitv.showDetail:
		listW := w * fitv.listPct / 100
		detailW := w - listW - 1
		left := padBlockTo(m.floorList(listW, paneH), paneH)
		var right string
		if header {
			right = m.floorGroupDetail(detailW, paneH)
		} else {
			right = m.floorDetail(detailW, paneH)
		}
		right = padBlockTo(right, paneH)
		body = hjoin(left, vrule(paneH, cRule), right)
	case m.narrowPane == "detail":
		if header {
			body = m.floorGroupDetail(w, paneH)
		} else {
			body = m.floorDetail(w, paneH)
		}
	default:
		body = m.floorList(w, paneH)
	}

	parts := make([]string, 0, len(hero)+1+len(strip))
	parts = append(parts, hero...)
	parts = append(parts, body)
	parts = append(parts, strip...)
	return clampLines(strings.Join(parts, "\n"), h)
}

// floorHero is the ASK panel for the selected dispatcher.
func (m model) floorHero(w int) []string {
	sel := m.selected()
	meta := stateMetaBy[sel.state]
	flat := m.floorFlat()
	inner := w - 2*pad

	var kicker, headline, evidence string
	var acts []action
	if sel.ask != nil {
		kicker, headline, evidence, acts = sel.ask.kicker, sel.ask.headline, sel.ask.evidence, sel.ask.actions
	} else {
		kicker = strings.ToUpper(meta.label)
		headline = sel.repo + " · " + sel.why
		acts = []action{{"r", "reply"}, {"enter", "attach"}, {"m", "merge"}}
		evidence = itoa(sel.commits) + " commits · +" + itoa(sel.plus) + " −" + itoa(sel.minus) + " · " + sel.age
	}
	accent := cBlue
	switch sel.state {
	case "blocked":
		accent = cRed
	case "needs":
		accent = cAmber
	}
	where := sel.feature + " · " + sel.repo
	then := "then " + itoa(maxi(0, len(flat)-1)) + " more want you · " + itoa(len(working)) + " working, leave them"

	line1 := flSpread(fg(accent, kicker)+"  "+fg(cDim, where), fg(cFaint, then), inner)
	line2 := fg(cWhite, truncate(headline, inner))
	var ab strings.Builder
	for i, a := range acts {
		if i > 0 {
			ab.WriteString("   ")
		}
		ab.WriteString(fg(cWhite, a.key) + " " + fg(cMid, a.label))
	}
	line3 := flSpread(ab.String(), fg(cFaint, evidence), inner)

	return []string{flG(line1), flG(line2), flG(line3), ""}
}

// floorList is the wants-you list pane.
func (m model) floorList(width, height int) string {
	flat := m.floorFlat()
	inner := width - 2*pad
	repoW := mini(20, maxi(0, width-41))

	// title + filter + column header
	listTitle := "wants you · " + itoa(len(flat))
	if m.workingOpen {
		listTitle = "working · " + itoa(len(working)) + " · w back to wants you"
	}
	titleLine := flG(flSpread(fg(cDim, listTitle), fg(cFaint, groupLabel[m.groupByMode()]+" · t"), inner))

	filterColor := cFaint
	if m.filter != "" {
		filterColor = cAmber
	}
	var fbody string
	if m.filter != "" {
		fbody = fg(cWhite, m.filter)
	} else {
		fbody = fg(cFaint, "filter by feature, repo or product")
	}
	filterCount := ""
	if m.filter != "" {
		filterCount = itoa(len(flat)) + " of " + itoa(len(dispatches))
	}
	filterLine := flG(flSpread(fg(filterColor, "/")+" "+fbody, fg(cFaint, filterCount), inner))

	colHeader := row(width, "",
		c("", pad, ""), c("", 4, cFaint),
		flexc("FEATURE", cFaint), c("REPO", repoW, cFaint),
		cr("SIGNAL", 13, cFaint), cr("STACK", 8, cFaint), cr("AGE", 6, cFaint),
		c("", pad, ""))

	// rows, tracking the cursor across headers + items
	groups := m.buildGroups()
	headersOn := m.headersSelectable()
	cur := clampCursor(m.cursor, len(m.seq()))
	fx := m.shipFx
	i := 0
	var rows []string
	for _, g := range groups {
		if headersOn {
			rows = append(rows, floorHeaderRow(g, i == cur, width, repoW))
			i++
		}
		for _, x := range g.raw {
			rows = append(rows, m.floorItemRow(x, i == cur, width, repoW, fx))
			i++
		}
	}

	// viewport: window the rows around the cursor to fit
	bodyH := height - 4 // title, filter, column header, dark footer
	if bodyH < 1 {
		bodyH = 1
	}
	start := 0
	if len(rows) > bodyH {
		start = cur - bodyH/2
		if start < 0 {
			start = 0
		}
		if start > len(rows)-bodyH {
			start = len(rows) - bodyH
		}
	}
	end := mini(start+bodyH, len(rows))
	visible := rows[start:end]

	darkCount := 0
	for _, g := range groups {
		if g.dark {
			darkCount++
		}
	}
	darkLine := "scroll for the rest of the list"
	if darkCount > 0 {
		darkLine = "○ " + itoa(darkCount) + " with nothing out — scroll to the bottom of the list"
	}
	footer := flG(fg(cFaint, truncate(darkLine, inner)))

	top := append([]string{titleLine, filterLine, colHeader}, visible...)
	block := padBlockTo(strings.Join(top, "\n"), height-1)
	return block + "\n" + footer
}

func floorHeaderRow(g floorGroup, on bool, width, repoW int) string {
	bg := ""
	marker := ""
	gColor := g.color
	if on {
		bg, marker, gColor = cSel, "▸", cWhite
	}
	return row(width, bg,
		c("", pad, ""), c(marker, 2, cMid), c(g.glyph, 2, gColor),
		c(g.label, dispWidth(g.label)+1, gColor), flexc(g.rule, cFaint),
		c("", pad, ""))
}

func (m model) floorItemRow(x dispatch, on bool, width, repoW int, fx *shipFxState) string {
	sm := stateMetaBy[x.state]
	shipping := fx != nil && fx.feature == x.feature

	glyph := sm.glyph
	signalText := x.signal
	if shipping {
		if fx.frame > 8 {
			signalText = "shipping"
		} else {
			signalText = "merged ✓"
		}
		glyph = "✓"
		if fx.frame < len(burst) && burst[fx.frame] != "" {
			glyph = burst[fx.frame]
		}
	}

	glyphColor := "#7a7a7a"
	switch {
	case shipping:
		glyphColor = cGreen
	case on, x.urgent:
		glyphColor = sm.color
	}
	color := cFg
	bg := ""
	if on {
		color, bg = cWhite, cSel
	}
	if shipping && fx.frame < 3 {
		bg = "#1e3524"
	}
	repoColor := cDim
	ageColor := cFaint
	if on {
		repoColor, ageColor = cMid, cMid
	}
	mark := ""
	markColor := cMid
	if m.marked[x.feature] {
		mark, markColor = "✓", cGreen
	} else if on {
		mark = "▸"
	}
	signalColor := cDim
	switch {
	case shipping:
		signalColor = cGreen
	case x.urgent:
		signalColor = cRed
	case on:
		signalColor = cMid
	}

	return row(width, bg,
		c("", pad, ""), c(mark, 2, markColor), c(glyph, 2, glyphColor),
		flexc(x.feature, color), c(x.repo, repoW, repoColor),
		cr(signalText, 13, signalColor), cr(m.stackPos(x), 8, m.stackColor(x)), cr(x.age, 6, ageColor),
		c("", pad, ""))
}

// floorDetail is the dispatcher detail pane.
func (m model) floorDetail(width, height int) string {
	sel := m.selected()
	meta := stateMetaBy[sel.state]
	fitv := m.fit()
	inner := width - 2*pad
	seqLen := len(m.seq())
	cursorLabel := itoa(clampCursor(m.cursor, seqLen)+1) + " of " + itoa(seqLen)

	forgeLabel := "github"
	if sel.forge == "ado" {
		forgeLabel = "azure devops"
	}
	from := fromBy[sel.feature]
	if from == "" {
		from = "dispatched by hand"
	}
	said := saidBy[sel.feature]
	if said == "" {
		said = "Working. No message since the turn started."
	}
	selModel := m.modelOf(sel)
	selMode := modeByID(m.modeOf(sel))
	modeColor := cFg
	switch selMode.id {
	case "full":
		modeColor = cRed
	case "auto":
		modeColor = cAmber
	}

	var L []string
	push := func(s string) { L = append(L, flG(s)) }

	push(flSpread(fg(cDim, sel.product+" / "+sel.repo+" · "+forgeLabel+" · from "+from), fg(cFaint, cursorLabel), inner))
	push(fg(cWhite, truncate(sel.feature, inner)))
	stateLine := fg(meta.color, meta.glyph+" "+meta.label+" · "+sel.age) + "  " +
		fg(cFaint, sel.branch) + "  " +
		fg(cWhite, selModel) + fg(cFaint, " M") + "  " +
		fg(modeColor, selMode.label) + fg(cFaint, " p") + "  " +
		fg(cFaint, selMode.note)
	push(truncateAnsi(stateLine, inner))
	push("")
	push(fg(cFaint, "claude said · "+sel.age+" ago"))
	for _, ln := range flWrap(said, inner, 3) {
		push(fg(cFg, ln))
	}
	push(fg(cFaint, "so"))
	for _, ln := range flWrap(sel.why, inner, 2) {
		push(fg(meta.color, ln))
	}
	push("")

	// chain: rule / label / meta
	for _, ln := range floorChainRows(sel.chain, inner) {
		push(ln)
	}

	// agents (wide only)
	if fitv.showAgents && len(sel.agents) > 0 {
		push("")
		now := 0
		for _, a := range sel.agents {
			if a.state == "now" {
				now++
			}
		}
		agentMeta := itoa(maxi(0, len(sel.agents)-1)) + " subagents · " + itoa(now) + " running"
		push(flSpread(fg(cDim, "agents"), fg(cFaint, agentMeta), inner))
		for _, a := range sel.agents {
			st := agentStyle[a.state]
			L = append(L, flG(row(inner, "",
				c(a.branch, 2, cFaint), c(a.name, 16, st.color),
				flexc(a.doing, cMid), c(a.model, 9, cFaint),
				cr(st.glyph+" "+a.meta, 22, st.metaColor))))
		}
	}

	// two-column: last tools | stack + runs
	push("")
	gap := 2
	leftW := (inner - gap) * 115 / 200
	if leftW < 10 {
		leftW = inner - gap
	}
	rightW := inner - gap - leftW
	left := m.floorToolsCol(sel, leftW, fitv)
	right := m.floorStackCol(sel, rightW)
	for _, ln := range flTwoCol(left, right, leftW, rightW, gap) {
		L = append(L, flG(ln))
	}

	// reply row pinned to the bottom
	replyLabel := cDim
	if m.replyFocused {
		replyLabel = cWhite
	}
	var replyBody string
	if m.replyText != "" {
		replyBody = fg(cWhite, m.replyText)
	} else {
		replyBody = fg(cFaint, "r to answer without attaching · enter sends")
	}
	replyLine := flG(fg(replyLabel, "reply") + " " + replyBody)

	block := padBlockTo(strings.Join(L, "\n"), maxi(1, height-1))
	return block + "\n" + replyLine
}

func floorChainRows(chain []chainStep, inner int) []string {
	n := len(chain)
	if n == 0 {
		return nil
	}
	cw := inner / n
	if cw < 1 {
		cw = 1
	}
	var ruleR, labelR, metaR strings.Builder
	for idx, cs := range chain {
		st := chainStyle[cs.state]
		w := cw
		if idx == n-1 {
			w = inner - cw*(n-1)
		}
		ruleR.WriteString(flPad(fg(st.rule, strings.Repeat("─", maxi(1, w-1))), w, ""))
		labelR.WriteString(flPad(fg(st.color, truncate(st.glyph+" "+cs.label, w-1)), w, ""))
		metaR.WriteString(flPad(fg(st.metaColor, truncate(cs.meta, w-1)), w, ""))
	}
	return []string{ruleR.String(), labelR.String(), metaR.String()}
}

func (m model) floorToolsCol(sel dispatch, w int, fitv fitTier) []string {
	var out []string
	out = append(out, fg(cDim, "last tools"))
	for _, a := range sel.activity {
		out = append(out, row(w, "",
			c(a.tool, 6, cDim), flexc(a.arg, cMid),
			c(a.result, dispWidth(a.result), a.resultColor)))
	}
	diffLine := "+" + itoa(sel.plus) + " −" + itoa(sel.minus) + " across " + itoa(sel.files) + " files · " + itoa(sel.commits) + " commits"
	out = append(out, fg(cFaint, truncate(diffLine, w)))

	if fitv.tail {
		followLabel, followColor := "F to follow", cDim
		if m.follow {
			followLabel, followColor = "● following · F to stop", cGreen
		}
		out = append(out, flSpread(fg(cDim, "live output · "+sel.repo), fg(followColor, followLabel), w))
		src := tailLines[sel.feature]
		if src == nil {
			src = tailFallback
		}
		count := 3
		if m.follow {
			count = mini(8, 3+(m.tailN%6))
		}
		if count > len(src) {
			count = len(src)
		}
		for _, t := range src[:count] {
			color := cFg
			switch {
			case strings.Contains(t, "✗"):
				color = cRed
			case strings.HasPrefix(strings.TrimSpace(t), "⎿"):
				color = cDim
			}
			out = append(out, fg(color, truncate(t, w)))
		}
	}

	out = append(out, fg(cDim, "you asked for"))
	for _, ln := range flWrap(sel.prompt, w, 3) {
		out = append(out, fg(cMid, ln))
	}
	return out
}

func (m model) floorStackCol(sel dispatch, w int) []string {
	st := stacks[sel.repo]
	var out []string
	stackRule := "nothing stacked"
	if len(st) > 1 {
		stackRule = itoa(len(st)) + " deep · merges bottom-up"
	}
	out = append(out, flSpread(fg(cDim, "stack · "+sel.repo), fg(cFaint, stackRule), w))
	for n, p := range st {
		ss := stackStateMeta[p.state]
		cur := m.pane == "detail" && n == clampCursor(m.stackCursor, len(st))
		on := cur || (m.pane != "detail" && p.feature == sel.feature)
		marker := ""
		idColor, color := cMid, cFg
		if on {
			marker, idColor, color = "▸", cWhite, cWhite
		}
		rail := "│"
		if n == 0 {
			rail = "└"
		}
		out = append(out, row(w, "",
			c(marker, 2, cFaint), c(rail, 2, ss.railColor), c(p.id, 6, idColor),
			flexc(p.feature, color), cr(ss.label, 11, ss.color)))
		noteColor := cFaint
		if p.state == "behind" || p.state == "blocked" {
			noteColor = ss.color
		}
		out = append(out, strings.Repeat(" ", 4)+fg(noteColor, truncate(p.note, maxi(1, w-4))))
	}
	if len(st) == 0 {
		out = append(out, fg(cFaint, "nothing else in flight in this repo"))
	}
	out = append(out, "")
	out = append(out, fg(cDim, "workflow runs"))
	for _, r := range sel.runs {
		out = append(out, row(w, "",
			flexc(r.name, cMid), c(r.state, dispWidth(r.state)+1, r.color), cr(r.age, 4, cFaint)))
	}
	return out
}

// floorGroupDetail is the roll-up shown when a group header is selected.
func (m model) floorGroupDetail(width, height int) string {
	e := m.entry()
	g := e.g
	mode := m.groupByMode()
	inner := width - 2*pad
	fitv := m.fit()
	seqLen := len(m.seq())
	cursorLabel := itoa(clampCursor(m.cursor, seqLen)+1) + " of " + itoa(seqLen)

	key := g.label
	isProd := mode == "product"
	inflight := len(g.raw)
	pm := productByName(key)
	stx := productStats[key]

	kind := "forge"
	if isProd {
		kind = "product · many repos"
	} else if mode == "repo" {
		kind = "repository"
	}

	// repos + stale
	var repos []repoRef
	if isProd {
		repos = reposByProduct[key]
	} else if mode == "repo" {
		repos = []repoRef{{name: key, out: inflight, ci: "—", ciColor: cDim}}
	}
	var stale []staleRepo
	for _, s := range staleRepos {
		if (isProd && s.product == key) || (!isProd && s.repo == key) {
			stale = append(stale, s)
		}
	}

	// tickets
	tickets := m.headerTickets(g)
	fullCount := 0
	for _, t := range backlogTickets {
		switch {
		case isProd:
			if t.product == key {
				fullCount++
			}
		case mode == "repo":
			if t.repo == key {
				fullCount++
			}
		default:
			if (key == "github" && t.src == "gh") || (key == "azure devops" && t.src == "ado") {
				fullCount++
			}
		}
	}

	sub := itoa(inflight) + " want you"
	if isProd {
		sub = itoa(len(repos)) + " repos · " + itoa(pm.inflight) + " in flight · " + itoa(inflight) + " want you · " + itoa(pm.live) + " live today"
	}
	note := stx.note
	if note == "" {
		note = "No roll-up note for this grouping."
	}

	// stat tiles
	type tile struct{ label, value, meta, color, rule string }
	var tiles []tile
	if isProd {
		rejColor, rejRule := cMid, "#2a2a2a"
		if stx.rejected7d > 2 {
			rejColor, rejRule = cRed, "#e0554a"
		}
		usageColor, usageRule := cMid, "#2a2a2a"
		if stx.pace > 1.25 {
			usageColor, usageRule = cAmber, "#e0a33a"
		}
		tiles = []tile{
			{"shipped 7d", itoa(stx.closed7d), "of " + itoa(stx.dispatched7d) + " dispatched", cWhite, "#2a2a2a"},
			{"rejected", itoa(stx.rejected7d), itoa(pct(stx.rejected7d, stx.dispatched7d)) + "% claims refused", rejColor, rejRule},
			{"lead time", pm.lead, "dispatch → live, median", cWhite, "#2a2a2a"},
			{"live today", itoa(pm.live), itoa(pm.review) + " waiting in review", cWhite, "#2a2a2a"},
			{"usage", itoa(stx.budget) + "%", "of the week · " + strconv.FormatFloat(stx.pace, 'g', -1, 64) + "× pace", usageColor, usageRule},
			{"trend", pm.spark, "merges per day", cGreen, "#2f6b41"},
		}
	} else {
		tiles = []tile{
			{"in flight", itoa(inflight), "want you now", cWhite, "#2a2a2a"},
			{"tickets", itoa(fullCount), "open in the backlog", cWhite, "#2a2a2a"},
		}
	}
	if len(tiles) > fitv.stats {
		tiles = tiles[:fitv.stats]
	}

	var L []string
	push := func(s string) { L = append(L, flG(s)) }
	push(flSpread(fg(cDim, kind), fg(cFaint, cursorLabel), inner))
	push(fg(cWhite, truncate(key, inner)))
	push(fg(cDim, truncate(sub, inner)))
	push("")
	for _, ln := range flWrap(note, inner, 2) {
		push(fg(cMid, ln))
	}
	push("")

	// tiles, three per row
	tw := inner / 3
	if tw < 1 {
		tw = 1
	}
	for base := 0; base < len(tiles); base += 3 {
		chunk := tiles[base:mini(base+3, len(tiles))]
		var ruleR, labelR, valueR, metaR strings.Builder
		for _, t := range chunk {
			ruleR.WriteString(flPad(fg(t.rule, strings.Repeat("─", maxi(1, tw-1))), tw, ""))
			labelR.WriteString(flPad(fg(cFaint, truncate(t.label, tw-1)), tw, ""))
			valueR.WriteString(flPad(fg(t.color, truncate(t.value, tw-1)), tw, ""))
			metaR.WriteString(flPad(fg(cFaint, truncate(t.meta, tw-1)), tw, ""))
		}
		push(ruleR.String())
		push(labelR.String())
		push(valueR.String())
		push(metaR.String())
	}
	push("")

	// tickets | repos + stale
	gap := 2
	leftW := (inner - gap) * 120 / 200
	rightW := inner - gap - leftW

	var tcol []string
	tcol = append(tcol, flSpread(fg(cDim, "outstanding tickets · "+itoa(fullCount)), fg(cFaint, "5 backlog · d dispatch"), leftW))
	for ti, t := range tickets {
		on := m.pane == "detail" && ti == clampCursor(m.ticketCursor, len(tickets))
		bg, marker := "", ""
		if on {
			bg, marker = cSel, "▸"
		}
		priGlyph := "·"
		switch t.pri {
		case "urgent":
			priGlyph = "■"
		case "high":
			priGlyph = "◆"
		}
		titleColor := cFg
		stateTxt, stateColor := t.age+" old", cDim
		if t.taken != "" {
			titleColor = cDim
			stateTxt, stateColor = "dispatched", cGreen
		}
		tcol = append(tcol, row(leftW, bg,
			c(marker, 2, cMid), c(priGlyph, 2, priColor[t.pri]), c(t.id, 17, cMid),
			flexc(t.title, titleColor), cr(stateTxt, 14, stateColor)))
	}

	var rcol []string
	rcol = append(rcol, fg(cDim, "repos"))
	for _, r := range repos {
		rcol = append(rcol, row(rightW, "",
			flexc(r.name, cMid), cr(itoa(r.out)+" out", 6, cFaint), cr(r.ci, 16, r.ciColor)))
	}
	rcol = append(rcol, fg(cDim, "stale"))
	for _, s := range stale {
		dc := cAmber
		if s.days > 30 {
			dc = cRed
		}
		rcol = append(rcol, row(rightW, "", flexc(s.repo, cMid), cr(itoa(s.days)+"d", 6, dc)))
	}
	if len(stale) == 0 {
		rcol = append(rcol, fg(cFaint, "nothing stale here"))
	}

	for _, ln := range flTwoCol(tcol, rcol, leftW, rightW, gap) {
		L = append(L, flG(ln))
	}
	return clampLines(strings.Join(L, "\n"), height)
}

// floorStrip is the bottom activity strip (today / working / week + chips).
func (m model) floorStrip(w int) []string {
	fitv := m.fit()
	leftW := w * fitv.listPct / 100
	rightW := w - leftW
	li := leftW - 2*pad
	ri := rightW - 2*pad

	todayLeft := fg(cDim, "today") + " " + fg(cWhite, "3 live") + " " + fg(cMid, "9 merged · 17 prs · 47 commits, 34 yours-by-proxy")
	l1 := flG(flSpread(todayLeft, fg(cGreen, "▁▁▂▃▂▅▇▅▃▇▆▂"), li))

	workingMeta := "13 · oldest 5h · none idle · w to view"
	if m.workingOpen {
		workingMeta = "13 · showing · w back to wants you"
	}
	workLeft := fg(cDim, "working") + " " + fg(cGreen, strings.Repeat("●", 13))
	l2 := flG(flSpread(workLeft, fg(cFaint, workingMeta), li))

	weekLeft := fg(cDim, "week") + " " + fg(cAmber, bar(65, 12)) + " " + fg(cAmber, "65%") + " " + fg(cFaint, "today 18% · opus 58%")
	l3 := flG(flSpread(weekLeft, fg(cFaint, "6 usage"), li))

	var chips strings.Builder
	first := true
	for _, p := range products {
		if p.inflight == 0 {
			continue
		}
		if !first {
			chips.WriteString("  ")
		}
		first = false
		chipColor := cDim
		if p.needs > 0 {
			chipColor = cFg
		}
		meta := itoa(p.inflight)
		if p.needs > 0 {
			meta += " · " + itoa(p.needs) + "◆"
		}
		chips.WriteString(fg(chipColor, p.name) + fg(cDim, " "+meta))
	}
	rMid := flG(flSpread(chips.String(), fg(cFaint, "6 stale 17d+"), ri))

	left := []string{l1, l2, l3}
	right := []string{"", rMid, ""}
	out := make([]string, 3)
	for i := 0; i < 3; i++ {
		out[i] = flPad(left[i], leftW, "") + flPad(right[i], rightW, "")
	}
	return out
}

// viewDiff is the full-screen diff overlay for the selected dispatcher.
func (m model) viewDiff(w, h int) string {
	sel := m.selected()
	df, ok := diffsBy[sel.feature]
	if !ok {
		df.files, df.hunk = diffFallback.files, diffFallback.hunk
	}
	inner := w - 2*pad

	var L []string
	L = append(L, flG(fg(cDim, "diff")+"  "+fg(cWhite, sel.feature+" · "+sel.repo)))
	L = append(L, "")
	for _, f := range df.files {
		right := fg(cGreen, "+"+itoa(f.plus)) + " " + fg(cRed, "−"+itoa(f.minus))
		L = append(L, flG(flSpread(fg(cFg, truncate(f.path, inner-16)), right, inner)))
	}
	L = append(L, "")
	for _, hl := range df.hunk {
		color := cDim
		switch hl.sign {
		case "+":
			color = cGreen
		case "-":
			color = cRed
		}
		L = append(L, flG(fg(color, truncate(hl.sign+hl.text, inner))))
	}
	return clampLines(strings.Join(L, "\n"), h)
}

// ---- update -----------------------------------------------------------------

func (m model) updateFloor(k string) (model, tea.Cmd) {
	// pane navigation
	switch {
	case k == "right" || k == "l":
		m.pane, m.narrowPane = "detail", "detail"
		m.stackCursor, m.ticketCursor = 0, 0
		return m, nil
	case k == "left" || k == "h" || (k == "esc" && m.pane == "detail"):
		m.pane, m.narrowPane = "list", "list"
		return m, nil
	}

	// group-header ticket navigation
	if m.pane == "detail" && m.entry().header {
		g := m.entry().g
		tix := m.headerTickets(g)
		switch k {
		case "j", "down":
			m.ticketCursor = mini(m.ticketCursor+1, maxi(0, len(tix)-1))
			return m, nil
		case "k", "up":
			m.ticketCursor = maxi(m.ticketCursor-1, 0)
			return m, nil
		case "enter", "d":
			if len(tix) == 0 {
				m.notice = "no open tickets for " + g.label
			} else {
				t := tix[clampCursor(m.ticketCursor, len(tix))]
				if t.taken != "" {
					m.notice = t.id + " already dispatched as \"" + t.taken + "\""
				} else {
					m.notice = "dispatched " + t.id + " → " + t.repo
				}
			}
			return m, nil
		}
	}

	// stack navigation within the detail pane
	if m.pane == "detail" {
		st := stacks[m.selected().repo]
		switch k {
		case "j", "down":
			m.stackCursor = mini(m.stackCursor+1, maxi(0, len(st)-1))
			return m, nil
		case "k", "up":
			m.stackCursor = maxi(m.stackCursor-1, 0)
			return m, nil
		case "enter":
			if len(st) > 0 {
				p := st[clampCursor(m.stackCursor, len(st))]
				id := p.id
				if id == "—" {
					id = p.feature
				}
				m.notice = "opening " + id + " · " + m.selected().repo
			} else {
				m.notice = "nothing stacked here"
			}
			return m, nil
		case "r":
			m.replyFocused = true
			return m, nil
		}
	}

	// list navigation + actions
	seqLen := len(m.seq())
	switch k {
	case "j", "down":
		m.cursor = mini(m.cursor+1, maxi(0, seqLen-1))
		m.notice = ""
	case "k", "up":
		m.cursor = maxi(m.cursor-1, 0)
		m.notice = ""
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = maxi(0, seqLen-1)
	case "w":
		m.workingOpen = !m.workingOpen
		m.cursor = 0
	case "/":
		m.filterOpen = true
	case " ", "space":
		x := m.selected()
		if m.marked[x.feature] {
			delete(m.marked, x.feature)
		} else {
			m.marked[x.feature] = true
		}
	case "esc":
		m.marked = map[string]bool{}
		m.filter = ""
	case "F":
		return m.startFollow()
	case "D":
		m.diffOpen = true
	case "x":
		n := len(m.marked)
		x := m.selected()
		label := "kill \"" + x.feature + "\" in " + x.repo
		feats := []string{x.feature}
		if n > 0 {
			label = "kill " + itoa(n) + " marked dispatchers"
			feats = feats[:0]
			for f := range m.marked {
				feats = append(feats, f)
			}
		}
		m.confirm = &confirmState{label: label + "? their branches survive, the sessions do not", kind: "kill", count: n, features: feats}
	case "M":
		x := m.selected()
		next := models[(flIndex(models, m.modelOf(x))+1)%len(models)]
		m.modelsBy[x.feature] = next
		m.notice = x.feature + " → " + next + " on next turn"
	case "p":
		x := m.selected()
		ids := make([]string, len(modes))
		for i, md := range modes {
			ids[i] = md.id
		}
		next := ids[(flIndex(ids, m.modeOf(x))+1)%len(ids)]
		m.modesBy[x.feature] = next
		m.notice = x.feature + " → " + modeByID(next).label
	case "t":
		order := []string{"band", "product", "repo", "forge"}
		next := order[(flIndex(order, m.groupByMode())+1)%len(order)]
		m.groupBy, m.cursor = next, 0
		m.notice = "grouped " + groupLabel[next]
	case "r":
		m.replyFocused = true
	case "y", "m":
		x := m.selected()
		if m.canShip(x) {
			m.confirm = &confirmState{
				label: "ship \"" + x.feature + "\" to production? " + x.repo + " · squash-merge and deploy",
				kind:  "ship", feature: x.feature, repo: x.repo,
			}
		} else {
			m.notice = "approved · " + x.feature
		}
	case "n":
		x := m.selected()
		if x.state == "claimed" {
			m.notice = "reopened · \"" + x.feature + "\" back to the dispatcher"
		} else {
			m.notice = "denied · told it to stop there"
		}
	case "enter", "a":
		return m.attach(m.selected().feature)
	case "d":
		return m, markDoneCmd(m.selected().feature)
	}
	return m, nil
}
