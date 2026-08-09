package cockpit

// cluster_view.go renders the assignment editor: repos on the left, products on
// the right, and the inline naming prompt. Layout follows the design's
// clOpen/clNaming sections — a 30% right pane, a marked column, and the
// explanation pinned to the bottom of the product list.

import "strings"

// viewCluster is the products lens while the editor is open.
func (m model) viewCluster(w, h int) string {
	rightW := w * 30 / 100
	leftW := w - rightW - 1
	leftInner := maxi(leftW-2*pad, 1)
	rightInner := maxi(rightW-2*pad, 1)

	left := productsPane(m.clLeft(leftInner, h), leftW)
	right := productsPane(m.clRight(rightInner, h), rightW)
	hh := mini(maxi(strings.Count(left, "\n"), strings.Count(right, "\n"))+1, maxi(h, 1))
	return clampLines(hjoin(padBlockTo(left, hh), vrule(hh, cRule), padBlockTo(right, hh)), h)
}

// clLeft is the repo table: mark, name, forge, current product, out, last.
//
// h is the body height, and the rows scroll within it. The list used to render
// every repo and let clampLines cut the overflow, which meant that on any
// portfolio taller than the terminal the cursor walked off the bottom and
// stayed there: j kept working, the selection kept moving, and none of it was
// visible. A repo list you cannot see the cursor in is not one you can assign
// from.
func (m model) clLeft(cw, h int) []string {
	rows := m.clRepos()
	sel := clampCursor(m.clRepo, len(rows))

	marked := 0
	for _, on := range m.clMarked {
		if on {
			marked++
		}
	}
	markedLine := "space marks · enter moves them"
	if marked > 0 {
		word := "repo"
		if marked != 1 {
			word = "repos"
		}
		markedLine = itoa(marked) + " " + word + " marked · enter moves them"
	}

	out := []string{
		spread(fg(cDim, m.clPaneLabel()), fg(cFaint, markedLine), cw+2*pad),
		"",
		row(cw, "",
			c("", 3, ""),
			flexc("REPO", cFaint),
			c("FORGE", 6, cFaint),
			flexc("PRODUCT", cFaint),
			cr("OUT", 8, cFaint),
			cr("LAST", 8, cFaint),
		),
	}
	if len(rows) == 0 {
		return append(out, "", fg(cFaint, "no repos found — check your scan roots with ,"))
	}
	// Leave a line for the "showing x of y" footer so it cannot itself be the
	// row that gets clipped.
	start, end := window(sel, len(rows), maxi(h-len(out)-1, 1))
	for i := start; i < end; i++ {
		r := rows[i]
		bg, mark, nameColor := cTransparent, " ", cFg
		if m.clMarked[r.name] {
			mark = "◆"
		}
		if i == sel && m.clPane == "repos" {
			bg, nameColor = cSel, cWhite
			if mark == " " {
				mark = "▸"
			}
		}
		prod, prodColor := r.product, cMid
		if prod == "" {
			prod, prodColor = "—", cFaint
		}
		out = append(out, row(cw, bg,
			c(mark, 3, cAmber),
			flexc(r.name, nameColor),
			c(r.forge, 6, cFaint),
			flexc(prod, prodColor),
			cr(itoa(r.out), 8, cFaint),
			cr(r.last, 8, cFaint),
		))
	}
	// Only when the list is actually scrolling: on a portfolio that fits, a
	// position counter is noise.
	if end-start < len(rows) {
		out = append(out, fg(cFaint, itoa(sel+1)+" of "+itoa(len(rows))))
	}
	return out
}

// clPaneLabel names which pane has the keyboard.
func (m model) clPaneLabel() string {
	if m.clPane == "products" {
		return "products · tab to repos"
	}
	return "repos · tab to products"
}

// clRight is the product list, the new-product affordance, the naming prompt
// when it is up, and the explanation pinned to the bottom.
func (m model) clRight(cw, h int) []string {
	prods := m.clProducts()
	counts := map[string]int{}
	for _, r := range m.clRepos() {
		if r.product != "" {
			counts[r.product]++
		}
	}

	out := []string{fg(cDim, "products"), ""}
	if len(prods) == 0 {
		out = append(out, fg(cFaint, "none yet — n names one"))
	}
	sel := clampCursor(m.clProd, len(prods))
	for i, p := range prods {
		bg, mark := cTransparent, " "
		if i == sel && m.clPane == "products" {
			bg, mark = cSel, "▸"
		}
		n := counts[p]
		word := "repo"
		if n != 1 {
			word = "repos"
		}
		out = append(out,
			row(cw, bg, c(mark, 3, cMid), flexc(p, cFg)),
			row(cw, bg, c("", 3, ""), flexc(itoa(n)+" "+word, cFaint)),
		)
	}

	out = append(out, "", fg(cRule, strings.Repeat("─", cw)))
	out = append(out, row(cw, "", c("n", 3, cFg), flexc("new product…", cDim)))

	if m.clNaming {
		out = append(out, "", fg(cDim, "new product"))
		out = append(out, fg(cWhite, m.clNewName)+paint(cFg, cFg, " "))
		hint := "enter creates it and moves the marked repos in"
		if len(m.clTargets()) == 1 {
			hint = "enter creates it and moves this repo in"
		}
		out = append(out, "", fg(cFaint, hint))
	}

	// Pin the explanation to the bottom, as the design does with flex:1.
	tail := []string{}
	for _, ln := range productsWrap("A product is the thing you ship. Repos are where it lives. Assign them once and every other screen groups by product.", cw) {
		tail = append(tail, fg(cFaint, ln))
	}
	if pad := h - len(out) - len(tail) - 1; pad > 0 {
		out = append(out, make([]string, pad)...)
	}
	return append(out, tail...)
}
