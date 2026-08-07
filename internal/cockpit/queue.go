package cockpit

import "strings"

// queue.go is lens 4: the drafted batch of dispatches waiting to go out, beside
// a capacity read-out. It is display-only — the batch is composed elsewhere and
// this lens just shows what is queued and whether there is room for it. Mirrors
// QUEUE and the isQueue block of the design.

// queueItem is one drafted dispatch: a feature/repo pair with a readiness
// status (its own colour), a dark left-edge colour, and the prompt it will run.
type queueItem struct {
	feature, repo, status, color, edge, prompt string
}

// queueItems is the drafted batch, verbatim from the design's QUEUE.
var queueItems = []queueItem{
	{feature: "webhook retry ui", repo: "cortiva-hq", status: "ready", color: cGreen, edge: "#2f6b41", prompt: "surface failed stripe webhooks in the admin with a one-click retry. reuse the audit log table."},
	{feature: "ado deploy detect", repo: "claude-dispatcher", status: "ready", color: cGreen, edge: "#2f6b41", prompt: "treat a successful azure pipelines release run as the live signal, same as gh deploy workflows."},
	{feature: "invoice vat lines", repo: "nw-billing", status: "needs prompt", color: cAmber, edge: "#6b4f1e", prompt: "(no prompt yet — press e)"},
}

// viewQueue renders the two-pane queue lens: the drafted batch on the left
// (~58%) and the capacity read-out on the right. On a narrow terminal only the
// left pane is shown.
func (m model) viewQueue(w, h int) string {
	narrow := !m.fit().showDetail

	if narrow {
		leftW := w
		left := gutter(vjoin(m.queueLeft(leftW-pad)...), pad)
		return clampLines(left, h)
	}

	leftW := w * 58 / 100
	rightW := w - leftW - 1 // 1 col for the vrule

	left := gutter(vjoin(m.queueLeft(leftW-pad)...), pad)
	right := gutter(vjoin(m.queueRight(rightW-pad)...), pad)

	out := hjoin(padBlockTo(left, h), vrule(h, cRule), padBlockTo(right, h))
	return clampLines(out, h)
}

// queueLeft builds the drafted-batch pane content lines to inner width iw.
func (m model) queueLeft(iw int) []string {
	var out []string

	// Header: "queue · N drafted" with the batch hint on the right.
	out = append(out, row(iw, "",
		flexc("queue · "+itoa(len(queueItems))+" drafted", cDim),
		cr("ctrl+d dispatches the batch", 26, cFaint),
	))

	// Each drafted item: a coloured left edge, feature/repo/status, then prompt.
	inner := iw - 3 // edge glyph (1) + 2ch padding-left
	for _, q := range queueItems {
		edge := fg(q.edge, "▌") + "  "
		out = append(out, "") // margin above each item
		out = append(out, edge+row(inner, "",
			flexc(q.feature, cWhite),
			c(q.repo+"  ", dispWidth(q.repo)+2, cDim),
			cr(q.status, 15, q.color),
		))
		for _, pl := range queueWrap(q.prompt, inner, 2) {
			out = append(out, edge+line(pl, inner, cMid, ""))
		}
	}

	// Batch actions and the mechanics footnote.
	out = append(out, "", "")
	out = append(out, line("a add · e edit prompt · x drop · ctrl+d dispatch all "+itoa(len(queueItems)), iw, cDim, ""))
	out = append(out, line("branches feature/<slug> · one tmux session each · hook reports back", iw, cFaint, ""))
	return out
}

// queueRight builds the capacity pane content lines to inner width iw. The copy
// is static, matching the design.
func (m model) queueRight(iw int) []string {
	var out []string
	out = append(out, line("capacity", iw, cDim, ""))
	out = append(out, line("27 out, 6 want you", iw, cWhite, ""))
	for _, nl := range queueWrap("after this batch: 30 out. Past 24 you stop reading turns — that is measured from your own reply latency, not a guess.", iw, 4) {
		out = append(out, line(nl, iw, cMid, ""))
	}

	out = append(out, "")
	out = append(out, line("recently shipped, reusable", iw, cDim, ""))
	for _, r := range []struct{ name, meta string }{
		{"hook backoff", " · claude-dispatcher · 4h"},
		{"pdf export", " · cortiva-hq · 3h"},
		{"wind arrows", " · altsports-web · 2h"},
	} {
		out = append(out, padTo(fg(cMid, r.name)+fg(cFaint, r.meta), iw, alignLeft))
	}

	out = append(out, "")
	out = append(out, line("c clones one into another repo with its prompt", iw, cDim, ""))
	return out
}

// queueWrap greedily word-wraps s to width w over at most maxLines lines, with
// an ellipsis on the last line when the text overruns. It mirrors the design's
// text-wrap:pretty / max-height clamp.
func queueWrap(s string, w, maxLines int) []string {
	if w <= 0 {
		return []string{""}
	}
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, word := range words {
		cand := word
		if cur != "" {
			cand = cur + " " + word
		}
		if dispWidth(cand) <= w {
			cur = cand
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		cur = word
		if len(lines) == maxLines-1 {
			break
		}
	}
	if cur != "" && len(lines) < maxLines {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	// If content remains beyond maxLines, ellipsize the last shown line.
	if dispWidth(strings.Join(lines, " ")) < dispWidth(s) {
		last := lines[len(lines)-1]
		lines[len(lines)-1] = truncate(last+" …", w)
	}
	return lines
}
