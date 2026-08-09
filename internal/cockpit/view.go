package cockpit

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const pad = 2 // left/right page gutter, in columns (the design's 24px)

var footerByLens = map[string]string{
	"products":  "j/k move · enter open product · a assign repos · n new product · 1 triage",
	"product":   "R review · T team · S shipped · j/k move · enter open · d dispatch a reviewer · 1 triage",
	"queue":     "a add · e edit · x drop · ctrl+d dispatch all · 1 triage",
	"backlog":   "j/k move · space pick · enter dispatch · ctrl+d dispatch picked · s source · 1 triage",
	"usage":     "budget by window, model and product · 8 velocity · 1 triage",
	"decisions": "j/k records · J/K repo · → body · a accept · s supersede · e tool · o open",
	"velocity":  "velocity is what reached production, not what you set in motion · 3 product · 1 triage",
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}
	header := m.headerView()
	bars := m.barsView()
	footer := m.footerView()
	bodyH := m.bodyHeight()

	body, isOverlay := m.overlayView(m.width, bodyH)
	if !isOverlay {
		body = m.lensBody(m.width, bodyH)
	}
	body = padBlockTo(body, bodyH)

	parts := []string{header, body}
	if bars != "" {
		parts = append(parts, bars)
	}
	parts = append(parts, footer)
	return strings.Join(parts, "\n")
}

// bodyHeight is the height a lens is rendered into: the terminal less the
// chrome. It is a method rather than a local in View because a key handler
// sometimes has to size what the view is about to lay out — the triage lens's
// scroll panes clamp against it (see cqScrollMax).
func (m model) bodyHeight() int {
	used := lipgloss.Height(m.headerView()) + lipgloss.Height(m.footerView())
	if bars := m.barsView(); bars != "" {
		used += lipgloss.Height(bars)
	}
	return maxi(1, m.height-used)
}

// headerView is the top lens bar plus, on wide-enough terminals, the summary.
func (m model) headerView() string {
	dq := func(n string) string { return fg(cFaint, n+" ") }
	lensLabel := func(id, label string) string {
		c := cDim
		if m.lens == id {
			c = cWhite
		}
		return fg(c, label)
	}
	productLabel := "product"
	if len(products) > 0 {
		productLabel = products[clampCursor(m.productCursor, len(products))].name
	}

	left := strings.Join([]string{
		fg(cMid, "⚡ dispatch"),
		dq("1") + lensLabel("floor", "triage"),
		dq("2") + lensLabel("products", "products"),
		dq("3") + lensLabel("product", productLabel),
		dq("4") + lensLabel("queue", "queue"),
		dq("5") + lensLabel("backlog", "backlog"),
		dq("6") + lensLabel("usage", "usage"),
		dq("7") + lensLabel("decisions", "decisions"),
		dq("8") + lensLabel("velocity", "velocity"),
	}, "  ")

	fitv := m.fit()
	if !fitv.showSummary {
		return gutter(left, pad)
	}
	// The design hardcodes this strip ("27 out · 5 products · 57 repos",
	// "week 65%", "21:56") because it is a static mock. Every figure here is
	// counted or omitted instead: a header that states a number the user cannot
	// act on is worse than a header that says nothing.
	parts := []string{fg(cDim, m.portfolioLine())}
	if w := weekWindow(); w != nil {
		parts = append(parts, fg(cFaint, "week ")+fg(usageBandColor(w.pace), itoa(w.used)+"%")+fg(cFaint, " · resets "+usageResetDay()))
	}
	parts = append(parts, fg(cFaint, fitv.cols+" · "+nowHHMM()))

	return spread(left, strings.Join(parts, "   "), m.width)
}

func (m model) footerView() string {
	left := fg(cDim, m.footerHelp())
	notice := m.notice
	if m.undo != "" {
		notice += "  ·  u to undo"
	}
	right := ""
	if notice != "" {
		right = fg(cAmber, notice)
	}

	// The build version shares the bottom-right corner with the notice. They
	// compete for the same space, so the version yields first — a notice is
	// about what just happened, the version is ambient — and yields in stages,
	// shedding the upgrade command before the version number itself.
	inner := m.width - 2*pad
	for _, v := range m.versionForms() {
		cand := v
		if right != "" {
			cand = right + fg(cFaint, "  ·  ") + v
		}
		if dispWidth(left)+2+dispWidth(cand) <= inner {
			right = cand
			break
		}
	}
	return spread(left, right, m.width)
}

func (m model) footerHelp() string {
	// The triage lens's keys change with its mode, so it writes its own.
	if m.lens == "floor" {
		return m.cqFooterHelp()
	}
	if m.lens == "products" && m.clOpen {
		if m.clNaming {
			return "type the name · enter creates it · esc cancels"
		}
		return "j/k move · tab pane · space mark · enter assign · u unassign · n new · a done"
	}
	if h, ok := footerByLens[m.lens]; ok {
		return h
	}
	return "1 triage"
}

// barsView renders the confirm bar that sits above the footer.
func (m model) barsView() string {
	var lines []string
	if m.confirm != nil {
		left := fg(cAmber, "confirm") + "  " + fg(cWhite, m.confirm.label)
		right := fg(cWhite, "y") + fg(cDim, " do it · ") + fg(cWhite, "n") + fg(cDim, " cancel")
		lines = append(lines, spread(left, right, m.width))
	}
	return strings.Join(lines, "\n")
}

func (m model) lensBody(w, h int) string {
	switch m.lens {
	case "floor":
		return m.viewCQ(w, h)
	case "products":
		return m.viewProducts(w, h)
	case "product":
		return m.viewProduct(w, h)
	case "queue":
		return m.viewQueue(w, h)
	case "backlog":
		return m.viewBacklog(w, h)
	case "usage":
		return m.viewUsage(w, h)
	case "decisions":
		return m.viewDecisions(w, h)
	case "velocity":
		return m.viewVelocity(w, h)
	}
	return ""
}

// overlayView returns the active full-screen modal, if any.
func (m model) overlayView(w, h int) (string, bool) {
	switch {
	case m.settings != nil:
		return m.viewSettings(w, h), true
	case m.dispatchForm != nil:
		return m.viewDispatchForm(w, h), true
	case m.helpOpen:
		return m.viewHelp(w, h), true
	case m.reviewOpen:
		return m.viewReview(w, h), true
	case m.resumeOpen:
		return m.viewResume(w, h), true
	case m.paletteOpen:
		return m.viewPalette(w, h), true
	}
	return "", false
}

// spread places left and right on one line width w, with the page gutter on
// both edges and the gap between them filled. Both sides are already
// colour-rendered strings; it truncates the right side, then the left, to fit.
func spread(left, right string, w int) string {
	inner := w - 2*pad
	if inner < 1 {
		inner = w
	}
	lw, rw := dispWidth(left), dispWidth(right)
	if lw+1+rw > inner {
		// Not enough room for both — drop the right side, clip the left.
		return strings.Repeat(" ", pad) + truncateAnsi(left, inner)
	}
	gap := inner - lw - rw
	return strings.Repeat(" ", pad) + left + strings.Repeat(" ", gap) + right + strings.Repeat(" ", pad)
}

// portfolioLine is the header's "N out · M products · R repos" summary, counted
// from what is actually loaded. Clauses whose count is zero are dropped rather
// than printed, so an empty portfolio reads as empty instead of as zeroes.
func (m model) portfolioLine() string {
	out := 0
	for _, x := range dispatches {
		if x.state != "live" && x.state != "closed" {
			out++
		}
	}
	var parts []string
	if out > 0 {
		parts = append(parts, itoa(out)+" out")
	}
	// "unassigned" is the bucket collectProducts folds every unmapped repo into,
	// not a product. Counting it made a portfolio with nothing grouped claim
	// "1 product" — the one reading a user is most likely to check against, on
	// the install where it is least likely to be true.
	prods := 0
	for _, p := range products {
		if p.name != clUnassigned {
			prods++
		}
	}
	if prods > 0 {
		parts = append(parts, itoa(prods)+" "+plural(prods, "product", "products"))
	}
	if n := m.repoCount(); n > 0 {
		parts = append(parts, itoa(n)+" "+plural(n, "repo", "repos"))
	}
	if len(parts) == 0 {
		return "nothing dispatched yet"
	}
	return strings.Join(parts, " · ")
}

// weekWindow returns the weekly usage window, or nil when usage has not been
// measured yet — the header then omits the clause entirely.
func weekWindow() *usageWindow {
	for i := range usageWindows {
		if strings.Contains(strings.ToLower(usageWindows[i].label), "week") {
			return &usageWindows[i]
		}
	}
	return nil
}

// usageBandColor colours the weekly figure by pace: over budget is amber, well
// over is red.
func usageBandColor(pace float64) string {
	switch {
	case pace >= 1.5:
		return cRed
	case pace >= 1.0:
		return cAmber
	}
	return cMid
}

// usageResetDay names the weekday the weekly window rolls over on.
func usageResetDay() string {
	return strings.ToLower(time.Now().Add(7 * 24 * time.Hour).Format("Mon"))
}

// nowHHMM is the wall clock in the header.
func nowHHMM() string { return time.Now().Format("15:04") }

// plural picks the singular or plural form for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
