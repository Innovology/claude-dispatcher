package cockpit

// product.go is lens 3: a single product's detail view. Three panes —
//   left:   the product's repos + CI, its deploy block, and its stale repos.
//   middle: velocity tiles and the in-flight kanban lanes.
//   right:  a tabbed panel over review / team / shipped.
// Plus two overlays: the review overlay (a PR's findings + hunk) and the resume
// overlay (enhance a shipped feature). A faithful port of the design's product
// lens (renderVals 1821-1900) and its key handling (handleKey 1285-1316).

import (
	"strings"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/repos"

	tea "github.com/charmbracelet/bubbletea"
)

// ---- lane types -------------------------------------------------------------

type laneCard struct{ feature, repo, meta, metaColor string }

type lane struct {
	name, color string
	items       []laneCard
}

// ---- selection helpers ------------------------------------------------------

func (m model) productFocusName() string {
	if len(products) == 0 {
		return "—"
	}
	return products[clampCursor(m.productCursor, len(products))].name
}

// shippedFlat flattens shipped[name] into one newest-first list (SHIPPED order).
func (m model) shippedFlat() []shippedItem {
	var out []shippedItem
	for _, d := range shipped[m.productFocusName()] {
		out = append(out, d.items...)
	}
	return out
}

// shipSelected returns the shipped feature under the ship cursor, or a placeholder.
func (m model) shipSelected() shippedItem {
	f := m.shippedFlat()
	if len(f) == 0 {
		return shippedItem{feature: "—", repo: "—", pr: "—", at: "—", session: "—", closedBy: "—", prompt: "nothing shipped for this product yet."}
	}
	return f[clampCursor(m.shipCursor, len(f))]
}

// productSelectedReview returns the review under the review cursor, if any.
func (m model) productSelectedReview() (reviewItem, bool) {
	rv := reviews[m.productFocusName()]
	if len(rv) == 0 {
		return reviewItem{}, false
	}
	return rv[clampCursor(m.reviewCursor, len(rv))], true
}

// productLanes derives the in-flight kanban lanes for the selected product from
// the live working/dispatches/shipped data (renderVals 1827-1832).
func (m model) productLanes() []lane {
	name := m.productFocusName()

	var work []laneCard
	for _, w := range working {
		if repoProduct(w.repo) == name {
			work = append(work, laneCard{feature: w.feature, repo: w.repo, meta: w.age, metaColor: cDim})
		}
	}

	var claims, review []laneCard
	for _, x := range dispatches {
		if x.product != name {
			continue
		}
		switch x.state {
		case "claimed":
			claims = append(claims, laneCard{feature: x.feature, repo: x.repo, meta: "accept to close · " + x.age, metaColor: cViolet})
		case "review", "blocked":
			meta := x.signal
			if len(x.prs) > 0 && x.prs[0].id != "" {
				meta = x.prs[0].id + " · " + x.signal
			}
			mc := cMid
			if x.urgent {
				mc = cRed
			}
			review = append(review, laneCard{feature: x.feature, repo: x.repo, meta: meta, metaColor: mc})
		}
	}

	var shippedToday []laneCard
	if days := shipped[name]; len(days) > 0 {
		for _, f := range days[0].items {
			shippedToday = append(shippedToday, laneCard{feature: f.feature, repo: f.repo, meta: f.at, metaColor: cDim})
		}
	}

	return []lane{
		{name: "working", color: "#2f6b41", items: work},
		{name: "claims done", color: "#5a4a7a", items: claims},
		{name: "in review", color: "#33507e", items: review},
		{name: "shipped today", color: "#4a4a4a", items: shippedToday},
	}
}

// ---- small render helpers ---------------------------------------------------

// productWrap word-wraps s to lines of at most w columns.
func productWrap(s string, w int) []string {
	if w <= 0 || s == "" {
		return []string{s}
	}
	var out []string
	cur := ""
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case dispWidth(cur)+1+dispWidth(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// productWaitColor is amber when a PR is waiting on you, dim otherwise.
func productWaitColor(waiting string) string {
	if waiting == "you" {
		return cAmber
	}
	return cDim
}

// productCheckColor colours a CI checks string: red on failure, blue in progress, else green.
func productCheckColor(checks string) string {
	switch {
	case strings.Contains(checks, "✗"):
		return cRed
	case strings.Contains(checks, "●"):
		return cBlue
	default:
		return cGreen
	}
}

// productHunkColor colours a diff line by its sign.
func productHunkColor(sign string) string {
	switch sign {
	case "+":
		return cGreen
	case "-":
		return cRed
	default:
		return cDim
	}
}

// productChunkBlocks lays a list of equal-width column blocks into rows of n, hjoining
// each row and stacking the rows with a blank line between them.
func productChunkBlocks(blocks []string, n int) string {
	if n < 1 {
		n = 1
	}
	var rows []string
	for i := 0; i < len(blocks); i += n {
		end := mini(i+n, len(blocks))
		rows = append(rows, hjoin(blocks[i:end]...))
	}
	return strings.Join(rows, "\n")
}

// ---- panes ------------------------------------------------------------------

// leftPane: repos + CI, the product deploy block, and stale repos.
func (m model) productLeftPane(cw int) []string {
	name := m.productFocusName()
	p := productByName(name)
	var out []string

	out = append(out, line(name, cw, cWhite, ""))
	summary := itoa(len(reposByProduct[name])) + " repos · " + itoa(p.inflight) + " in flight · " + itoa(p.live) + " live today"
	out = append(out, line(summary, cw, cMid, ""))
	out = append(out, "")
	out = append(out, line("repos", cw, cDim, ""))
	if len(reposByProduct[name]) == 0 {
		out = append(out, fg(cFaint, "no repos in this product yet — 2, then a to assign some"))
	}
	for _, r := range reposByProduct[name] {
		out = append(out, row(cw, "", flexc(r.name, cFg), cr(itoa(r.out)+" out", 8, cDim)))
		out = append(out, line(r.ci, cw, r.ciColor, ""))
	}
	out = append(out, "")
	out = append(out, line("product deploy", cw, cDim, ""))
	// Read from the repo's actual deploy workflow. This block used to assert
	// "Deploy production · gh actions ✓ last green 41m ago · 6 runs today" for
	// every product on every machine — the design's mock, transcribed.
	pipeline, glyph, status, statusColor := m.deployLine(name)
	out = append(out, line(pipeline, cw, cMid, ""))
	if status != "" {
		out = append(out, fg(statusColor, glyph)+fg(cMid, " "+status))
	}
	out = append(out, "")
	out = append(out, line("stale repos in "+name, cw, cDim, ""))
	if !productHasStale(name) {
		out = append(out, fg(cFaint, "nothing stale"))
	}
	for _, s := range staleRepos {
		if s.product != name {
			continue
		}
		col := cAmber
		if s.days > 30 {
			col = cRed
		}
		out = append(out, row(cw, "", flexc(s.repo, cMid), cr(itoa(s.days)+"d", 5, col)))
	}
	return out
}

// midPane: velocity tiles row then the in-flight lanes.
func (m model) productMidPane(cw int, tilesPerRow, lanesPerRow int) []string {
	name := m.productFocusName()
	var out []string

	// velocity tiles
	tiles := productVelocity[name]
	if len(tiles) == 0 {
		tiles = productVelocity["unassigned"]
	}
	tw := cw / tilesPerRow
	if tw < 8 {
		tw = cw
		tilesPerRow = 1
	}
	tileBlocks := make([]string, 0, len(tiles))
	for _, t := range tiles {
		band := bandColor[t.band]
		inner := tw - 2
		if inner < 4 {
			inner = tw
		}
		l1 := padTo(fg(band, strings.Repeat("─", inner)), tw, alignLeft)
		l2 := padTo(fg(cFaint, truncate(t.k, inner)), tw, alignLeft)
		l3 := padTo(row(inner, "", flexc(t.v, cWhite), cr(t.spark, dispWidth(t.spark), band)), tw, alignLeft)
		tileBlocks = append(tileBlocks, strings.Join([]string{l1, l2, l3}, "\n"))
	}
	out = append(out, productChunkBlocks(tileBlocks, tilesPerRow))
	out = append(out, "")

	// in-flight header
	out = append(out, row(cw, "",
		c("in flight", dispWidth("in flight")+1, cDim),
		flexc("", ""),
		cr("working → claims done → review → live", dispWidth("working → claims done → review → live"), cFaint)))
	out = append(out, "")

	// lanes
	lanes := m.productLanes()
	lw := cw / lanesPerRow
	if lw < 12 {
		lw = cw
		lanesPerRow = 1
	}
	laneBlocks := make([]string, 0, len(lanes))
	for _, l := range lanes {
		inner := lw - 2
		if inner < 6 {
			inner = lw
		}
		var lines []string
		lines = append(lines, padTo(fg(l.color, strings.Repeat("─", inner)), lw, alignLeft))
		lines = append(lines, row(lw, "", flexc(l.name, cMid), cr(itoa(len(l.items)), 4, cFaint)))
		for _, cd := range l.items {
			lines = append(lines, line(cd.feature, lw, cFg, ""))
			lines = append(lines, line(cd.repo, lw, cDim, ""))
			lines = append(lines, line(cd.meta, lw, cd.metaColor, ""))
		}
		laneBlocks = append(laneBlocks, strings.Join(lines, "\n"))
	}
	out = append(out, productChunkBlocks(laneBlocks, lanesPerRow))
	return out
}

// rightPane: the R review / T team / S shipped tabbed panel.
func (m model) productRightPane(cw int) []string {
	name := m.productFocusName()
	var out []string

	// tab bar
	waitingCount := 0
	for _, r := range reviews[name] {
		if r.waiting == "you" {
			waitingCount++
		}
	}
	shipCount := len(m.shippedFlat())
	tabColor := func(id string) string {
		if m.rightTab == id {
			return cWhite
		}
		return cDim
	}
	tabs := fg(tabColor("review"), "R review "+itoa(waitingCount)) + "  " +
		fg(tabColor("team"), "T team") + "  " +
		fg(tabColor("shipped"), "S shipped "+itoa(shipCount))
	out = append(out, tabs)
	out = append(out, "")

	switch m.rightTab {
	case "team":
		out = append(out, m.productTeamBody(cw, name)...)
	case "shipped":
		out = append(out, m.productShippedBody(cw)...)
	default:
		out = append(out, m.productReviewBody(cw, name)...)
	}
	return out
}

func (m model) productReviewBody(cw int, name string) []string {
	rv := reviews[name]
	sel := clampCursor(m.reviewCursor, len(rv))
	var out []string
	for n, r := range rv {
		bg := ""
		marker := " "
		titleColor := cFg
		if n == sel {
			bg, marker, titleColor = cSel, "▸", cWhite
		}
		out = append(out, row(cw, bg,
			c(marker, 2, cMid),
			c(r.pr, 7, cMid),
			flexc(r.title, titleColor),
			cr(r.waiting, 9, productWaitColor(r.waiting)),
			cr(r.age, 5, cFaint)))
		reviewer := ""
		reviewerColor := cFaint
		if r.reviewer != nil {
			reviewer, reviewerColor = r.reviewer.label, r.reviewer.color
		}
		out = append(out, row(cw, bg,
			c("", 3, ""),
			c(r.repo, 16, cFaint),
			c(r.checks, 12, productCheckColor(r.checks)),
			c(r.size, 10, cFaint),
			flexc(reviewer, reviewerColor)))
	}
	out = append(out, "")
	for _, s := range productWrap("enter review it yourself · d dispatch a reviewer · a approve · c request changes", cw) {
		out = append(out, fg(cFaint, s))
	}
	return out
}

func (m model) productTeamBody(cw int, name string) []string {
	var out []string
	out = append(out, row(cw, "",
		c("WHO", 9, cFaint),
		cr("LIVE", 7, cFaint),
		cr("OPENED", 8, cFaint),
		cr("REVIEWS", 9, cFaint),
		cr("DEBT", 7, cFaint)))
	for _, t := range team[name] {
		ratio := 1.0
		if t.opened > 0 {
			ratio = float64(t.reviews) / float64(t.opened)
		}
		verdict, verdictColor := "slowing the team", cRed
		switch {
		case ratio >= 0.8:
			verdict, verdictColor = "accelerating the team", cGreen
		case ratio >= 0.4:
			verdict, verdictColor = "holding even", cMid
		}
		color, bg := cFg, ""
		if t.me {
			color, bg = cWhite, cSel
		}
		revColor := cFg
		if ratio < 0.4 {
			revColor = cRed
		}
		debtColor := cDim
		if t.debt > 1 {
			debtColor = cAmber
		}
		out = append(out, row(cw, bg,
			c(t.who, 9, color),
			cr(itoa(t.live7), 7, color),
			cr(itoa(t.opened), 8, color),
			cr(itoa(t.reviews), 9, revColor),
			cr(itoa(t.debt), 7, debtColor)))
		out = append(out, fg(verdictColor, verdict)+fg(cFaint, " reviews in "+t.latency))
	}
	out = append(out, "")
	for _, s := range productWrap(teamVerdict[name], cw) {
		out = append(out, fg(cAmber, s))
	}
	return out
}

func (m model) productShippedBody(cw int) []string {
	name := m.productFocusName()
	flat := m.shippedFlat()
	sel := clampCursor(m.shipCursor, len(flat))
	var out []string
	fi := 0
	for _, d := range shipped[name] {
		out = append(out, fg(cFaint, d.day))
		for _, f := range d.items {
			bg := ""
			marker := " "
			featColor := cFg
			if fi == sel {
				bg, marker, featColor = cSel, "▸", cWhite
			}
			out = append(out, row(cw, bg,
				c(marker, 2, cFaint),
				flexc(f.feature, featColor),
				c(f.repo, 16, cDim),
				cr(f.at, 7, cFaint)))
			fi++
		}
	}
	// selected detail
	s := m.shipSelected()
	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	out = append(out, fg(cMid, s.feature)+fg(cFaint, " · "+s.pr+" · session "+s.session))
	out = append(out, fg(cFaint, s.closedBy))
	for _, ln := range productWrap(s.prompt, cw) {
		out = append(out, fg(cMid, ln))
	}
	out = append(out, fg(cDim, "enter enhance · c clone to another repo · o open pr"))
	return out
}

// ---- viewProduct ------------------------------------------------------------

func (m model) viewProduct(w, h int) string {
	f := m.fit()
	tilesPerRow := 100 / f.velTilePct
	lanesPerRow := 100 / f.lanePct
	if tilesPerRow < 1 {
		tilesPerRow = 1
	}
	if lanesPerRow < 1 {
		lanesPerRow = 1
	}

	joinPane := func(lines []string, inset int) string {
		block := gutter(strings.Join(lines, "\n"), inset)
		return padBlockTo(clampLines(block, h), h)
	}

	var body string
	switch {
	case m.width >= 170:
		// three panes
		avail := w - 2 // two vrules
		leftW := maxi(avail*24/100, 22)
		rightW := maxi(avail*30/100, 34)
		midW := avail - leftW - rightW
		if midW < 30 {
			midW = 30
		}
		left := joinPane(m.productLeftPane(leftW-pad), pad)
		mid := joinPane(m.productMidPane(midW-1, tilesPerRow, lanesPerRow), 1)
		right := joinPane(m.productRightPane(rightW-1), 1)
		body = hjoin(left, vrule(h, cRule), mid, vrule(h, cRule), right)
	case m.width >= 110:
		// two panes: middle + right
		avail := w - 1
		rightW := maxi(avail*36/100, 34)
		midW := avail - rightW
		mid := joinPane(m.productMidPane(midW-pad, tilesPerRow, lanesPerRow), pad)
		right := joinPane(m.productRightPane(rightW-1), 1)
		body = hjoin(mid, vrule(h, cRule), right)
	default:
		// narrow: middle only
		body = joinPane(m.productMidPane(w-pad, tilesPerRow, lanesPerRow), pad)
	}
	return clampLines(body, h)
}

// ---- review overlay ---------------------------------------------------------

func (m model) viewReview(w, h int) string {
	it, ok := m.productSelectedReview()
	cw := w - 2*pad
	if cw < 10 {
		cw = w
	}
	pr, title := "—", "nothing waiting"
	if ok {
		pr, title = it.pr, it.title
	}
	var out []string

	meta := ""
	if ok {
		meta = it.repo + " · " + it.size + " · " + it.age + " old"
	}
	out = append(out, row(cw, "",
		c("review "+pr, dispWidth("review "+pr)+2, cDim),
		flexc("", ""),
		cr(meta, dispWidth(meta), cFaint)))
	for _, s := range productWrap(title, cw) {
		out = append(out, fg(cWhite, s))
	}
	mineLine, mineColor := "opened by "+it.author, cDim
	if it.mine {
		mineLine, mineColor = "your own PR — you cannot approve it, but a reviewer dispatcher can read it", cAmber
	}
	out = append(out, fg(mineColor, mineLine))
	if it.summary != "" {
		out = append(out, "")
		for _, s := range productWrap(it.summary, mini(cw, 78)) {
			out = append(out, fg(cMid, s))
		}
	}

	out = append(out, "")
	out = append(out, fg(cDim, "what a reviewer dispatcher found"))
	fds := findings[pr]
	if len(fds) == 0 {
		out = append(out, fg(cFaint, "no reviewer has read this yet · d dispatches one"))
	}
	for _, fd := range fds {
		wrapped := productWrap(fd.text, maxi(cw-16, 10))
		out = append(out, row(cw, "", c(fd.sev, 14, fd.color), flexc(wrapped[0], cFg)))
		for _, cont := range wrapped[1:] {
			out = append(out, row(cw, "", c("", 14, ""), flexc(cont, cFg)))
		}
	}

	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	hunk := diffsByPR[pr]
	if len(hunk) == 0 {
		out = append(out, fg(cFaint, "no diff loaded for this pr"))
	}
	for _, hl := range hunk {
		col := productHunkColor(hl.sign)
		out = append(out, fg(col, truncate(padTo(hl.sign, 2, alignLeft)+hl.text, cw)))
	}

	out = append(out, "")
	hints := fg(cWhite, "a") + fg(cDim, " approve yourself   ") +
		fg(cWhite, "c") + fg(cDim, " request changes   ") +
		fg(cWhite, "d") + fg(cDim, " dispatch a reviewer   ") +
		fg(cWhite, "esc") + fg(cDim, " close")
	out = append(out, hints)

	return clampLines(gutter(strings.Join(out, "\n"), pad), h)
}

// ---- resume overlay ---------------------------------------------------------

func (m model) viewResume(w, h int) string {
	s := m.shipSelected()
	cw := w - 2*pad
	if cw < 10 {
		cw = w
	}
	var out []string
	out = append(out, fg(cDim, "enhance a shipped feature"))
	out = append(out, line(s.feature, cw, cWhite, ""))
	out = append(out, fg(cDim, s.repo+" · "+s.pr+" · shipped "+s.at))
	for _, ln := range productWrap("resumes session "+s.session+" with its full context — the branch is recreated from the merge commit", cw) {
		out = append(out, fg(cFaint, ln))
	}
	out = append(out, "")
	out = append(out, fg(cDim, "originally"))
	for _, ln := range productWrap(s.prompt, cw) {
		out = append(out, fg(cMid, ln))
	}
	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	out = append(out, fg(cAmber, "now ")+fg(cWhite, m.resumeText+"▏"))
	out = append(out, fg(cFaint, "enter dispatch · esc cancel"))
	return clampLines(gutter(strings.Join(out, "\n"), pad), h)
}

// ---- updateProduct ----------------------------------------------------------

func (m model) updateProduct(k string) (model, tea.Cmd) {
	rv := reviews[m.productFocusName()]
	cur, hasCur := m.productSelectedReview()

	// review overlay first (mirrors handleKey 1289-1295)
	if m.reviewOpen {
		switch k {
		case "esc", "q":
			m.reviewOpen = false
		case "a":
			m.reviewOpen = false
			if hasCur {
				m.notice = "approved " + cur.pr + " · you reviewed it"
			}
		case "c":
			m.reviewOpen = false
			if hasCur {
				m.notice = "changes requested on " + cur.pr
			}
		case "d":
			m.reviewOpen = false
			if hasCur {
				m.notice = "reviewer dispatched on " + cur.pr + " · it reports back as needs-you"
			}
		}
		return m, nil
	}

	// resume overlay (mirrors handleKey 1223-1230)
	if m.resumeOpen {
		next, submit, cancel := applyEdit(m.resumeText, k, m.key)
		if cancel {
			m.resumeOpen, m.resumeText = false, ""
			return m, nil
		}
		if submit {
			sel := m.shipSelected()
			m.resumeOpen, m.resumeText = false, ""
			m.notice = "resumed session " + sel.session + " · \"" + sel.feature + "\" dispatched again"
			return m, nil
		}
		m.resumeText = next
		return m, nil
	}

	// tab switches
	switch k {
	case "R":
		m.rightTab, m.reviewCursor = "review", 0
		return m, nil
	case "T":
		m.rightTab = "team"
		return m, nil
	case "S":
		m.rightTab = "shipped"
		return m, nil
	}

	switch m.rightTab {
	case "review":
		switch k {
		case "j", "down":
			if len(rv) > 0 {
				m.reviewCursor = mini(m.reviewCursor+1, len(rv)-1)
			}
		case "k", "up":
			m.reviewCursor = maxi(m.reviewCursor-1, 0)
		case "enter":
			if hasCur {
				m.reviewOpen = true
			}
		case "a":
			if hasCur {
				if cur.mine {
					m.notice = "you cannot approve your own " + cur.pr
				} else {
					m.notice = "approved " + cur.pr
				}
			}
		case "c":
			if hasCur {
				m.notice = "changes requested on " + cur.pr
			}
		case "d":
			if hasCur {
				m.notice = "reviewer dispatched on " + cur.pr + " · claude reads it and reports findings"
			}
		}
		return m, nil
	case "team":
		return m, nil
	}

	// shipped tab
	n := len(m.shippedFlat())
	switch k {
	case "j", "down":
		if n > 0 {
			m.shipCursor = mini(m.shipCursor+1, n-1)
		}
	case "k", "up":
		m.shipCursor = maxi(m.shipCursor-1, 0)
	case "enter":
		if n > 0 {
			m.resumeOpen, m.resumeText = true, ""
		}
	case "c":
		m.notice = "clone \"" + m.shipSelected().feature + "\" into which repo?"
	case "o":
		m.notice = "opening " + m.shipSelected().pr
	}
	return m, nil
}

// deployLine describes the product's deploy pipeline and its latest run. It
// reports honestly when there is no workflow, when gh cannot answer, and when a
// run is still going — none of which is the same as green.
func (m model) deployLine(product string) (pipeline, glyph, status, color string) {
	refs := reposByProduct[product]
	if len(refs) == 0 {
		return "no repos in this product", "", "", cFaint
	}
	r, ok := discoveredRepo(refs[0].name)
	if !ok {
		return "deploy workflow unknown", "", "", cFaint
	}
	override := ""
	if m.cfg != nil {
		override = m.cfg.DeployWorkflows[refs[0].name]
	}
	p := gh.DeployStatus(r.Path, override)
	if p.Name == "" {
		return "no deploy workflow · merge counts as live", "", "", cFaint
	}
	pipeline = p.Name + " · " + refs[0].name
	runs := ""
	if p.RunsToday > 0 {
		runs = " · " + itoa(p.RunsToday) + " " + plural(p.RunsToday, "run", "runs") + " today"
	}
	switch {
	case p.Status != "completed" && p.Status != "":
		return pipeline, "●", "running now" + runs, cBlue
	case p.Conclusion == "success":
		return pipeline, "✓", "last green " + floorAge(p.At) + " ago" + runs, cGreen
	case p.Conclusion == "":
		return pipeline, "·", "no runs yet", cFaint
	default:
		return pipeline, "✗", "last run " + p.Conclusion + " " + floorAge(p.At) + " ago" + runs, cRed
	}
}

// productHasStale reports whether any stale repo belongs to the product.
func productHasStale(name string) bool {
	for _, s := range staleRepos {
		if s.product == name {
			return true
		}
	}
	return false
}

// discoveredRepo finds a scanned repo by name.
func discoveredRepo(name string) (repos.Repo, bool) {
	for _, r := range lastDiscovered {
		if r.Name == name {
			return r, true
		}
	}
	return repos.Repo{}, false
}
