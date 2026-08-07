package cockpit

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const pad = 2 // left/right page gutter, in columns (the design's 24px)

var footerByLens = map[string]string{
	"products":  "j/k move · enter open product · : palette · 1 triage",
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

	used := lipgloss.Height(header) + lipgloss.Height(footer)
	if bars != "" {
		used += lipgloss.Height(bars)
	}
	bodyH := m.height - used
	if bodyH < 1 {
		bodyH = 1
	}

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
	newLabel := ""
	if m.newCount > 0 {
		newLabel = fg(cAmber, "▲ "+itoa(m.newCount)+" new since you looked") + "   "
	}
	right := newLabel +
		fg(cDim, "27 out · 5 products · 57 repos") + "   " +
		fg(cFaint, "week ") + fg(cAmber, "65%") + fg(cFaint, " · resets thu") + "   " +
		fg(cFaint, fitv.cols+" · 21:56")

	return spread(left, right, m.width)
}

func (m model) footerView() string {
	left := fg(cDim, m.footerHelp())
	notice := m.notice
	if m.undo != "" {
		notice += "  ·  u to undo"
	}
	right := fg(cAmber, notice)
	return spread(left, right, m.width)
}

func (m model) footerHelp() string {
	if m.lens != "floor" {
		if h, ok := footerByLens[m.lens]; ok {
			return h
		}
		return "1 triage"
	}
	if m.pane != "detail" {
		return "j/k · → detail · / filter · space mark · y ship · D diff · F follow · u undo · ? keys"
	}
	if m.floorEntryIsHeader() {
		return "j/k through the tickets · enter dispatch one · ← back to the list · : palette"
	}
	return "j/k through the stack · enter open pr · r reply · ← back to the list · : palette"
}

// barsView renders the marks bar and confirm bar that sit above the footer.
func (m model) barsView() string {
	var lines []string
	if len(m.marked) > 0 {
		names := make([]string, 0, len(m.marked))
		for n := range m.marked {
			names = append(names, n)
		}
		left := fg(cWhite, itoa(len(m.marked))+" marked") + "  " + fg(cMid, strings.Join(names, " · "))
		right := fg(cDim, "x kill all · r reply all · M model · esc clear")
		lines = append(lines, spread(left, right, m.width))
	}
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
		return m.viewFloor(w, h)
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
	case m.helpOpen:
		return m.viewHelp(w, h), true
	case m.diffOpen:
		return m.viewDiff(w, h), true
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
