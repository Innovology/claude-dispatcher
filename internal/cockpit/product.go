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

	dispatchpkg "claude-dispatcher/internal/dispatch"
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

// historyFlat is every finished dispatcher in the focused product, newest first.
func (m model) historyFlat() []historyItem { return productHistory[m.productFocusName()] }

// historySelected returns the finished dispatcher under the history cursor.
func (m model) historySelected() (historyItem, bool) {
	h := m.historyFlat()
	if len(h) == 0 {
		return historyItem{}, false
	}
	return h[clampCursor(m.historyCursor, len(h))], true
}

// ---- the resume target ------------------------------------------------------

// resumeTarget is the dispatcher the resume overlay is about, from whichever
// tab opened it. id addresses the record; session is the conversation to pick
// back up, and "" means there is none — the overlay then offers the only honest
// alternative, which is to dispatch the feature again.
type resumeTarget struct {
	id, feature, repo, pr, at, session, prompt string
}

// resumable reports whether this target can actually be resumed: a record we
// can find, and a session id on it.
func (t resumeTarget) resumable() bool { return t.id != "" && t.session != "" }

func shipTarget(s shippedItem) *resumeTarget {
	return &resumeTarget{id: s.id, feature: s.feature, repo: s.repo, pr: s.pr,
		at: "shipped " + s.at, session: s.session, prompt: s.prompt}
}

func historyTarget(h historyItem) *resumeTarget {
	return &resumeTarget{id: h.id, feature: h.feature, repo: h.repo, pr: h.pr,
		at: h.ended + " " + h.at, session: h.session, prompt: h.prompt}
}

// resumeSelected is the overlay's target: the one it opened on, falling back to
// the shipped cursor for a caller that opened the overlay without setting one.
func (m model) resumeSelected() resumeTarget {
	if m.resumeAt != nil {
		return *m.resumeAt
	}
	return *shipTarget(m.shipSelected())
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
// ch is the height it has to fill. Only the history tab reads it, and it has
// to: its list is as long as the product's past, and the lines that fall off
// the bottom are the ones that matter — the session id and `enter resume it`
// live under the list, so on a 28-row terminal the way back into a session was
// already gone, while j went on moving a cursor nobody could see.
func (m model) productPanel(cw, ch int) []string {
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
	case "history":
		out = append(out, m.productHistoryBody(cw, ch-len(out))...)
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

// productTabStrip is the O/R/T/S/H strip. The counts are on the tabs because
// they are the reason to switch to one: nothing waiting is worth a keypress to
// see.
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
		tab("history", "H history "+itoa(len(m.historyFlat()))),
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
	out = append(out, fg(cDim, "enter pick it back up · o open pr · H every finished session"))
	return out
}

// productHistoryBody is the H tab: every dispatcher in this product whose
// session is over, newest first, and the way back into one.
//
// This is the tab that answers "where did it go". A dispatcher that was killed,
// that ended without opening a PR, or that was marked shipped by hand is on no
// other screen in the cockpit — SHIPPED is a ship log and the triage table is
// what is in flight — so without it the session, its transcript and its branch
// were all still on disk with nothing anywhere able to reach them.
// The detail block is built before the list and the list gets what is left of
// ch, because the detail is the half that must never be pushed off: it names
// the session and carries the key that reopens it. A product's history only
// grows, so "it fits today" is not a property this can be built on.
func (m model) productHistoryBody(cw, ch int) []string {
	items := m.historyFlat()
	if len(items) == 0 {
		return []string{fg(cFaint, "no dispatcher in this product has finished yet")}
	}
	sel := clampCursor(m.historyCursor, len(items))

	h, _ := m.historySelected()
	detail := []string{fg(cRule, strings.Repeat("─", cw))}
	ref := h.pr
	if ref == "" {
		ref = "no pr"
	}
	detail = append(detail, fg(cMid, h.feature)+fg(cFaint, " · "+ref+" · "+h.ended))
	// The session id is the handle resume works through, so its absence is
	// stated rather than left as a blank.
	if h.session == "" {
		detail = append(detail, fg(cFaint, "no session recorded — enter dispatches it again"))
	} else {
		detail = append(detail, fg(cFaint, "session "+h.session))
	}
	for _, ln := range productWrap(h.prompt, cw) {
		detail = append(detail, fg(cMid, ln))
	}
	detail = append(detail, fg(cDim, "enter resume it · o open pr"))

	var rows []string
	for i, it := range items {
		bg, marker, featColor := "", " ", cFg
		if i == sel {
			bg, marker, featColor = cSel, "▸", cWhite
		}
		endedColor := cDim
		if it.ended == "stopped" {
			endedColor = cFaint
		}
		rows = append(rows, row(cw, bg,
			c(marker, 2, cFaint),
			flexc(it.feature, featColor),
			c(it.repo, 16, cDim),
			c(it.ended, 15, endedColor),
			cr(it.at, 8, cFaint)))
	}
	if older := historyOlder[m.productFocusName()]; older > 0 {
		rows = append(rows, fg(cFaint, "…and "+itoa(older)+" older"))
	}

	return append(historyWindow(rows, sel, ch-len(detail)), detail...)
}

// historyWindow returns the slice of rows that fits in budget lines and holds
// the selected one, replacing the row it displaces at each cut with a count of
// what lies beyond it. A list that simply stopped would read as the whole of a
// product's past, and j would walk the cursor off a screen that never moved.
func historyWindow(rows []string, sel, budget int) []string {
	// Below three there is nothing left to say: one row and two markers.
	if budget < 3 {
		budget = 3
	}
	if len(rows) <= budget {
		return rows
	}
	start := sel - budget/2
	if start < 0 {
		start = 0
	}
	if start+budget > len(rows) {
		start = len(rows) - budget
	}
	out := append([]string(nil), rows[start:start+budget]...)
	if start > 0 {
		out[0] = fg(cFaint, "↑ "+itoa(start+1)+" more above")
	}
	if end := start + budget; end < len(rows) {
		out[len(out)-1] = fg(cFaint, "↓ "+itoa(len(rows)-end+1)+" more below")
	}
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

// viewResume is the overlay that picks a finished dispatcher back up.
//
// Its copy used to promise a resume it did not perform — "resumes session <id>
// with its full context — the branch is recreated from the merge commit" — over
// a key that launched an entirely new session on the same feature name. Both
// halves are now real and each says which one is about to happen: with a
// recorded session the transcript is reopened in the dispatch's own worktree;
// without one there is nothing to reopen, and a fresh dispatch is offered as
// what it is.
func (m model) viewResume(w, h int) string {
	t := m.resumeSelected()
	cw := w - 2*pad
	if cw < 10 {
		cw = w
	}
	where := t.repo
	for _, part := range []string{t.pr, t.at} {
		if part != "" && part != "—" {
			where += " · " + part
		}
	}

	var out []string
	lead, explain := "pick a finished dispatcher back up", "reopens session "+t.session+
		" in its own worktree, with everything it already knows — leave the line empty to just open it"
	if !t.resumable() {
		lead = "dispatch this feature again"
		explain = "no session was recorded for this one, so there is no conversation to reopen — " +
			"this starts a fresh dispatcher on the same feature, and needs a prompt"
	}
	out = append(out, fg(cDim, lead))
	out = append(out, line(t.feature, cw, cWhite, ""))
	out = append(out, fg(cDim, where))
	for _, ln := range productWrap(explain, cw) {
		out = append(out, fg(cFaint, ln))
	}
	out = append(out, "")
	out = append(out, fg(cDim, "originally"))
	for _, ln := range productWrap(t.prompt, cw) {
		out = append(out, fg(cMid, ln))
	}
	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	out = append(out, fg(cAmber, "now ")+fg(cWhite, m.resumeText+"▏"))
	verb := "enter resume"
	if !t.resumable() {
		verb = "enter dispatch"
	}
	out = append(out, fg(cFaint, verb+" · esc cancel"))
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
			m.resumeOpen, m.resumeText, m.resumeAt = false, "", nil
			return m, nil
		}
		if submit {
			t := m.resumeSelected()
			text := strings.TrimSpace(m.resumeText)
			m.resumeOpen, m.resumeText, m.resumeAt = false, "", nil
			// A recorded session is reopened where it ran, and an empty line is a
			// legitimate ask: "just give it back to me". Without one there is no
			// conversation to reopen, so this falls back to a fresh dispatch on
			// the same feature — which does need a prompt, and says so rather
			// than announcing a dispatch it did not make.
			if t.resumable() {
				m.notice = "resuming \"" + t.feature + "\"…"
				return m, resumeCmd(t.id, text)
			}
			if text == "" {
				m.notice = "nothing to dispatch — type what to change"
				return m, nil
			}
			m.notice = "dispatching \"" + t.feature + "\" again…"
			// Same reading as the backlog's enter: this panel asks for a line of
			// text, not for a mode, a model or a fan-out, so the re-dispatch
			// takes the defaults rather than inheriting the finished
			// dispatcher's — the human typed a new brief, and choices made for
			// the last run are not consent for this one.
			return m, launchCmd(m.cfg, t.repo, t.feature, text, dispatchpkg.DefaultMode, dispatchpkg.DefaultModel, false)
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
	case "H":
		m.rightTab, m.historyCursor = "history", 0
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
	case "history":
		items := m.historyFlat()
		sel, has := m.historySelected()
		switch k {
		case "j", "down":
			if len(items) > 0 {
				m.historyCursor = mini(m.historyCursor+1, len(items)-1)
			}
		case "k", "up":
			m.historyCursor = maxi(m.historyCursor-1, 0)
		case "enter":
			if has {
				m.resumeOpen, m.resumeText, m.resumeAt = true, "", historyTarget(sel)
			}
		case "o":
			if !has {
				return m, nil
			}
			if sel.pr == "" {
				m.notice = "\"" + sel.feature + "\" has no pull request"
				return m, nil
			}
			return m, openPRCmd(sel.repo, sel.pr)
		}
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
			m.resumeOpen, m.resumeText, m.resumeAt = true, "", shipTarget(m.shipSelected())
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
