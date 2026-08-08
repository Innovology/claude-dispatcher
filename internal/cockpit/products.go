package cockpit

// products.go is the PRODUCTS portfolio lens (lens 2). A product is many repos;
// the left pane is the portfolio table (one row per product) plus the stale list
// and the "where the factory is stuck" call-outs, and the right pane focuses the
// product under the cursor — its note, its repos, and its in-flight features.
//
// Faithful port of v2_template.html lines 316-386 and renderVals `products`.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// productsFocusItem is one in-flight feature in the right pane's focus panel.
type productsFocusItem struct {
	feature, repo, age, glyph, color string
}

// productsFocusFeatures mirrors focusFeatures: DISPATCHES (already tagged with a
// product + state) concatenated with WORKING (tagged working, product derived
// from the repo), filtered to the selected product, first eight only.
func productsFocusFeatures(name string) []productsFocusItem {
	var out []productsFocusItem
	for _, d := range dispatches {
		if d.product != name {
			continue
		}
		sm := stateMetaBy[d.state]
		out = append(out, productsFocusItem{d.feature, d.repo, d.age, sm.glyph, sm.color})
	}
	for _, w := range working {
		if repoProduct(w.repo) != name {
			continue
		}
		sm := stateMetaBy["working"]
		out = append(out, productsFocusItem{w.feature, w.repo, w.age, sm.glyph, sm.color})
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// productsWrap word-wraps s to lines of at most w columns.
func productsWrap(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := ""
	for _, word := range words {
		switch {
		case cur == "":
			cur = word
		case dispWidth(cur)+1+dispWidth(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// productsPane gutters each content line by the page pad, then pads or clips it
// to exactly w columns so side-by-side panes align and never overflow.
func productsPane(lines []string, w int) string {
	lead := strings.Repeat(" ", pad)
	out := make([]string, len(lines))
	for i, ln := range lines {
		s := lead + ln
		switch d := dispWidth(s); {
		case d > w:
			s = truncateAnsi(s, w)
		case d < w:
			s += strings.Repeat(" ", w-d)
		}
		out[i] = s
	}
	return strings.Join(out, "\n")
}

func (m model) viewProducts(w, h int) string {
	if m.clOpen {
		return m.viewCluster(w, h)
	}
	fitv := m.fit()
	pc := clampCursor(m.productCursor, len(products))

	// Pane geometry: right focus panel is flex:0 0 30%; the left table fills the
	// rest with a one-column rule between them. Narrow terminals show the table
	// only (fitNarrow disables the detail pane).
	// With nothing grouped there is no product to focus, so the onboarding gets
	// the full width instead of sitting beside a panel about "unassigned".
	twoPane := fitv.showDetail && m.hasRealProducts()
	leftW := w
	rightW := 0
	if twoPane {
		rightW = w * 30 / 100
		leftW = w - rightW - 1 // 1 col for the vrule
	}
	leftInner := leftW - 2*pad
	if leftInner < 1 {
		leftInner = 1
	}

	left := m.productsLeft(leftInner, pc)
	leftBlock := productsPane(left, leftW)

	if !twoPane {
		return clampLines(leftBlock, h)
	}

	rightInner := rightW - 2*pad
	if rightInner < 1 {
		rightInner = 1
	}
	right := m.productsRight(rightInner, pc)
	rightBlock := productsPane(right, rightW)

	hh := maxi(strings.Count(leftBlock, "\n"), strings.Count(rightBlock, "\n")) + 1
	if hh > h && h > 0 {
		hh = h
	}
	out := hjoin(padBlockTo(leftBlock, hh), vrule(hh, cRule), padBlockTo(rightBlock, hh))
	return clampLines(out, h)
}

// productsLeft builds the portfolio table, the stale list and the stuck lines.
func (m model) productsLeft(cw, pc int) []string {
	var out []string

	loose, looseColor := m.looseLine()
	out = append(out, spread(fg(cDim, "portfolio · a product is many repos"), fg(looseColor, loose), cw+2*pad))

	// With nothing mapped, the table below would be a single "unassigned" row
	// explaining nothing. Say what a product is for and how to make one.
	if !m.hasRealProducts() {
		return append(out, m.productsOnboarding(cw)...)
	}

	out = append(out, row(cw, "",
		c("a", 2, cFg), flexc("assign repos to products", cDim),
		c("n", 2, cFg), flexc("new product", cDim),
	))
	out = append(out, "")
	out = append(out, row(cw, "",
		c("", 2, ""),
		flexc("PRODUCT", cFaint),
		c("REPOS", 10, cFaint),
		c("FORGE", 7, cFaint),
		cr("IN FLIGHT", 11, cFaint),
		cr("WANTS YOU", 12, cFaint),
		cr("LIVE TODAY", 12, cFaint),
		cr("7d SHIPPED", 16, cFaint),
		cr("LEAD TIME", 12, cFaint),
	))

	for n, p := range products {
		on := n == pc
		bg := ""
		marker := ""
		nameColor := cFg
		if on {
			bg = cSel
			marker = "▸"
			nameColor = cWhite
		}
		needsLabel := "—"
		if p.needs+p.review > 0 {
			needsLabel = itoa(p.needs + p.review)
		}
		needsColor := cDim
		if p.needs > 0 {
			needsColor = cAmber
		}
		out = append(out, row(cw, bg,
			c(marker, 2, cMid),
			flexc(p.name, nameColor),
			c(p.repos, 10, cDim),
			c(p.forge, 7, cDim),
			cr(itoa(p.inflight), 11, cMid),
			cr(needsLabel, 12, needsColor),
			cr(itoa(p.live), 12, cWhite),
			cr(p.spark, 16, cGreen),
			cr(p.lead, 12, cMid),
		))
	}

	out = append(out, "")
	// "N of M repos" is counted, never quoted: a fixed pair here read as a fact
	// about the user's portfolio and was wrong for everyone but the mock.
	staleCount := itoa(len(staleRepos)) + " of " + itoa(m.repoCount()) + " repos"
	out = append(out, row(cw, "",
		flexc("stale · nothing dispatched, nothing merged", cDim),
		cr(staleCount, 15, cFaint),
	))
	for _, s := range staleRepos {
		daysColor := cAmber
		if s.days > 30 {
			daysColor = cRed
		}
		out = append(out, row(cw, "",
			c(s.repo, 24, cFg),
			c(s.product, 13, cDim),
			cr(itoa(s.days)+"d", 9, daysColor),
			flexc("   "+s.note, cFaint),
		))
	}

	out = append(out, "")
	out = append(out, line("where the factory is stuck", cw, cDim, ""))
	if stuck := m.stuckLines(); len(stuck) > 0 {
		out = append(out, stuck...)
	} else {
		out = append(out, fg(cFaint, "nothing blocked"))
	}

	return out
}

// stuckLines reports what is actually blocking the factory, worst first: the
// dispatchers sitting in blocked, then needs-you. Derived from the live floor,
// so it is empty when nothing is blocked rather than asserting a blockage.
func (m model) stuckLines() []string {
	var out []string
	for _, st := range []string{"blocked", "needs"} {
		for _, x := range dispatches {
			if x.state != st {
				continue
			}
			meta := stateMetaBy[x.state]
			why := x.why
			if why == "" {
				why = meta.label
			}
			where := x.repo
			if x.product != "" && x.product != "—" {
				where = x.product + " · " + x.repo
			}
			out = append(out, fg(meta.color, meta.glyph)+fg(cMid, " "+where+" · "+why+" — "+x.age))
			if len(out) == 3 {
				return out
			}
		}
	}
	return out
}

// repoCount is how many repos the portfolio covers, counted from the product
// map so it always matches what the lens is showing.
func (m model) repoCount() int {
	seen := map[string]bool{}
	for _, rs := range reposByProduct {
		for _, r := range rs {
			seen[r.name] = true
		}
	}
	for _, s := range staleRepos {
		seen[s.repo] = true
	}
	return len(seen)
}

// productsRight builds the focus panel for the product under the cursor.
func (m model) productsRight(cw, pc int) []string {
	if pc < 0 || pc >= len(products) {
		return []string{fg(cFaint, "no products configured"), "", fg(cFaint, "map repos to products in config.toml")}
	}
	name := products[pc].name
	var out []string

	out = append(out, line(name, cw, cWhite, ""))
	for _, ln := range productsWrap(productNote[name], cw) {
		out = append(out, line(ln, cw, cMid, ""))
	}

	out = append(out, "")
	out = append(out, line("repos", cw, cDim, ""))
	for _, r := range reposByProduct[name] {
		out = append(out, row(cw, "",
			flexc(r.name, cFg),
			c(r.forge, 5, cDim),
			cr(itoa(r.out)+" out", 7, cDim),
			cr(r.ci, 22, r.ciColor),
		))
	}

	out = append(out, "")
	out = append(out, line("in flight", cw, cDim, ""))
	for _, f := range productsFocusFeatures(name) {
		out = append(out, row(cw, "",
			c(f.glyph, 2, f.color),
			flexc(f.feature, cMid),
			c(f.repo, 17, cDim),
			cr(f.age, 5, cDim),
		))
	}

	return out
}

func (m model) updateProducts(k string) (model, tea.Cmd) {
	if m.clOpen {
		mm, cmd, _ := m.updateCluster(k)
		return mm, cmd
	}
	switch k {
	case "a", "n":
		// Assigning repos to products is the only way to create the grouping
		// every other lens reads, so it lives one key away from the portfolio.
		m.clOpen, m.clPane, m.clRepo, m.clMarked = true, "repos", 0, map[string]bool{}
		m.clNaming, m.clNewName = k == "n", ""
		return m, nil
	case "j", "down":
		if m.productCursor < len(products)-1 {
			m.productCursor++
		}
	case "k", "up":
		if m.productCursor > 0 {
			m.productCursor--
		}
	case "enter":
		if len(products) == 0 {
			return m, nil // nothing discovered yet — there is no product to open
		}
		m.lens = "product"
		m.shipCursor = 0
		m.notice = "opened " + products[clampCursor(m.productCursor, len(products))].name
	}
	return m, nil
}

// hasRealProducts reports whether any product other than the "unassigned"
// display bucket exists. A portfolio of one unassigned bucket is not a
// portfolio, and the lens says so rather than tabulating it.
func (m model) hasRealProducts() bool {
	for _, p := range products {
		if p.name != clUnassigned {
			return true
		}
	}
	return false
}

// looseLine counts repos in no product. It is the whole reason the assignment
// editor exists, so it sits in the header and turns amber once it is the
// majority of the portfolio.
func (m model) looseLine() (string, string) {
	loose, total := 0, 0
	for prod, refs := range reposByProduct {
		total += len(refs)
		if prod == clUnassigned {
			loose += len(refs)
		}
	}
	if total == 0 {
		return "", cFaint
	}
	if loose == 0 {
		return "every repo assigned", cFaint
	}
	s := itoa(loose) + " of " + itoa(total) + " repos in no product"
	if loose*2 > total {
		return s, cAmber
	}
	return s, cFaint
}

// productsOnboarding is the empty-portfolio state: what a product is, why the
// cockpit needs one, and the two keys that make one — followed by the repos
// waiting to be grouped.
func (m model) productsOnboarding(cw int) []string {
	var out []string
	out = append(out, "", fg(cFg, "no products yet"))
	for _, ln := range productsWrap("A product is the thing you ship — usually several repos. This cockpit groups everything by it: which blockers come first, what shipped, whether you are going fast.", mini(cw, 86)) {
		out = append(out, fg(cMid, ln))
	}

	rows := m.clRepos()
	out = append(out, fg(cDim, itoa(len(rows))+" "+plural(len(rows), "repo", "repos")+" found, none grouped"))
	out = append(out, "")
	out = append(out, row(cw, "",
		c("n", 2, cFg), flexc("name a product, then move repos in", cDim),
		c("a", 2, cFg), flexc("open the repo list and assign", cDim),
	))
	if s := productsSuggestion(rows); s != "" {
		out = append(out, "", fg(cFaint, s))
	}

	out = append(out, "")
	out = append(out, row(cw, "",
		flexc("REPO", cFaint), c("FORGE", 7, cFaint),
		cr("OUT", 9, cFaint), cr("LAST", 10, cFaint),
	))
	for i, r := range rows {
		if i >= 8 {
			out = append(out, fg(cFaint, "…and "+itoa(len(rows)-8)+" more"))
			break
		}
		out = append(out, row(cw, "",
			flexc(r.name, cMid), c(r.forge, 7, cFaint),
			cr(itoa(r.out), 9, cDim), cr(r.last, 10, cFaint),
		))
	}
	return out
}

// productsSuggestion offers the grouping the repo names already imply, so the
// first product is one keypress of thought rather than a blank page. It only
// speaks when a prefix covers more than one repo.
func productsSuggestion(rows []clRepoRow) string {
	byPrefix := map[string]int{}
	for _, r := range rows {
		if i := strings.IndexAny(r.name, "-_"); i > 0 {
			byPrefix[r.name[:i]]++
		}
	}
	best, n := "", 1
	for p, c := range byPrefix {
		if c > n || (c == n && p < best) {
			best, n = p, c
		}
	}
	if best == "" {
		return ""
	}
	return "Your repo names suggest a start: " + itoa(n) + " of them begin \"" + best + "\"."
}
