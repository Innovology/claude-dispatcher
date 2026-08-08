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

// queueRight builds the capacity pane content lines to inner width iw. Every
// figure is counted from the live floor: how many dispatchers are out now, how
// many want you, and what the queued batch would add.
func (m model) queueRight(iw int) []string {
	out, want := 0, 0
	for _, x := range dispatches {
		if x.state == "live" || x.state == "closed" {
			continue
		}
		out++
		if x.urgent {
			want++
		}
	}

	var L []string
	L = append(L, line("capacity", iw, cDim, ""))
	L = append(L, line(itoa(out)+" out, "+itoa(want)+" want you", iw, cWhite, ""))
	if n := len(queueItems); n > 0 {
		for _, nl := range queueWrap("after this batch: "+itoa(out+n)+" out. Past 24 you stop reading turns — that is measured from your own reply latency, not a guess.", iw, 4) {
			L = append(L, line(nl, iw, cMid, ""))
		}
	}

	L = append(L, "")
	L = append(L, line("recently shipped, reusable", iw, cDim, ""))
	if recent := m.recentlyShipped(3); len(recent) > 0 {
		for _, r := range recent {
			L = append(L, padTo(fg(cMid, r.feature)+fg(cFaint, " · "+r.repo+" · "+r.age), iw, alignLeft))
		}
	} else {
		L = append(L, line("nothing shipped yet", iw, cFaint, ""))
	}

	L = append(L, "")
	L = append(L, line("c clones one into another repo with its prompt", iw, cDim, ""))
	return L
}

// recentlyShipped returns up to n features that have reached live, most recent
// first, for cloning into another repo.
func (m model) recentlyShipped(n int) []dispatch {
	var out []dispatch
	for _, x := range dispatches {
		if x.state != "live" && x.state != "closed" {
			continue
		}
		out = append(out, x)
		if len(out) == n {
			break
		}
	}
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
