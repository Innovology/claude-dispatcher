package cockpit

// decisions.go is the DECISIONS lens (lens 5): every decision the repos have
// written down, wherever they wrote it (collect_decisions.go). The left column
// is the repos that have any, grouped by product; the middle is the selected
// repo's decisions, grouped by the file or folder they came from; the right is
// the selected one in full, with `o` to open it where it was written. `/`
// searches every repo's decisions at once (decsearch.go).
//
// It was a port of a design in which Claude proposed decisions and the human
// accepted them, and kept the parts of that nothing ever did: a "plugins" box,
// a status the screen said "you accepted", and `a`, `s` and `e` keys that only
// printed notices saying they could not act. They are gone. The lens reads; the
// one key that does something is the one that takes you to the file.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// decision is one decision found in a repo's markdown. by is where it was
// found as "path:line", file and line the same for the editor; group and where
// are the group it is listed under and that group's folder; when dates it, and
// at is that date as an age, taken at each load.
type decision struct {
	id, title, status, at, by, context, decision, consequences string
	group, where, file                                         string
	line                                                       int
	when                                                       time.Time
	adr                                                        bool // read from a file that is one ADR
}

// decisionStatusMeta is the colour, glyph and one-line note for each state.
// The words are the repos' own: a decision is decided, still open, or replaced.
var decisionStatusMeta = map[string]struct{ color, glyph, note string }{
	"open":       {cAmber, "◆", "still open — written down as not decided yet"},
	"decided":    {cGreen, "✓", "written down as decided"},
	"superseded": {cDim, "·", "replaced or withdrawn"},
}

// decWrap word-wraps s into lines no wider than w columns.
func decWrap(s string, w int) []string {
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
		if cur == "" {
			cur = wd
			continue
		}
		if dispWidth(cur)+1+dispWidth(wd) <= w {
			cur += " " + wd
		} else {
			lines = append(lines, cur)
			cur = wd
		}
	}
	return append(lines, cur)
}

// viewDecisions renders the three-column decisions lens. Which columns show is
// driven by m.fit().dec (3 → all three; 2 → mid+body; 1 → a single column
// chosen by m.pane).
func (m model) viewDecisions(w, h int) string {
	dec := m.fit().dec

	repos := decisionRepoOrder
	if len(repos) == 0 {
		msg := vjoin(
			fg(cDim, "where decisions live"),
			"",
			fg(cFaint, "no decisions found in any repo."),
			"",
			fg(cFaint, "every .md file in each repo is read, wherever it is, and"),
			fg(cFaint, "these count:"),
			fg(cFaint, "  · an ADR — Status, Context, Decision, Consequences"),
			fg(cFaint, "  · a file titled as decisions (\"Stack decisions\"): each"),
			fg(cFaint, "    ## under it is one"),
			fg(cFaint, "  · a section named for decisions (\"Decided\", \"Open"),
			fg(cFaint, "    decisions\"): each bullet or sub-heading is one"),
			fg(cFaint, "  · a heading marked RATIFIED, RULED, WITHDRAWN, or"),
			fg(cFaint, "    \"Decision 3 — …\", and a line marked [NEEDS DECISION]"),
			"",
			fg(cFaint, "write any of them and it appears here on the next load."),
		)
		return clampLines(gutter(msg, pad), h)
	}
	ri := clampCursor(m.decRepo, len(repos))
	repo := repos[ri]
	list := decisions[repo]
	ci := clampCursor(m.decCursor, len(list))
	// A search is drawn in the same columns: its hits where the repo's list
	// was, and the selected hit in the body.
	mid := func(cw, budget int) string {
		if m.decSearchShown() {
			return m.decHitsBlock(cw, budget)
		}
		return decMidBlock(cw, budget, repo, list, ci)
	}

	// Single column: mid when not in detail, body when in detail.
	if dec <= 1 {
		cw := maxi(w-pad, 1)
		var block string
		if m.pane == "detail" {
			block = m.decBodyBlock(cw, list, ci)
		} else {
			// The body pane and the left column are both gone here, so the
			// inline block carries the decision's prose as well as the repos.
			// The list keeps at least half the height: a decision's prose can
			// run to a screen, and is cut at the bottom here — → shows it whole.
			narrow := m.decNarrowBlock(cw, list, ci, true)
			block = vjoin(append([]string{mid(cw, maxi(h-len(narrow), maxi(h*55/100, 8)))},
				narrow...)...)
		}
		return clampLines(gutter(padBlockTo(block, h), pad), h)
	}

	var blocks []string
	if dec >= 3 {
		lw := maxi(w*22/100, 1)
		mw := maxi(w*38/100, 1)
		bw := maxi(w-2-lw-mw, 1)
		left := gutter(padBlockTo(m.decLeftBlock(maxi(lw-pad, 1), repo), h), pad)
		middle := gutter(padBlockTo(mid(maxi(mw-pad, 1), h), h), pad)
		body := gutter(padBlockTo(m.decBodyBlock(maxi(bw-pad, 1), list, ci), h), pad)
		blocks = []string{left, vrule(h, cRule), middle, vrule(h, cRule), body}
	} else { // dec == 2: mid + body, left hidden.
		mw := maxi(w*42/100, 1)
		bw := maxi(w-1-mw, 1)
		mwi := maxi(mw-pad, 1)
		// Only the left column is hidden at this tier, and the body pane is
		// still showing the prose — so the inline block re-homes just what the
		// left column carried, without repeating the decision side by side.
		narrow := m.decNarrowBlock(mwi, list, ci, false)
		midBlock := vjoin(append([]string{mid(mwi, maxi(h-len(narrow), 8))},
			narrow...)...)
		middle := gutter(padBlockTo(midBlock, h), pad)
		body := gutter(padBlockTo(m.decBodyBlock(maxi(bw-pad, 1), list, ci), h), pad)
		blocks = []string{middle, vrule(h, cRule), body}
	}
	return clampLines(hjoin(blocks...), h)
}

// decRepoCount is a repo's tally for the "where decisions live" rows: the
// number still open (amber, flagged ◆) when any are, else the total. Shared by
// the left column and the collapsed layouts' inline tail so the two can never
// disagree about a repo's count.
func decRepoCount(repo string) (count, color string) {
	rl := decisions[repo]
	open := 0
	for _, d := range rl {
		if d.status == "open" {
			open++
		}
	}
	if open > 0 {
		return itoa(open) + "◆", cAmber
	}
	return itoa(len(rl)), cDim
}

// decRepoTally is a repo's count as the lens draws it now: decRepoCount, or
// while a query is up how many of its hits are that repo's — "·" for none, so
// a repo the search found nothing in is still listed where it was.
func decRepoTally(repo string, hits map[string]int) (count, color string) {
	if hits == nil {
		return decRepoCount(repo)
	}
	if n := hits[repo]; n > 0 {
		return itoa(n), cSearchHit
	}
	return "·", cFaint
}

// decProduct is the label a repo is grouped under in the left column.
func decProduct(repo string) string {
	if p := decisionProduct[repo]; p != "" {
		return p
	}
	return "unassigned"
}

// decNarrowBlock is what the collapsed layouts lose, re-homed under the list.
// At two columns only the left column is hidden, so it carries just the repos;
// at one column the body pane is gone too and the selected decision's prose
// comes with it. At three columns nothing is missing and this is not rendered.
func (m model) decNarrowBlock(cw int, list []decision, ci int, prose bool) []string {
	out := []string{"", fg(cRule, strings.Repeat("─", cw))}

	if x, lit, ok := m.decShown(list, ci); prose && ok {
		section := func(label, body string, lit map[int]bool, hex string) {
			if body == "" {
				return
			}
			out = append(out, "")
			out = append(out, line(label, cw, cDim, ""))
			out = append(out, decWrapLit(body, cw, lit, hex)...)
		}
		section("context", x.context, lit.prose[0], cMid)
		section("decision", x.decision, lit.prose[1], cFg)
		section("consequences", x.consequences, lit.prose[2], cMid)
		out = append(out, "", line(x.by, cw, cFaint, ""))
	}

	out = append(out, "")
	out = append(out, line("where decisions live", cw, cDim, ""))
	hits := m.decHitCounts()
	for _, r := range decisionRepoOrder {
		count, countColor := decRepoTally(r, hits)
		out = append(out, row(cw, "", flexc(r, cMid), cr(count, 6, countColor)))
	}
	return out
}

// decLeftBlock is the "where decisions live" column: the repos that have any,
// under their products, with each one's open count or total.
func (m model) decLeftBlock(cw int, selRepo string) string {
	// While a query is up the repo marked is the selected hit's, and each
	// count is how many hits that repo has.
	hits := m.decHitCounts()
	if hits != nil {
		selRepo = ""
		if rec, ok := m.decSelectedRec(); ok {
			selRepo = rec.repo
		}
	}
	var lines []string
	lines = append(lines, line("where decisions live", cw, cDim, ""))
	prev := "\x00"
	for _, r := range decisionRepoOrder {
		if p := decProduct(r); p != prev {
			lines = append(lines, "", line(p, cw, cFaint, ""))
			prev = p
		}
		bg, nameColor, marker := "", cFg, ""
		if r == selRepo {
			bg, nameColor, marker = cSel, cWhite, "▸"
		} else if hits != nil && hits[r] == 0 {
			nameColor = cFaint
		}
		count, countColor := decRepoTally(r, hits)
		lines = append(lines, row(cw, bg, c(marker, 2, cMid), flexc(r, nameColor), cr(count, 6, countColor)))
	}
	lines = append(lines, "")
	lines = append(lines, line("read from every .md file in each repo", cw, cFaint, ""))
	if decisionNone > 0 {
		lines = append(lines, line(countOf(decisionNone, "repo", "repos")+" with none found", cw, cFaint, ""))
	}
	if hits != nil {
		lines = append(lines, line("matches in each repo", cw, cFaint, ""))
	} else {
		lines = append(lines, line("J/K another repo · / search them all", cw, cFaint, ""))
	}
	return vjoin(lines...)
}

// decMidBlock is the selected repo's decisions in budget lines: the ones from
// the repo's root files first, then each group under a header naming it and
// the folder it came from. A repo with hundreds of decisions is windowed
// around the cursor, with what is cut counted, so j never walks the cursor off
// a list that does not move.
func decMidBlock(cw, budget int, repo string, list []decision, ci int) string {
	open := 0
	for _, d := range list {
		if d.status == "open" {
			open++
		}
	}
	tally := countOf(len(list), "decision", "decisions")
	if open > 0 {
		tally += " · " + itoa(open) + " open"
	}
	head := row(cw, "",
		c(repo, dispWidth(repo)+2, cDim),
		seg{text: tally, flex: true, align: alignRight, hex: cFaint},
	)

	var rows []string
	sel := 0
	group := "\x00"
	for n, d := range list {
		if d.group != group {
			group = d.group
			if group != "" {
				size := 0
				for _, x := range list {
					if x.group == group {
						size++
					}
				}
				where := d.where
				if where != "" {
					where += " · "
				}
				rows = append(rows, "", row(cw, "",
					c(group, dispWidth(group)+2, cMid),
					seg{text: where + itoa(size), flex: true, align: alignRight, hex: cFaint},
				))
			}
		}
		on := n == ci
		if on {
			sel = len(rows)
		}
		bg, titleColor, marker := "", cFg, ""
		if on {
			bg, titleColor, marker = cSel, cWhite, "▸"
		}
		st := decisionStatusMeta[d.status]
		rows = append(rows, row(cw, bg,
			c(marker, 2, cMid), c(st.glyph, 2, st.color), c(d.id, 7, cMid), flexc(d.title, titleColor),
		))
		rows = append(rows, row(cw, "",
			c("", 4, ""), c(d.status, dispWidth(d.status)+1, st.color), flexc(d.by, cFaint),
		))
	}
	if len(list) == 0 {
		rows = append(rows, "", line("nothing recorded here yet", cw, cFaint, ""))
	}
	return vjoin(append([]string{head}, historyWindow(rows, sel, budget-1)...)...)
}

// decBodyBlock is the selected decision in full: where it was found, its
// state, and its prose.
func (m model) decBodyBlock(cw int, list []decision, ci int) string {
	x, lit, ok := m.decShown(list, ci)
	if !ok {
		if m.decSearching() {
			return fg(cFaint, "no decision matches")
		}
		return fg(cFaint, "no decisions")
	}
	st := decisionStatusMeta[x.status]
	wrapW := mini(cw, 74)

	var lines []string
	tag := x.group
	if tag == "" {
		tag = x.id
	}
	when := st.glyph + " " + x.status
	if x.at != "" {
		when += " · " + x.at
	}
	lines = append(lines, row(cw, "",
		c(tag, dispWidth(tag)+2, cDim),
		seg{text: when, flex: true, align: alignRight, hex: st.color},
	))
	lines = append(lines, decWrapLit(x.title, cw, lit.title, cWhite)...)
	lines = append(lines, fg(cFaint, x.by))
	lines = append(lines, fg(st.color, st.note))

	section := func(label, body string, lit map[int]bool, hex string) {
		if body == "" {
			return
		}
		lines = append(lines, "")
		lines = append(lines, fg(cDim, label))
		lines = append(lines, decWrapLit(body, wrapW, lit, hex)...)
	}
	section("context", x.context, lit.prose[0], cMid)
	section("decision", x.decision, lit.prose[1], cFg)
	section("consequences", x.consequences, lit.prose[2], cMid)

	lines = append(lines, "")
	lines = append(lines, fg(cDim, "o opens it in your editor"))
	return vjoin(lines...)
}

// updateDecisions handles the lens's keys: j/k move within a repo's
// decisions, ] and [ to the next or previous group, J/K change repo, →/←
// toggle the detail pane, o opens the selected one in the human's editor at
// its line, and / searches every repo's (decsearch.go), whose keys come first
// while a search is up.
func (m model) updateDecisions(k string) (model, tea.Cmd) {
	if mm, cmd, handled := m.updateDecSearch(k); handled {
		return mm, cmd
	}
	repos := decisionRepoOrder
	if len(repos) == 0 {
		return m, nil // nothing found anywhere — there is nothing to move through
	}
	repo := repos[clampCursor(m.decRepo, len(repos))]
	list := decisions[repo]

	switch k {
	case "j", "down":
		n := mini(m.decCursor+1, len(list)-1)
		if n < 0 {
			n = 0
		}
		m.decCursor = n
	case "k", "up":
		m.decCursor = maxi(m.decCursor-1, 0)
	case "J":
		m.decRepo = mini(m.decRepo+1, len(repos)-1)
		m.decCursor = 0
	case "K":
		m.decRepo = maxi(m.decRepo-1, 0)
		m.decCursor = 0
	case "]", "[":
		// A repo can have hundreds; a group is the step a reader takes.
		m.decCursor = decGroupStep(list, clampCursor(m.decCursor, len(list)), k == "]")
	case "o":
		if len(list) == 0 {
			return m, nil
		}
		return m.decOpen(list[clampCursor(m.decCursor, len(list))])
	case "/":
		return m.decSearchStart(), nil
	case "right", "l":
		m.pane = "detail"
	case "left", "h", "esc":
		m.pane = "list"
	}
	return m, nil
}

// decOpen hands the terminal to the human's editor on the decision's file, at
// its line, and reloads on the way back — the file may well have changed.
func (m model) decOpen(d decision) (model, tea.Cmd) {
	cmd := decEditorCommand(d.file, d.line)
	if cmd == nil {
		m.notice = "no editor to open " + d.by + " with — set $EDITOR"
		return m, nil
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return actionMsg{notice: "editor: " + err.Error()}
		}
		return actionMsg{notice: "back from " + d.by}
	})
}

// decEditorCommand is the human's editor on file at line: $VISUAL, then
// $EDITOR, then vi. A value with arguments ("code --wait") is split on
// whitespace, as git reads core.editor. Most editors take the line as "+N"
// before the file; the ones that do not are told in their own spelling. nil
// when nothing can be run.
func decEditorCommand(file string, line int) *exec.Cmd {
	for _, v := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi"} {
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		if _, err := exec.LookPath(f[0]); err != nil {
			continue
		}
		args := append([]string(nil), f[1:]...)
		at := file + ":" + itoa(maxi(line, 1))
		switch filepath.Base(f[0]) {
		case "code", "codium", "code-insiders", "cursor", "windsurf":
			args = append(args, "-g", at)
		case "hx", "helix", "zed", "subl", "sublime_text":
			args = append(args, at)
		default:
			args = append(args, "+"+itoa(maxi(line, 1)), file)
		}
		return exec.Command(f[0], args...)
	}
	return nil
}

// decGroupStep is the cursor moved to the first decision of the next group,
// or back to the start of this one — and from there to the previous — the way
// [ and ] step through paragraphs.
func decGroupStep(list []decision, at int, forward bool) int {
	if len(list) == 0 {
		return 0
	}
	g := list[at].group
	if forward {
		for i := at + 1; i < len(list); i++ {
			if list[i].group != g {
				return i
			}
		}
		return at
	}
	start := at
	for start > 0 && list[start-1].group == g {
		start--
	}
	if start < at {
		return start
	}
	if start == 0 {
		return 0
	}
	prev := list[start-1].group
	for start > 0 && list[start-1].group == prev {
		start--
	}
	return start
}
