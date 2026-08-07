package cockpit

// usage.go is lens 6 — subscription budget by window, model and product. It is a
// display-only lens (no key handler): a faithful port of the design's USAGE
// panel. The user is on a Claude subscription, so everything here speaks in
// tokens and effort, never dollars.
//
// Layout mirrors the mock: a left pane (~52%) "subscription usage · all seats"
// with one row per rolling window, a projection line and "what would change it"
// advice; a right pane "by model · this week" and "by product · this week".

import (
	"strconv"
	"strings"
)

// usageWindows are the rolling budget windows (USAGE.windows). used is the
// percentage of the window's cap consumed; pace is the burn rate versus a flat
// draw-down (1.0 = on track).
type usageWindow struct {
	label string
	used  int
	note  string
	pace  float64
}

var usageWindows = []usageWindow{
	{label: "today", used: 18, note: "since 09:00 · 27 sessions", pace: 1.2},
	{label: "this week", used: 65, note: "resets thu 09:00 · 141 sessions", pace: 1.4},
	{label: "this month", used: 41, note: "aug · 512 sessions", pace: 0.9},
}

// usageModels is the week's spend split by model (USAGE.models). share is the
// percentage of the week's tokens; avg is the per-session token draw.
type usageModel struct {
	name            string
	share, sessions int
	avg, note       string
}

var usageModels = []usageModel{
	{name: "opus", share: 58, sessions: 9, avg: "4.2M tok/session", note: "blocked, urgent and long-context work"},
	{name: "sonnet", share: 36, sessions: 15, avg: "1.8M tok/session", note: "the default for feature work"},
	{name: "haiku", share: 6, sessions: 3, avg: "0.4M tok/session", note: "pr writing, changelogs, contrast passes"},
}

// usageProjection is the amber forecast line (USAGE.projection).
var usageProjection = "At 1.4× pace you hit the weekly cap thursday 14:00 — nineteen hours before it resets."

// usageAdvice is the "what would change it" list (USAGE.advice): first line amber
// (the headline lever), the rest mid.
type usageAdviceItem struct{ text, color string }

var usageAdvice = []usageAdviceItem{
	{text: "cortiva is 31% of the week on its own · 9 in flight, 4 of them opus", color: cAmber},
	{text: "move the 13 working dispatchers to sonnet and the week lands at 48%", color: cMid},
	{text: "subagents are 41% of all usage — pr-writer and test-runner are cheap on haiku", color: cMid},
}

// viewUsage renders the usage lens: two panes joined by a vertical rule. On a
// narrow terminal (no detail tier) only the left pane shows, at full width.
func (m model) viewUsage(w, h int) string {
	inner := w - 2*pad
	if inner < 1 {
		inner = w
	}

	if !m.fit().showDetail { // narrow — left pane only
		body := vjoin(padLines(m.usageLeft(inner), inner)...)
		return clampLines(gutter(body, pad), h)
	}

	leftW := inner * 52 / 100
	rightW := inner - leftW - 1 // 1 column for the vertical rule
	leftBlock := padBlockTo(vjoin(padLines(m.usageLeft(leftW), leftW)...), h)
	rightBlock := padBlockTo(vjoin(padLines(m.usageRight(rightW), rightW)...), h)
	body := hjoin(leftBlock, vrule(h, cRule), rightBlock)
	return clampLines(gutter(body, pad), h)
}

// usageLeft builds the left pane: windows, projection, advice.
func (m model) usageLeft(w int) []string {
	out := []string{fg(cDim, "subscription usage · all seats")}

	for _, win := range usageWindows {
		out = append(out, "") // padding-top between windows
		col := cGreen
		if win.used > 60 {
			col = cAmber
		}
		paceCol := cMid
		if win.pace > 1.25 {
			paceCol = cRed
		}
		lead := fg(cMid, padTo(win.label, 12, alignLeft)) + "  " +
			fg(col, bar(win.used, 18)) + "  " +
			fg(col, itoa(win.used)+"%")
		pace := fg(paceCol, usagePace(win.pace)+" pace")
		out = append(out, usageFill(w, lead, pace))
		out = append(out, strings.Repeat(" ", 12)+fg(cFaint, truncatePlain(win.note, w-12)))
	}

	// Projection: a bordered forecast line.
	out = append(out, "")
	out = append(out, fg(cRule, strings.Repeat("─", w)))
	for _, l := range usageWrap(usageProjection, w) {
		out = append(out, fg(cAmber, l))
	}

	// What would change it — the levers.
	out = append(out, "")
	out = append(out, fg(cDim, "what would change it"))
	for _, a := range usageAdvice {
		for _, l := range usageWrap(a.text, w) {
			out = append(out, fg(a.color, l))
		}
	}
	return out
}

// usageRight builds the right pane: by model, then by product. Content is inset
// one column so it does not hug the vertical rule.
func (m model) usageRight(w int) []string {
	cw := w - 1
	if cw < 1 {
		cw = w
	}
	ind := func(s string) string { return " " + s }

	out := []string{ind(usageFill(cw,
		fg(cDim, "by model · this week"),
		fg(cFaint, "M changes the selected dispatcher")))}

	for _, md := range usageModels {
		out = append(out, "")
		col := cGreen
		if md.name == "opus" {
			col = cAmber
		}
		lead := fg(cWhite, padTo(md.name, 9, alignLeft)) + "  " +
			fg(col, bar(md.share, 16)) + "  " +
			fg(cWhite, itoa(md.share)+"%")
		right := fg(cFaint, itoa(md.sessions)+" sessions")
		out = append(out, ind(usageFill(cw, lead, right)))
		sub := strings.Repeat(" ", 9) + fg(cFaint, md.avg) + "  " + fg(cFaint, md.note)
		out = append(out, ind(truncateAnsi(sub, cw)))
	}

	out = append(out, "")
	out = append(out, ind(fg(cDim, "by product · this week")))
	for _, name := range productOrder {
		st := productStats[name]
		if st.budget <= 0 {
			continue
		}
		col := cGreen
		if st.budget > 25 {
			col = cAmber
		}
		paceCol := cFaint
		if st.pace > 1.25 {
			paceCol = cRed
		}
		lead := fg(cMid, padTo(name, 12, alignLeft)) + "  " +
			fg(col, bar(int(float64(st.budget)*2.5), 14)) + "  " +
			fg(cMid, padTo(itoa(st.budget)+"%", 5, alignRight))
		pace := fg(paceCol, usagePaceShort(st.pace))
		out = append(out, ind(usageFill(cw, lead, pace)))
	}
	return out
}

// ---- usage-local helpers ----------------------------------------------------

// usageFill lays left flush-left and right flush-right within width w, the
// terminal analogue of the design's margin-left:auto.
func usageFill(w int, left, right string) string {
	gap := w - dispWidth(left) - dispWidth(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// usagePace formats a burn rate like "1.4×".
func usagePace(p float64) string { return strconv.FormatFloat(p, 'g', -1, 64) + "×" }

// usagePaceShort is the compact per-product form (same as usagePace today, kept
// separate so the two columns can diverge without churn).
func usagePaceShort(p float64) string { return usagePace(p) }

// truncatePlain clips uncoloured text to w columns with an ellipsis.
func truncatePlain(s string, w int) string { return truncate(s, w) }

// usageWrap word-wraps plain text to lines of at most w columns.
func usageWrap(s string, w int) []string {
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
	return lines
}

// padLines pads each line to exactly w columns (truncating any overflow) so
// side-by-side panes keep a straight rule.
func padLines(lines []string, w int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if dispWidth(l) > w {
			out[i] = truncateAnsi(l, w)
		} else {
			out[i] = padTo(l, w, alignLeft)
		}
	}
	return out
}
