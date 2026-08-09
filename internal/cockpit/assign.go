package cockpit

// assign.go is the product creation flow — the design's `a` (assign repos to
// products) and `n` (new product) on the products lens, which are one overlay
// rather than two: naming a product without moving repos into it would create
// something the portfolio cannot draw, so `n` opens the same repo list with the
// name prompt already up, and enter creates the product AND fills it in one act.
//
// The source of truth is config.toml's [products] table, so every act writes it
// through config.Save immediately (the settings editor's contract). The next
// snapshot re-derives everything from that file; the overlay also updates
// repoInventory in place so the screen answers now rather than after a refresh
// that talks to git and gh.

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
)

// assignState is the open overlay. Like the settings and dispatch forms it
// lives behind a pointer on the model, so a value-receiver Update cannot lose
// what is half-typed.
type assignState struct {
	pane    string          // "repos" | "products" — which side the cursor is on
	repoCur int             // cursor in the repo list
	prodCur int             // cursor in the product list
	marked  map[string]bool // repos marked with space, by name
	naming  bool            // the new-product name prompt is up
	newName string          // what has been typed into it
	err     string          // a failed save, shown until the next key
}

func newAssign(naming bool) *assignState {
	return &assignState{pane: "repos", marked: map[string]bool{}, naming: naming}
}

// assignRepos is the repo list, unassigned first — those are the ones the
// screen exists to deal with — then grouped by product, then by name.
func assignRepos() []repoRow {
	out := append([]repoRow(nil), repoInventory...)
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].product != "", out[j].product != ""
		if ai != aj {
			return !ai
		}
		if out[i].product != out[j].product {
			return out[i].product < out[j].product
		}
		return out[i].name < out[j].name
	})
	return out
}

// assignProducts lists what repos can be moved into: every configured product,
// including one whose repos have all been moved out — the name survives so it
// can be filled again — plus any product a discovered repo claims.
func (m model) assignProducts() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if m.cfg != nil {
		for p := range m.cfg.Products {
			add(p)
		}
	}
	for _, r := range repoInventory {
		add(r.product)
	}
	sort.Strings(out)
	return out
}

// targets are the repos an act applies to: everything marked, or — when nothing
// is marked — the row under the cursor. Mirrors the design, and means the
// common case (move this one) needs no marking at all.
func (st *assignState) targets(rows []repoRow) []string {
	var out []string
	for _, r := range rows {
		if st.marked[r.name] {
			out = append(out, r.name)
		}
	}
	if len(out) == 0 && len(rows) > 0 {
		out = append(out, rows[clampCursor(st.repoCur, len(rows))].name)
	}
	return out
}

// nRepos renders "1 repo" / "N repos".
func nRepos(n int) string { return itoa(n) + " " + plural(n, "repo", "repos") }

// ---- writing the mapping ----------------------------------------------------

// assignTo moves names into product (or out of every product when product is
// ""), saves config.toml, and reports it. The in-memory inventory is updated
// too, so the list under the cursor redraws immediately rather than after the
// next snapshot — which is a git/gh round trip away.
func (m model) assignTo(names []string, product, notice string) (model, tea.Cmd) {
	if m.cfg == nil {
		m.notice = "no config loaded — nothing to assign into"
		return m, nil
	}
	if len(names) == 0 {
		return m, nil
	}
	if m.cfg.Products == nil {
		m.cfg.Products = map[string][]string{}
	}
	moving := map[string]bool{}
	for _, n := range names {
		moving[n] = true
	}
	// Out of wherever they were: a repo belongs to exactly one product, so the
	// move is a removal everywhere plus one insert.
	for p, list := range m.cfg.Products {
		kept := make([]string, 0, len(list))
		for _, n := range list {
			if !moving[n] {
				kept = append(kept, n)
			}
		}
		m.cfg.Products[p] = kept
	}
	if product != "" {
		m.cfg.Products[product] = append(m.cfg.Products[product], names...)
		sort.Strings(m.cfg.Products[product])
	}
	if err := config.Save(m.cfg); err != nil {
		if m.assign != nil {
			m.assign.err = "config.toml not written: " + err.Error()
		}
		return m, nil
	}
	for i := range repoInventory {
		if moving[repoInventory[i].name] {
			repoInventory[i].product = product
		}
	}
	m.notice = notice
	return m, loadSnapshotCmd(m.cfg)
}

// ---- keys -------------------------------------------------------------------

// updateAssign handles keys while the overlay is open. handled is false only
// when the key closed the overlay and still means something outside it (the
// lens digits), so keys.go can carry on with it.
func (m model) updateAssign(k string) (model, tea.Cmd, bool) {
	st := m.assign
	if st == nil {
		return m, nil, false
	}
	rows := assignRepos()
	prods := m.assignProducts()

	st.err = ""
	if st.naming {
		mm, cmd := m.assignNameKey(k, rows)
		return mm, cmd, true
	}

	switch k {
	case "esc", "a":
		m.assign = nil
		return m, nil, true

	case "tab":
		if st.pane == "repos" {
			st.pane = "products"
		} else {
			st.pane = "repos"
		}
		return m, nil, true

	case "n":
		st.naming, st.newName = true, ""
		return m, nil, true

	case "j", "down":
		if st.pane == "repos" {
			st.repoCur = clampCursor(st.repoCur+1, len(rows))
		} else {
			st.prodCur = clampCursor(st.prodCur+1, len(prods))
		}
		return m, nil, true

	case "k", "up":
		if st.pane == "repos" {
			st.repoCur = maxi(0, st.repoCur-1)
		} else {
			st.prodCur = maxi(0, st.prodCur-1)
		}
		return m, nil, true

	case " ", "space":
		if len(rows) == 0 {
			return m, nil, true
		}
		name := rows[clampCursor(st.repoCur, len(rows))].name
		if st.marked[name] {
			delete(st.marked, name)
		} else {
			st.marked[name] = true
		}
		st.repoCur = clampCursor(st.repoCur+1, len(rows))
		return m, nil, true

	case "enter":
		if len(prods) == 0 {
			st.err = "no product to move them into — n names one"
			return m, nil, true
		}
		targets := st.targets(rows)
		target := prods[clampCursor(st.prodCur, len(prods))]
		st.marked = map[string]bool{}
		mm, cmd := m.assignTo(targets, target, nRepos(len(targets))+" → "+target)
		return mm, cmd, true

	case "u":
		targets := st.targets(rows)
		st.marked = map[string]bool{}
		mm, cmd := m.assignTo(targets, "", nRepos(len(targets))+" moved out of their product")
		return mm, cmd, true

	case "U":
		var all []string
		for _, r := range rows {
			if r.product != "" {
				all = append(all, r.name)
			}
		}
		if len(all) == 0 {
			st.err = "nothing is assigned"
			return m, nil, true
		}
		st.marked = map[string]bool{}
		mm, cmd := m.assignTo(all, "", "every repo unassigned · "+nRepos(len(all))+" back in no product")
		return mm, cmd, true
	}

	// The lens digits still switch lenses from in here; everything else is the
	// overlay's, so a stray key cannot fire an action on the screen behind it.
	if isLensDigit(k) {
		m.assign = nil
		return m, nil, false
	}
	return m, nil, true
}

// assignNameKey handles the new-product name prompt. Enter creates the product
// and moves the targets in — a product is its repos, so creation fills it.
func (m model) assignNameKey(k string, rows []repoRow) (model, tea.Cmd) {
	st := m.assign
	next, submit, cancel := applyEdit(st.newName, k, m.key)
	switch {
	case cancel:
		st.naming, st.newName = false, ""
		return m, nil
	case !submit:
		st.newName = next
		return m, nil
	}

	name := strings.TrimSpace(st.newName)
	st.naming, st.newName = false, ""
	if name == "" {
		return m, nil
	}
	targets := st.targets(rows)
	if len(targets) == 0 {
		st.err = "no repos to move in — a product is its repos"
		return m, nil
	}
	existing := false
	for _, p := range m.assignProducts() {
		if p == name {
			existing = true
			break
		}
	}
	verb := "created \"" + name + "\" · "
	if existing {
		verb = "\"" + name + "\" already existed · "
	}
	st.marked = map[string]bool{}
	mm, cmd := m.assignTo(targets, name, verb+nRepos(len(targets))+" moved in")
	// Leave the cursor on what was just made, so the next enter fills it further.
	for i, p := range mm.assignProducts() {
		if p == name {
			st.prodCur, st.pane = i, "products"
		}
	}
	return mm, cmd
}

// ---- view -------------------------------------------------------------------

// assignBlurb is the standing explanation of what a product is for. It is the
// only prose on the screen, and it is what makes the two lists mean something.
const assignBlurb = "A product is the thing you ship. Repos are where it lives. " +
	"Assign them once and every other screen groups by product."

func (m model) viewAssign(w, h int) string {
	st := m.assign
	rows := assignRepos()
	prods := m.assignProducts()

	// Same geometry as the products lens: a 30% right rail, the lists on the
	// left. Below the two-pane breakpoint only the focused side is drawn — tab
	// moves between them — rather than squeezing two unreadable columns in.
	if !m.fit().showDetail {
		inner := maxi(1, w-2*pad)
		if st.pane == "repos" {
			return clampLines(productsPane(m.assignRepoPane(inner, rows, h), w), h)
		}
		// The rail is a narrow list of names; full-width it reads as a banner.
		return clampLines(productsPane(m.assignProductPane(mini(inner, 52), prods, rows, h), w), h)
	}

	rightW := w * 30 / 100
	leftW := w - rightW - 1
	leftInner := maxi(1, leftW-2*pad)
	rightInner := maxi(1, rightW-2*pad)

	leftBlock := productsPane(m.assignRepoPane(leftInner, rows, h), leftW)
	rightBlock := productsPane(m.assignProductPane(rightInner, prods, rows, h), rightW)

	hh := mini(h, maxi(strings.Count(leftBlock, "\n"), strings.Count(rightBlock, "\n"))+1)
	out := hjoin(padBlockTo(leftBlock, hh), vrule(hh, cRule), padBlockTo(rightBlock, hh))
	return clampLines(out, h)
}

// assignRepoPane is the repo table: mark, name, forge, current product, how
// many dispatchers are out on it, and how long since its last commit.
func (m model) assignRepoPane(cw int, rows []repoRow, h int) []string {
	st := m.assign
	focused := st.pane == "repos"

	label := "repos · tab to the products"
	if !focused {
		label = "repos"
	}
	marked := len(st.marked)
	right := "space marks · enter moves the cursor row"
	if marked > 0 {
		right = itoa(marked) + " marked · enter moves them"
	}

	out := []string{
		row(cw, "", flexc(label, cDim), cr(right, 40, cFaint)),
		row(cw, "",
			c("", 3, ""),
			flexc("REPO", cFaint),
			c("FORGE", 7, cFaint),
			c("PRODUCT", 22, cFaint),
			cr("OUT", 6, cFaint),
			cr("LAST COMMIT", 13, cFaint),
		),
	}
	if len(rows) == 0 {
		out = append(out, "", fg(cFaint, "no repos found · , edits the scan roots"))
		return out
	}

	sel := clampCursor(st.repoCur, len(rows))
	start, end := window(sel, len(rows), maxi(1, h-len(out)-2))
	for i := start; i < end; i++ {
		r := rows[i]
		on := focused && i == sel
		mark, markColor, bg, nameColor := " ", cMid, cTransparent, cFg
		if r.product != "" {
			nameColor = cMid // already placed — the unassigned ones are the work
		}
		if on {
			mark, bg, nameColor = "▸", cSel, cWhite
		}
		if st.marked[r.name] {
			mark, markColor = "✓", cGreen
		}
		product, productColor := r.product, cDim
		if product == "" {
			product, productColor = "—", cAmber
		}
		outLabel := ""
		if r.out > 0 {
			outLabel = itoa(r.out)
		}
		out = append(out, row(cw, bg,
			c(mark, 3, markColor),
			flexc(r.name, nameColor),
			c(r.forge, 7, cDim),
			c(product, 22, productColor),
			cr(outLabel, 6, cDim),
			cr(orDash(r.last), 13, cFaint),
		))
	}
	if end < len(rows) || start > 0 {
		out = append(out, fg(cFaint, itoa(sel+1)+"/"+itoa(len(rows))))
	}
	return out
}

// assignProductPane is the right rail: the products repos can move into, the
// new-product prompt, and the standing explanation at the foot.
func (m model) assignProductPane(cw int, prods []string, rows []repoRow, h int) []string {
	st := m.assign
	focused := st.pane == "products"

	label := "products · tab back to the repos"
	if !focused {
		label = "products"
	}
	out := []string{line(label, cw, cDim, "")}

	if len(prods) == 0 {
		out = append(out, "", fg(cFaint, "none yet · n names the first one"))
	}
	sel := clampCursor(st.prodCur, len(prods))
	for i, p := range prods {
		on := focused && i == sel
		mark, bg, nameColor := " ", cTransparent, cFg
		if on {
			mark, bg, nameColor = "▸", cSel, cWhite
		}
		n, dispatchersOut := 0, 0
		for _, r := range rows {
			if r.product == p {
				n++
				dispatchersOut += r.out
			}
		}
		out = append(out, row(cw, bg, c(mark, 3, cMid), flexc(p, nameColor)))
		out = append(out, row(cw, bg, c("", 3, ""), flexc(nRepos(n)+" · "+itoa(dispatchersOut)+" out", cFaint)))
	}

	out = append(out, "", fg(cRule, strings.Repeat("─", maxi(1, cw))))
	out = append(out, row(cw, "", c("n", 3, cFg), flexc("new product…", cDim)))

	if st.naming {
		hint := "enter creates it · mark repos with space first to fill it"
		if n := len(st.marked); n > 0 {
			hint = "enter creates it and moves the " + itoa(n) + " marked " + plural(n, "repo", "repos") + " in"
		} else if len(rows) > 0 {
			hint = "enter creates it and moves " + rows[clampCursor(st.repoCur, len(rows))].name + " in"
		}
		out = append(out, "", fg(cDim, "new product"))
		out = append(out, fg(cWhite, st.newName)+paint("#0a0a0a", cWhite, " "))
		for _, ln := range productsWrap(hint, cw) {
			out = append(out, fg(cFaint, ln))
		}
	}
	if st.err != "" {
		out = append(out, "")
		for _, ln := range productsWrap("! "+st.err, cw) {
			out = append(out, fg(cRed, ln))
		}
	}

	// Pin the blurb to the foot of the pane when there is room for it.
	blurb := productsWrap(assignBlurb, cw)
	if gap := h - len(out) - len(blurb); gap > 1 {
		out = append(out, make([]string, gap)...)
		for _, ln := range blurb {
			out = append(out, fg(cFaint, ln))
		}
	}
	return out
}

// orDash renders "" as an em dash so an unknown figure reads as unknown rather
// than as a blank the eye skips.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
