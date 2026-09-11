package cockpit

// cluster_view.go renders the assignment editor: repos on the left, products on
// the right, and the inline naming prompt. Layout follows the design's
// clOpen/clNaming sections — a 30% right pane, a marked column, and the
// explanation pinned to the bottom of the product list.

import (
	"path/filepath"
	"strings"
)

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
	markedLine := m.keys.KeyFor("editor.mark") + " marks · " +
		m.keys.KeyFor("editor.where") + " where it lives · " +
		m.keys.KeyFor("search.open") + " search"
	if marked > 0 {
		word := "repo"
		if marked != 1 {
			word = "repos"
		}
		markedLine = itoa(marked) + " " + word + " marked · enter moves them"
	}
	// While a fold has the keyboard the hint is about the fold, because every
	// key it names now means something else.
	if m.clFoldRow != "" {
		markedLine = "checkouts of " + m.clFoldRow + " · enter picks one · esc leaves"
	}
	// And a live search outranks both: it is the thing the keys currently mean.
	if s := m.searchLine(); s != "" {
		markedLine = s
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
	// An unfolded row is several lines, so the window is taken over rendered
	// LINES with the cursor's own line as the anchor. Windowing over rows and
	// then expanding them would scroll by a row and move the screen by six,
	// walking the cursor off exactly the way rendering everything used to.
	var lines []string
	rowLine := make([]int, len(rows))
	for i, r := range rows {
		bg, mark, nameColor := cTransparent, " ", cFg
		if m.clMarked[r.name] {
			mark = "◆"
		}
		if i == sel && m.clPane == "repos" {
			if mark == " " {
				mark = "▸"
			}
			// The repo row keeps its marker but gives up the highlight while its
			// own fold is being steered: two lit rows would be two cursors, and
			// only one of them is taking the keys.
			if m.clFoldRow == "" {
				bg, nameColor = cSel, cWhite
			}
		}
		prod, prodColor := r.product, cMid
		if prod == "" {
			prod, prodColor = "—", cFaint
		}
		rowLine[i] = len(lines)
		lines = append(lines, row(cw, bg,
			c(mark, 3, cAmber),
			flexc(r.name, nameColor).highlight(m.searchPosFor(searchRepos, i), cSearchHit),
			c(r.forge, 6, cFaint),
			flexc(prod, prodColor),
			cr(itoa(r.out), 8, cFaint),
			cr(r.last, 8, cFaint),
		))
		if m.clExpanded[r.name] {
			focus := -1
			var hi []map[int]bool
			if m.clFoldRow == r.name {
				focus = clampCursor(m.clFoldIdx, len(r.worktrees))
				hi = m.clFoldHighlights(r)
			}
			lines = append(lines, clFold(r, cw, focus, hi)...)
		}
	}
	// Leave a line for the "showing x of y" footer so it cannot itself be the
	// row that gets clipped.
	start, end := window(rowLine[sel], len(lines), maxi(h-len(out)-1, 1))
	out = append(out, lines[start:end]...)
	// Only when the list is actually scrolling: on a portfolio that fits, a
	// position counter is noise. It counts repos, not lines — the lines are an
	// artefact of what is unfolded, and "38 of 114" would be a figure about
	// nothing the human chose.
	if end-start < len(lines) {
		out = append(out, fg(cFaint, itoa(sel+1)+" of "+itoa(len(rows))))
	}
	return out
}

// clServerLine is the fold's one line about where a repo's sessions run: the
// server, what they are launched under when that is anything, and the key that
// changes it. An empty socket is named rather than left blank — "the default
// server" is an answer, and a gap where a name goes reads as a failure to find
// one.
func clServerLine(r clRepoRow) string {
	server := r.socket
	if server == "" {
		server = "default server"
	}
	line := "server " + server
	if r.env != "" {
		line += " · " + r.env
	}
	return line + " · s to change"
}

// clFoldMax is how many other checkouts an unfolded row lists before it counts
// the rest. One repo here has sixty-eight of them, and a list that long is a
// screen rather than an annotation — but it says how many it is not showing,
// the way the product panel's history does, rather than stopping mute.
const clFoldMax = 8

// clFold is what `p` unfolds under a repo row.
//
// A row is a repository, and a repository is not a folder: it may be named for
// none of the directories it occupies (a clone of `ordain` sitting in
// `ord-ai-n`) and it may occupy many (sixty-nine checkouts of one bare repo).
// Both of those make "which of these is it?" a real question to be asked while
// deciding where a repo belongs — so the fold is the absolute path the row acts
// in, then every checkout git knows of, with the acting one marked.
//
// focus is the checkout under the fold's own cursor while it has the keyboard,
// and -1 when it does not. The list windows around it: the answer to "which one
// is it now" must never be a row you have to scroll to find, and with
// sixty-nine checkouts a fixed first-eight would put it off screen for most
// repos.
// hi carries the search highlight for each checkout, indexed as r.worktrees is,
// and is nil when no query is up.
func clFold(r clRepoRow, cw, focus int, hi []map[int]bool) []string {
	lead := c("", 3, "")
	inner := maxi(cw-3, 1)

	path := r.path
	if path == "" {
		// Discovery found the repository but no checkout of it is on disk — a
		// bare repo whose worktrees have all been removed. Saying so is the
		// point of the fold; an empty line would read as "no path".
		path = "no checkout on disk"
	}
	pathColor := cMid
	if r.pinned {
		pathColor = cWhite
	}
	out := []string{row(cw, "", lead, flexc(clElide(path, inner), pathColor))}

	// Where its files are, then where its sessions are. The second is as much a
	// fact about this repo as the first, and it is the one nothing else on any
	// screen says: a dispatch starts on this server, under this command, and
	// every pane opened in it afterwards sees what that command put on PATH.
	out = append(out, row(cw, "", lead, flexc(clElide(clServerLine(r), inner), cFaint)))

	// One checkout is one directory, and "1 checkouts" beneath its own path is
	// a sentence about nothing.
	if len(r.worktrees) < 2 {
		return out
	}

	meta := itoa(len(r.worktrees)) + " checkouts"
	switch {
	case focus >= 0:
		meta += " · enter picks where this row works · esc leaves"
	case r.pinned:
		meta += " · pinned · p to change"
	default:
		meta += " · chosen · p to change"
	}
	out = append(out, row(cw, "", lead, flexc(meta, cFaint)))

	start, end := 0, mini(clFoldMax, len(r.worktrees))
	if focus >= 0 {
		start, end = window(focus, len(r.worktrees), clFoldMax)
	}
	for i := start; i < end; i++ {
		w := r.worktrees[i]
		branch := w.branch
		if branch == "" {
			branch = "detached"
		}
		// ● is the checkout this row acts in; the highlight is where the fold's
		// cursor is. They are different facts and a reader has to be able to see
		// both at once — the point of the screen is moving one onto the other.
		mark, nameColor, bg := " ", cDim, cTransparent
		if w.path == r.path {
			mark, nameColor = "●", cMid
		}
		if i == focus {
			bg, nameColor = cSel, cWhite
		}
		out = append(out, row(cw, bg, lead,
			c(mark, 2, cAmber),
			flexc(filepath.Base(w.path), nameColor).highlight(at(hi, i), cSearchHit),
			c(branch, 30, cFaint),
		))
	}
	if hidden := len(r.worktrees) - (end - start); hidden > 0 {
		out = append(out, row(cw, "", lead, c("", 2, ""), flexc("…"+itoa(hidden)+" more", cFaint)))
	}
	return out
}

// at indexes a highlight slice that is usually absent: no search is the normal
// state of the screen, so nil is the common case rather than an error one.
func at(hi []map[int]bool, i int) map[int]bool {
	if i < 0 || i >= len(hi) {
		return nil
	}
	return hi[i]
}

// clElide fits an absolute path into w columns by dropping its middle. A plain
// truncation takes the tail, which is the half that says which checkout this
// is; the head says whose machine and which root, and is worth as much.
func clElide(s string, w int) string {
	if w <= 1 || dispWidth(s) <= w {
		return s
	}
	r := []rune(s)
	keep := w - 1
	head := keep / 3
	return string(r[:head]) + "…" + string(r[len(r)-(keep-head):])
}

// clPaneLabel names which pane has the keyboard.
func (m model) clPaneLabel() string {
	if m.clPane == "products" {
		return "products · tab to repos"
	}
	return "repos · tab to products"
}

// clHint is a prompt's explanatory line, wrapped to the pane.
//
// These are full sentences in a column a third of the terminal wide, so they
// were drawn wider than the pane they sit in: the over-long line pushes past
// the vertical rule and the terminal wraps it, breaking the two-pane layout at
// exactly the moment the human has a half-typed buffer on screen. The tail
// explanation below has always gone through productsWrap for this reason; the
// prompts are the same prose in the same column.
func clHint(s string, cw int) []string {
	var out []string
	for _, ln := range productsWrap(s, cw) {
		out = append(out, fg(cFaint, ln))
	}
	return out
}

// clRight is the product list, the new-product affordance, the naming and token
// prompts when one is up, and the explanation pinned to the bottom.
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
		out = append(out, fg(cFaint, "none yet — "+m.keys.KeyFor("products.new")+" names one"))
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
		// A product's own Linear token is said, never shown: the sub-line is
		// already the product's summary, and whether its backlog is read with a
		// key of its own is the other thing about it that is configured. The
		// token itself never reaches the screen — it is a secret, and this pane
		// is what a shoulder reads.
		meta := itoa(n) + " " + word
		if m.clLinearKey(p) != "" {
			meta += " · linear token"
		}
		out = append(out,
			row(cw, bg, c(mark, 3, cMid), flexc(p, cFg)),
			row(cw, bg, c("", 3, ""), flexc(meta, cFaint)),
		)
	}

	// The keys these hints name are read from the keymap, not spelled here: a
	// hint beside a remappable binding is the same lie the help sheet would be.
	out = append(out, "", fg(cRule, strings.Repeat("─", cw)))
	out = append(out, row(cw, "", c(m.keys.KeyFor("products.new"), 3, cFg), flexc("new product…", cDim)))
	out = append(out, row(cw, "", c(m.keys.KeyFor("editor.linear"), 3, cFg), flexc("linear token…", cDim)))

	if m.clNaming {
		out = append(out, "", fg(cDim, "new product"))
		out = append(out, fg(cWhite, m.clNewName)+paint(cFg, cFg, " "))
		hint := "enter creates it and moves the marked repos in"
		if len(m.clTargets()) == 1 {
			hint = "enter creates it and moves this repo in"
		}
		out = append(out, "")
		out = append(out, clHint(hint, cw)...)
	}

	if m.clKeying {
		out = append(out, "", fg(cDim, "linear token · "+m.clKeyFor))
		// Masked as it is typed, like the settings editor's secret field: a
		// pasted key is not read back off the screen, and a screen-shared
		// cockpit must not be how it leaks.
		//
		// Capped at the pane. A Linear key runs to about fifty characters and
		// this pane is a third of the terminal, so one dot per rune is a line
		// wider than the column it is drawn in — which pushes the pane past its
		// own rule and wraps the whole editor. The dots are a sign that the
		// field is receiving, not a character count, so there is nothing to lose
		// by stopping at the edge.
		dots := mini(len([]rune(m.clKeyText)), maxi(cw-1, 0))
		out = append(out, fg(cWhite, strings.Repeat("•", dots))+paint(cFg, cFg, " "))
		hint := "enter saves it · this product's backlog is read with it"
		if m.clLinearKey(m.clKeyFor) != "" {
			hint = "enter replaces the token · empty clears it"
		}
		out = append(out, "")
		out = append(out, clHint(hint, cw)...)
		out = append(out, clHint("scope the key to this product's teams in Linear", cw)...)
	}

	if m.clSockOpen {
		out = append(out, "", fg(cDim, "tmux server · "+m.clSockFor))
		out = append(out, fg(cWhite, clElide(m.clSockText, maxi(cw-1, 0)))+paint(cFg, cFg, " "))
		out = append(out, "")
		out = append(out, clHint("enter saves it · the -L socket this repo's sessions live on", cw)...)
		out = append(out, clHint("a session sees the binaries of whoever started it, so one server per repo keeps this repo's own", cw)...)
		out = append(out, clHint("leave it as the repo name unless you already start a server for this project yourself — then type that name", cw)...)
		out = append(out, clHint("empty means the default server, shared with every other repo that names none", cw)...)
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
