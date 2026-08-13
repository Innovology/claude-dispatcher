package cockpit

// product.go is a single product's detail PANEL — the right-hand 46% of the
// products area once enter opens a product. It is one column: a title, a
// four-tab strip, and the body of the tab that is up —
//   O overview: velocity, repos + CI, what is in flight, the deploy, stale repos.
//   R review / T team / S shipped: as before.
// Plus two overlays: the review overlay (a PR's findings + hunk) and the resume
// overlay (enhance a shipped feature).
//
// It was three full-width panes and a kanban of its own until the lens bar lost
// two digits; the lanes went with them — the panel is too narrow for four
// columns of cards, and "in flight" says the same thing in a list.

import (
	"strings"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/repos"

	tea "github.com/charmbracelet/bubbletea"
)

// The two empty states, shared with the products lens's summary panel so the
// same product reads the same either side of enter. The way out of an empty
// product is `a`, which the open panel does not own — hence the two hints.
const (
	productNoRepos  = "no repos in this product yet"
	productNoFlight = "nothing in flight"
)

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

// ---- the panel --------------------------------------------------------------

// productPanel is the whole panel: the product's name and one-line summary, the
// tab strip, then the body of the tab that is up. It renders into whatever width
// the products lens hands it — 46% of the terminal, or all of it when the
// terminal is too narrow to hold both.
func (m model) productPanel(cw int) []string {
	name := m.productFocusName()
	var out []string

	out = append(out, row(cw, "", flexc(name, cWhite), cr("esc close", 10, cFaint)))
	out = append(out, line(productSummaryLine(name), cw, cMid, ""))
	out = append(out, "")
	out = append(out, m.productTabStrip(name))
	out = append(out, fg(cRule, strings.Repeat("─", cw)))

	switch m.rightTab {
	case "overview":
		out = append(out, m.productOverviewBody(cw, name)...)
	case "team":
		out = append(out, m.productTeamBody(cw, name)...)
	case "shipped":
		out = append(out, m.productShippedBody(cw)...)
	default:
		out = append(out, m.productReviewBody(cw, name)...)
	}
	return out
}

// productSummaryLine is the line under the title: how much of the factory this
// product is, in the three numbers the portfolio table ranks products by.
func productSummaryLine(name string) string {
	p := productByName(name)
	n := len(reposByProduct[name])
	return itoa(n) + " " + plural(n, "repo", "repos") + " · " + itoa(p.inflight) + " in flight · " + itoa(p.live) + " live today"
}

// productTabStrip is the O/R/T/S strip. The counts are on the tabs because they
// are the reason to switch to one: nothing waiting is worth a keypress to see.
func (m model) productTabStrip(name string) string {
	waiting := 0
	for _, r := range reviews[name] {
		if r.waiting == "you" {
			waiting++
		}
	}
	tab := func(id, label string) string {
		if m.rightTab == id {
			return fg(cWhite, label)
		}
		return fg(cDim, label)
	}
	return strings.Join([]string{
		tab("overview", "O overview"),
		tab("review", "R review "+itoa(waiting)),
		tab("team", "T team"),
		tab("shipped", "S shipped "+itoa(len(m.shippedFlat()))),
	}, "  ")
}

// productOverviewBody is the O tab: the product's velocity, its repos and their
// CI, what is in flight in it, its deploy and its stale repos — the four panes
// the product lens used to spread across the whole screen, in one column.
func (m model) productOverviewBody(cw int, name string) []string {
	var out []string

	out = append(out, line("velocity", cw, cDim, ""))
	tiles := productVelocity[name]
	if len(tiles) == 0 {
		// No fallback to another product's figures: an unmeasured product says so.
		out = append(out, fg(cFaint, "nothing measured for this product yet"))
	}
	for _, t := range tiles {
		band := bandColor[t.band]
		// The design bands each metric with a coloured left edge rather than a
		// tile, which is what buys the row its width back.
		out = append(out, fg(band, "▎")+" "+row(maxi(cw-2, 1), "",
			flexc(t.k, cDim),
			cr(t.v, 10, cWhite),
			cr(t.spark, 15, band)))
	}

	out = append(out, "")
	out = append(out, line("repos", cw, cDim, ""))
	if len(reposByProduct[name]) == 0 {
		out = append(out, fg(cFaint, productNoRepos+" — esc, then a to assign some"))
	}
	for _, r := range reposByProduct[name] {
		out = append(out, row(cw, "",
			flexc(r.name, cMid),
			cr(itoa(r.out)+" out", 9, cDim),
			cr(r.ci, 21, r.ciColor)))
	}

	out = append(out, "")
	out = append(out, line("in flight · working → claims done → review → live", cw, cDim, ""))
	feats := productsFocusFeatures(name)
	if len(feats) == 0 {
		out = append(out, fg(cFaint, productNoFlight))
	}
	for _, f := range feats {
		out = append(out, row(cw, "",
			c(f.glyph, 2, f.color),
			flexc(f.feature, cMid),
			c(f.repo, 17, cFaint),
			cr(f.age, 5, cDim)))
	}

	out = append(out, "")
	out = append(out, line("product deploy", cw, cDim, ""))
	// Read from the repo's actual deploy workflow. This block used to assert
	// "Deploy production · gh actions ✓ last green 41m ago · 6 runs today" for
	// every product on every machine — the design's mock, transcribed.
	pipeline, glyph, status, statusColor := m.deployLine(name)
	if status == "" {
		out = append(out, line(pipeline, cw, cFaint, ""))
	} else {
		out = append(out, row(cw, "",
			c(glyph, 2, statusColor),
			flexc(status, cMid),
			cr(pipeline, mini(dispWidth(pipeline)+1, cw/2), cFaint)))
	}

	out = append(out, "")
	out = append(out, line("stale in "+name, cw, cDim, ""))
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
	// `c clone to another repo` is not offered: nothing implements it, and a key
	// the footer names that does nothing is worse than an absent one.
	out = append(out, fg(cDim, "enter dispatch again · o open pr"))
	return out
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
			text := strings.TrimSpace(m.resumeText)
			m.resumeOpen, m.resumeText = false, ""
			if text == "" {
				m.notice = "nothing to dispatch — type what to change"
				return m, nil
			}
			// This used to set a notice saying the feature had been "dispatched
			// again" and launch nothing. It re-dispatches the same feature, which
			// reuses its branch and worktree, with the typed text as the prompt.
			m.notice = "dispatching \"" + sel.feature + "\" again…"
			return m, launchCmd(m.cfg, sel.repo, sel.feature, text)
		}
		m.resumeText = next
		return m, nil
	}

	// esc closes the panel and hands the portfolio back. It is not a lens change
	// in the human's terms — the products lens was never left. q stays quit
	// everywhere, so the panel does not claim it and the footer does not offer it.
	if k == "esc" {
		m.lens = "products"
		return m, nil
	}

	// tab switches
	switch k {
	case "O":
		m.rightTab = "overview"
		return m, nil
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
	case "team", "overview":
		// Neither tab has a cursor. Without this the fall-through below would
		// move the shipped cursor from a tab that cannot show it moving.
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
	case "o":
		sel := m.shipSelected()
		if sel.pr == "" || sel.pr == "—" {
			m.notice = "\"" + sel.feature + "\" has no pull request"
			return m, nil
		}
		return m, openPRCmd(sel.repo, sel.pr)
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
