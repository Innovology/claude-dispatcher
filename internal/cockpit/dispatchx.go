package cockpit

// dispatchx.go is the triage lens's structured dispatch form — the `dx` form.
//
// It replaces the freeform draft (`cqDraft`) that used to sit under "what
// should we build?". A draft answered only half the question: it said what to
// build and then handed off to a three-step overlay to say where. The form asks
// the five things a dispatch actually needs, on one screen:
//
//	WHERE      which repo it lands in — filtered, not scrolled through
//	TITLE      what it is called: the feature name, and the branch
//	WHAT       the work itself, wrapped over as many rows as it takes
//	DONE WHEN  the completion condition, optional
//	AUTO       whether it runs unattended or checks in between steps
//
// TITLE and WHAT used to be one field, and the branch was named from it. That
// asked one line to be two things at once: a name short enough to live in
// `feature/…`, a worktree path and a tmux session, and a brief long enough to
// dispatch on. What happened in practice is that the brief went into DONE WHEN
// — the only field with room — and the completion condition was lost. Naming
// and describing are separate now, and only TITLE reaches the branch.
//
// Two of the five are honest about their reach. There is no permission plumbing
// behind AUTO and no supervisor watching DONE WHEN: the only thing that reaches
// a session is the prompt, so both are sentences in the prompt and the copy
// promises nothing more (see dxPrompt).
//
// Everything on screen comes from the same real collector data the products
// lens reads (clRepos → reposByProduct). Before the first snapshot lands the
// repo list falls back to a plain discovery scan, and renders the counts it has
// no source for as blank and "—" rather than as zeroes.

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
)

// ---- the five fields ---------------------------------------------------------

// dxFieldID names the line that owns the keyboard. The order is the tab order
// and the enter order, so it is also the order the fields are read in.
type dxFieldID int

const (
	dxWhereF dxFieldID = iota
	dxTitleF
	dxWhatF
	dxGoalF
	dxAutoF
	dxFieldCount
)

// dxRepoRow is one candidate repo: what it belongs to, how much is already in
// flight there, and how long since anyone touched it.
type dxRepoRow struct {
	repo, product, last string
	out                 int
}

// dxUnassigned is what a repo in no product is called *in this form*. It is a
// display and filter value only — typing "unass" narrows to them — and is never
// written anywhere. cluster.go's clUnassigned is the same word for the same
// reason; this one exists because the form maps "" to it before filtering.
const dxUnassigned = clUnassigned

// ---- form state --------------------------------------------------------------

// dxReset closes the form and returns every field to its opening value. It is
// one function because four call sites (esc, submit, `d`, leaving the lens)
// must agree on what "closed" means.
func (m model) dxReset() model {
	m.cqDispatch = false
	m.dxField, m.dxRepo = dxWhereF, 0
	m.dxFilter, m.dxTitle, m.dxWhat, m.dxGoal = "", "", "", ""
	m.dxAuto = true
	return m
}

// dxOpen opens the form over whatever is on screen, optionally pre-filtered so
// a caller that already knows the repo or product does not make the human type
// it again.
func (m model) dxOpen(filter string) model {
	m = m.dxReset()
	m.cqDispatch = true
	m.dxFilter = filter
	return m
}

// dxTouched reports whether anything has been typed into the four text fields.
// It is what decides whether a navigation key is navigation or text: an
// untouched form is not a trap, so 1–6, ':' and 'w' still leave it, but the
// moment there is a filter or a sentence they are letters again. dxAuto and
// dxField deliberately do not count — toggling auto or tabbing about is not
// typing, and must not strand the human on this screen.
func (m model) dxTouched() bool {
	return m.dxFilter != "" || m.dxTitle != "" || m.dxWhat != "" || m.dxGoal != ""
}

// ---- the repo list -----------------------------------------------------------

// dxAllRepos is every repo the form could dispatch into, unfiltered — the
// denominator dxRepoCount reports against.
//
// The snapshot is the source. Before the first one lands (or with no collector
// at all) it falls back to a discovery scan so the form is usable immediately,
// but a scan knows nothing about in-flight dispatches or commit ages: those
// come back as 0 and "—" and render as blank and an em dash. An invented "0
// out" would be a claim about a repo nobody has looked at.
func (m model) dxAllRepos() []dxRepoRow {
	rows := m.clRepos()
	out := make([]dxRepoRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, dxRepoRow{repo: r.name, product: dxProduct(r.product), out: r.out, last: r.last})
	}
	if len(out) == 0 && m.cfg != nil {
		for _, r := range repos.Discover(m.cfg) {
			out = append(out, dxRepoRow{repo: r.Name, product: dxProduct(r.Product), last: "—"})
		}
	}
	dxSort(out)
	return out
}

// dxProduct is the product a row is filed and filtered under.
func dxProduct(p string) string {
	if p == "" {
		return dxUnassigned
	}
	return p
}

// dxSort puts the named products first, alphabetically, and the unassigned
// last. That is the opposite of clRepos's order, on purpose: the assignment
// editor leads with the unassigned because they are the work, while this form
// leads with the products you actually ship.
func dxSort(rows []dxRepoRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ai, aj := rows[i].product == dxUnassigned, rows[j].product == dxUnassigned
		if ai != aj {
			return aj
		}
		if rows[i].product != rows[j].product {
			return rows[i].product < rows[j].product
		}
		return rows[i].repo < rows[j].repo
	})
}

// dxRows are the repos matching the filter. A repo matches on its own name or
// on its product, so "checkout" and "payments" both narrow to the same handful.
func (m model) dxRows() []dxRepoRow {
	all := m.dxAllRepos()
	q := strings.ToLower(strings.TrimSpace(m.dxFilter))
	if q == "" {
		return all
	}
	out := make([]dxRepoRow, 0, len(all))
	for _, r := range all {
		if strings.Contains(strings.ToLower(r.repo), q) || strings.Contains(strings.ToLower(r.product), q) {
			out = append(out, r)
		}
	}
	return out
}

// dxPicked is the row under the cursor, false when the filter matches nothing.
// The cursor is clamped on read rather than on write: the list is derived from
// a snapshot that can change under it between keystrokes.
func (m model) dxPicked() (dxRepoRow, bool) {
	rows := m.dxRows()
	if len(rows) == 0 {
		return dxRepoRow{}, false
	}
	return rows[clampCursor(m.dxRepo, len(rows))], true
}

// ---- the branch --------------------------------------------------------------

// dxBranchWords caps how many slug words the branch preview keeps.
//
// It tracks dispatch.SlugWords deliberately: the preview is only true if Launch
// applies the same cap, and Launch's slug also names the worktree directory and
// the tmux session. It used to be 0 — an uncapped, truthful-but-unusable branch
// — because capping here alone would have mislabelled three things at once.
// Launch caps now, so this can.
const dxBranchWords = dispatchpkg.SlugWords

// dxSlugWords slugifies s and keeps at most max slug words.
//
// It must stay identical to what dispatch.Launch does to the feature name, or
// the branch this form promises is not the branch that appears — and the same
// slug also names the worktree directory and the tmux session, so a drift here
// mislabels three things at once. Launch currently slugifies without a cap; the
// integrator either gives internal/dispatch the same capped call or sets
// dxBranchWords to 0, and nothing in between is honest.
func dxSlugWords(s string, max int) string {
	slug := dispatchpkg.Slugify(s)
	if max <= 0 || slug == "" {
		return slug
	}
	parts := strings.Split(slug, "-")
	if len(parts) > max {
		parts = parts[:max]
	}
	return strings.Join(parts, "-")
}

// dxBranch is the branch TITLE will produce. "untitled" is display-only: submit
// refuses a title that slugs to nothing long before Launch could see it.
func (m model) dxBranch() string {
	slug := dxSlugWords(m.dxTitle, dxBranchWords)
	if slug == "" {
		slug = "untitled"
	}
	return "feature/" + slug
}

// ---- what the dispatcher is actually told -------------------------------------

// dxDispatch turns the form into the two strings a launch needs: the name the
// dispatch is filed under, and the prompt the dispatcher actually receives.
//
// They must never be the same string, and they no longer even come from the
// same field: the name is TITLE, and the prompt opens with TITLE and then
// carries WHAT whole. A name has to be short, because the slug it makes also
// names the branch, the worktree directory and the tmux session; a brief has to
// be whole, because it is the only description of the work that ever reaches
// the session.
//
// This seam was cut when both readings came off WHAT alone. Submitting named
// the feature from the capped slug and then composed the prompt from that *same
// capped value*, so a cap meant to bound three filesystem-facing names silently
// truncated the brief as well: everything past the fifth word was discarded at
// the form, before the record was written and before claude was started, and
// the sentence had no other copy. Real records still carry the prompt
// "dispatching immediate look up when", which stops mid-clause on the word the
// condition was about to follow.
//
// Splitting that field into TITLE and WHAT removes the collision at its source.
// The two returns stay: a caller that reconstructs the brief from the feature
// name is precisely the bug, and one function returning both is what stops the
// next one from trying.
func dxDispatch(title, what, goal string, auto bool) (feature, prompt string) {
	title, what = strings.TrimSpace(title), strings.TrimSpace(what)
	return dxFeatureName(title), dxPrompt(title, what, goal, auto)
}

// dxPrompt composes the prompt. what is the sentence as it was typed, in full:
// the feature name is an abbreviation of it, and abbreviating a brief is not the
// same thing as abbreviating a name. AUTO and DONE WHEN have no plumbing behind
// them — the launch command is claude with a prompt, with no permission flags
// and no watchdog — so they are instructions, and this is the only place they
// exist. Keep the copy inside what a prompt can promise.
//
// TITLE leads, on its own line, because it is the name the branch, the worktree
// and every screen file this work under: the session should know what it is
// building before it reads the brief. WHAT follows as the body.
func dxPrompt(title, what, goal string, auto bool) string {
	lines := []string{title}
	if what != "" {
		lines = append(lines, "", what)
	}
	if goal != "" {
		lines = append(lines, "", "done when: "+goal, "Keep working until that is true.")
	}
	if auto {
		lines = append(lines, "", "Commit as you go, open the PR, and fix your own CI failures without stopping to ask.")
	} else {
		lines = append(lines, "", "Do one pass, then stop and check in before committing, pushing or opening a PR.")
	}
	return strings.Join(lines, "\n")
}

// ---- keys ---------------------------------------------------------------------

// dxKey is the form's editor, and the whole key surface while it is open.
//
// Text comes from the key message's runes via typedTextFor, never from the
// key's name: bubbletea coalesces a fast burst or a paste into ONE message
// carrying every rune, and rebuilding from the name would keep only the first
// (or the brackets a paste is reported inside).
//
// The branch order is load-bearing. AUTO is tested before backspace and before
// text, so on that line space is a toggle rather than a character; and the
// text branch is last, so nothing typeable can shadow a navigation key.
func (m model) dxKey(k string) (model, tea.Cmd) {
	rows := m.dxRows()
	field := m.dxField

	switch k {
	case "esc":
		return m.dxReset(), nil

	case "ctrl+d", "ctrl+enter":
		// The design fires on ctrl+⏎, which a terminal cannot report: ctrl+m
		// arrives as a plain enter outside the kitty protocol. ctrl+d is this
		// cockpit's existing "submit from anywhere" (the dispatch overlay's
		// prompt takes it too), and it is the key the footer advertises.
		// "ctrl+enter" is accepted in case bubbletea ever does report it.
		return m.dxSubmit()

	case "tab":
		m.dxField = (field + 1) % dxFieldCount
		return m, nil

	case "shift+tab":
		m.dxField = (field + dxFieldCount - 1) % dxFieldCount
		return m, nil

	case "enter":
		if field == dxAutoF {
			return m.dxSubmit()
		}
		m.dxField = field + 1
		return m, nil

	case "down":
		if field == dxWhereF {
			m.dxRepo = mini(m.dxRepo+1, maxi(len(rows)-1, 0))
			return m, nil
		}
		m.dxField = mini2(field+1, dxAutoF)
		return m, nil

	case "up":
		if field == dxWhereF {
			m.dxRepo = maxi(m.dxRepo-1, 0)
			return m, nil
		}
		m.dxField = maxi2(field-1, dxWhereF)
		return m, nil
	}

	// AUTO is a switch, not a field: space and `a` flip it, and nothing else on
	// that line types. This has to sit above the text branch, because typedText
	// turns a space into " ".
	if field == dxAutoF {
		if k == " " || k == "space" || k == "a" {
			m.dxAuto = !m.dxAuto
		}
		return m, nil
	}

	if k == "backspace" {
		switch field {
		case dxWhereF:
			m.dxFilter, m.dxRepo = dxChop(m.dxFilter), 0
		case dxTitleF:
			m.dxTitle = dxChop(m.dxTitle)
		case dxWhatF:
			m.dxWhat = dxChop(m.dxWhat)
		case dxGoalF:
			m.dxGoal = dxChop(m.dxGoal)
		}
		return m, nil
	}

	if s, ok := typedTextFor(m.key, k); ok {
		switch field {
		case dxWhereF:
			// A narrowed list is a different list, so the cursor goes home
			// rather than pointing at whatever now sits at that index.
			m.dxFilter, m.dxRepo = m.dxFilter+s, 0
		case dxTitleF:
			m.dxTitle += s
		case dxWhatF:
			m.dxWhat += s
		case dxGoalF:
			m.dxGoal += s
		}
	}
	// Everything else is swallowed: the form owns the keyboard, and a stray
	// key must not act on the queue behind it.
	return m, nil
}

// dxChop drops the last rune of s (backspace, on a string that may hold
// multi-byte characters).
func dxChop(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// mini2/maxi2 are mini/maxi for the field id, which is its own type.
func mini2(a, b dxFieldID) dxFieldID {
	if a < b {
		return a
	}
	return b
}

func maxi2(a, b dxFieldID) dxFieldID {
	if a > b {
		return a
	}
	return b
}

// dxSubmit validates and hands off. It never closes the form on a failure: what
// the human typed is the only copy of it, and a form that vanished with the
// sentence in it would be the worst possible answer to "you forgot a field".
func (m model) dxSubmit() (model, tea.Cmd) {
	row, ok := m.dxPicked()
	if !ok {
		m.notice = "nothing matches — widen the filter"
		return m, nil
	}
	goal := strings.TrimSpace(m.dxGoal)
	feature, prompt := dxDispatch(m.dxTitle, m.dxWhat, goal, m.dxAuto)
	if feature == "" {
		// Empty *or* unslugabble: a title of nothing but punctuation passes a
		// plain "is it blank" test and then fails inside Launch, where the human
		// is no longer looking at the form.
		m.dxField, m.notice = dxTitleF, "name it — the branch is named from the title"
		return m, nil
	}
	if strings.TrimSpace(m.dxWhat) == "" {
		m.dxField = dxWhatF
		m.notice = "say what it should do — the title alone is not a brief"
		return m, nil
	}
	branch, auto := m.dxBranch(), m.dxAuto

	m = m.dxReset()
	// Present tense: nothing has launched yet. launchCmd's actionMsg replaces
	// this line with what actually happened, including "launch failed: …".
	notice := "dispatching · " + row.repo + " · " + branch
	if goal != "" {
		notice += " · runs until: " + goal
	} else {
		notice += " · one pass, then waits"
	}
	if auto {
		notice += " · auto"
	}
	m.notice = notice
	// The row goes on the table now, not when the launch comes back. The form is
	// closing over the table it was drawn on top of, and without this the human
	// watches their dispatch land on an unchanged screen — or, with nothing else
	// in flight, on a blank form, because that is what this lens shows when the
	// table is empty. fleetSync re-keys the cursor over the row that just moved
	// down; on an empty table it lands the cursor on the new row itself.
	m = m.markPending(m.pendingFor(row.repo, feature, prompt)).fleetSync()
	return m, dxLaunch(m.cfg, row.repo, feature, prompt)
}

// dxLaunch is the launch dxSubmit hands off to, named as a variable so a test
// can read what the form actually passes without starting a real dispatcher.
//
// The hand-off is where the truncated prompt got out, and it was invisible to
// every test the form had: those assert the form's own state, and the form's
// state was correct — m.dxWhat held the whole sentence the entire time. Only the
// two arguments leaving this function were wrong, and nothing looked at them.
var dxLaunch = launchCmd

// ---- view strings -------------------------------------------------------------

// dxRepoCount sits at the right of the WHERE line: how much of the portfolio
// the filter is showing.
func (m model) dxRepoCount() string {
	n, all := len(m.dxRows()), len(m.dxAllRepos())
	if n == all {
		return itoa(n) + " repos · type to filter"
	}
	return itoa(n) + " of " + itoa(all)
}

// dxRepoNone replaces the list when the filter matches nothing, and says which
// key undoes that.
func (m model) dxRepoNone() string {
	if len(m.dxRows()) == 0 {
		return "nothing matches — backspace to widen"
	}
	return ""
}

// dxTitleHint turns into the branch preview as soon as there is something to
// preview — the one place the human sees the name their history is filed under.
func (m model) dxTitleHint() string {
	if strings.TrimSpace(m.dxTitle) != "" {
		return "branch " + m.dxBranch()
	}
	return "a few words · names the branch, the worktree and the session"
}

// dxWhatHint says what WHAT is for, and — while it has the keyboard — that
// enter leaves the field. A box that grows as you type invites enter as a
// newline, and this form has no newline: enter is the field walk everywhere
// else on the screen, and it stays that here.
func (m model) dxWhatHint() string {
	if m.dxField == dxWhatF {
		return "wraps as you type · enter moves on · ctrl+d dispatches"
	}
	if strings.TrimSpace(m.dxWhat) != "" {
		return "the brief the session opens with"
	}
	return "the work itself — as long as it needs to be"
}

// dxGoalHint says what leaving DONE WHEN empty costs.
func (m model) dxGoalHint() string {
	if strings.TrimSpace(m.dxGoal) != "" {
		return "it keeps working until this is true"
	}
	return "optional · leave empty and it does one pass, then waits for you"
}

// dxAutoValue is the switch's position and its colour.
func (m model) dxAutoValue() (string, string) {
	if m.dxAuto {
		return "on", cGreen
	}
	return "off", cFaint
}

// dxAutoHint describes what the prompt will actually instruct.
func (m model) dxAutoHint() string {
	if m.dxAuto {
		return "commits, opens the pr, retries its own failures"
	}
	return "stops and asks after every step · space toggles"
}

// dxSummary is the whole form in one line: where it lands, what it will be
// called, and whether it needs you.
//
// The design also prints a model name here. This does not: the launch command
// passes no --model, so the session runs whatever the user's Claude Code
// default is, and naming one we did not choose would be a fabricated fact on
// the most-read line of the screen.
func (m model) dxSummary() string {
	row, ok := m.dxPicked()
	if !ok {
		return "pick a repo to dispatch into"
	}
	s := row.repo + " · " + m.dxBranch() + " · "
	if m.dxAuto {
		return s + "auto"
	}
	return s + "asks each step"
}

// dxFooterHelp is the chrome row while the form is open. It advertises only
// keys that exist here: ctrl+d and not the design's ctrl+⏎, and the exits only
// while they are still exits.
func (m model) dxFooterHelp() string {
	if m.dxTouched() {
		return "enter next field · tab moves · ctrl+d dispatch · esc cancel"
	}
	return "dispatch · enter next field · tab moves · esc cancel · 1…6 sections"
}

// ---- view ---------------------------------------------------------------------

const (
	dxLabelW  = 11 // the design's 11ch label column
	dxGapW    = 2  // the design's 2ch flex gap
	dxIndent  = 13 // the repo list's padding-left: label column + gap
	dxNarrowW = 60 // below this the list gives the indent back to the repo name

	// dxWhatMax is how tall WHAT may grow. Past six rows the form stops being a
	// form and the repo list has nowhere left to be; the text keeps going, and
	// the box scrolls to hold the end — the part being typed.
	dxWhatMax = 6
	// dxListFloor is how many repo rows WHAT may never take. A hidden repo is a
	// repo that cannot be picked, and unlike the text it cannot be typed back
	// into view.
	dxListFloor = 3
)

// dxView is the dispatch prompt pane: the lead, the five fields, and the line
// that says what is getting on without you. cqViewEmpty renders it.
//
// Same row discipline as the rest of the triage lens (see cq_view.go): fixed
// spacers carrying a shed order, one flex gap, and one block — the repo list —
// that shrinks before any spacer does, because whitespace is only the shape of
// the pane while a hidden repo is a repo the human cannot pick.
func (m model) dxView(w, h int) string {
	inner := cqInner(w)

	head := []cqRow{
		cqGap(3), cqGap(4),
		cqFixed(flG(fg(cDim, truncate(m.cqPromptLead(), inner)))),
		cqGap(1), cqGap(2),
		cqFixed(m.dxFieldRow(inner, "WHERE", m.dxFilter, m.dxField == dxWhereF, m.dxRepoCount())),
	}

	autoVal, autoHex := m.dxAutoValue()
	// The gap under TITLE is order 10: the last spacer to go, because the two
	// fields either side of it are the two the human types into, and a title
	// touching its brief reads as one run-on sentence.
	aboveWhat := []cqRow{
		cqGap(5),
		cqFixed(m.dxFieldRow(inner, "TITLE", m.dxTitle, m.dxField == dxTitleF, "")),
		cqFixed(dxHintRow(inner, m.dxTitleHint())),
		cqGap(10),
	}
	belowWhat := []cqRow{
		cqFixed(dxHintRow(inner, m.dxWhatHint())),
		cqGap(7),
		cqFixed(m.dxFieldRow(inner, "DONE WHEN", m.dxGoal, m.dxField == dxGoalF, "")),
		cqFixed(dxHintRow(inner, m.dxGoalHint())),
		cqGap(8),
		cqFixed(dxAutoRow(inner, m.dxField == dxAutoF, autoVal, autoHex, m.dxAutoHint())),
		cqGap(6),
		cqFixed(dxHintRow(inner, m.dxSummary())),
	}

	tail := []cqRow{
		cqFixed(flG(fg(cFaint, truncate(cqUnattendedLine(), maxi(1, inner))))),
		cqGap(9),
	}

	// WHAT takes the rows its text needs out of the height nothing else has
	// claimed — never more than dxWhatMax, and never the repo list's floor. The
	// floor is only worth reserving where there is something to stand on it:
	// with two repos matching it is two rows, and with none it is the single
	// line that says so.
	floor := mini(dxListFloor, maxi(1, len(m.dxRows())))
	fixed := len(head) + len(aboveWhat) + len(belowWhat) + len(tail)
	whatRows := m.dxWhatRows(inner, mini(dxWhatMax, maxi(1, h-fixed-floor)))

	body := make([]cqRow, 0, len(aboveWhat)+len(whatRows)+len(belowWhat))
	body = append(body, aboveWhat...)
	for _, ln := range whatRows {
		body = append(body, cqFixed(ln))
	}
	body = append(body, belowWhat...)

	// The list takes what is left after everything that must be on screen, and
	// never more rows than it has repos to show.
	list := m.dxRepoLines(inner, maxi(0, h-len(head)-len(body)-len(tail)))

	rows := make([]cqRow, 0, len(head)+len(list)+len(body)+len(tail)+1)
	rows = append(rows, head...)
	for _, ln := range list {
		rows = append(rows, cqFixed(ln))
	}
	rows = append(rows, body...)
	rows = append(rows, cqFill())
	rows = append(rows, tail...)
	return cqRender(rows, h)
}

// dxFieldRow is one labelled input line: LABEL, the typed value, the caret, and
// (on WHERE only) a right-aligned count.
func (m model) dxFieldRow(inner int, label, value string, active bool, right string) string {
	lbl := fg(dxLabelColor(active), padTo(truncate(label, dxLabelW), dxLabelW, alignLeft))
	room := inner - dxLabelW - dxGapW
	if right != "" {
		room -= dispWidth(right) + dxGapW
	}
	left := lbl + blank(dxGapW) + dxValue(value, active, room)
	if right == "" {
		return flG(left)
	}
	return flG(flSpread(left, fg(cFaint, right), inner))
}

// dxHintRow is a line in the value column: the hints under WHAT and DONE WHEN,
// and the summary.
func dxHintRow(inner int, s string) string {
	if s == "" {
		return flG("")
	}
	return flG(blank(dxIndent) + fg(cFaint, truncate(s, maxi(1, inner-dxIndent))))
}

// dxAutoRow is the AUTO switch. It has no caret: there is nothing to type into
// it, and a caret would say otherwise.
func dxAutoRow(inner int, active bool, value, hex, hint string) string {
	lbl := fg(dxLabelColor(active), padTo("AUTO", dxLabelW, alignLeft))
	room := maxi(1, inner-dxLabelW-dxGapW-dispWidth(value)-dxGapW)
	return flG(lbl + blank(dxGapW) + fg(hex, value) + blank(dxGapW) + fg(cFaint, truncate(hint, room)))
}

// dxLabelColor lifts the label of the field that owns the keyboard, which is
// the only cue the form gives about where a keystroke will land.
func dxLabelColor(active bool) string {
	if active {
		return cWhite
	}
	return cFaint
}

// dxValue is a field's text with the block caret after it.
//
// The caret is steady. The design blinks it, but the cockpit has no blink tick
// and adding one would cost a full redraw twice a second for a cursor the
// terminal already draws. When the value outgrows its room it scrolls left,
// keeping the caret — the point of interest — on screen. An inactive field
// keeps the caret's column as a space, so the fields stay in line.
func dxValue(s string, active bool, room int) string {
	if room < 1 {
		return ""
	}
	r := []rune(s)
	for len(r) > 0 && dispWidth(string(r))+1 > room {
		r = r[1:]
	}
	if !active {
		return fg(cFg, string(r)) + " "
	}
	return fg(cFg, string(r)) + paint(cSurface, cFg, " ")
}

// dxWhatRows is WHAT: the label, then the typed text wrapped down the value
// column for at most max rows. Continuation rows line up under the first one,
// so the block reads as one paragraph in the column the other fields use.
func (m model) dxWhatRows(inner, max int) []string {
	active := m.dxField == dxWhatF
	lbl := fg(dxLabelColor(active), padTo("WHAT", dxLabelW, alignLeft))
	vals := dxWrapValue(m.dxWhat, active, inner-dxLabelW-dxGapW, max)
	if len(vals) == 0 {
		return []string{flG(lbl)}
	}
	rows := make([]string, 0, len(vals))
	for i, v := range vals {
		if i == 0 {
			rows = append(rows, flG(lbl+blank(dxGapW)+v))
			continue
		}
		rows = append(rows, flG(blank(dxIndent)+v))
	}
	return rows
}

// dxWrapValue is WHAT's text laid out down the value column with the block
// caret after it.
//
// It wraps where the other fields scroll (dxValue), because WHAT is the one
// field that holds a paragraph: scrolling it left would leave the human typing
// a brief through a letterbox, able to see only the last few words of what they
// wrote. When the text outgrows max rows the top goes rather than the bottom —
// the caret is where the work is.
func dxWrapValue(s string, active bool, room, max int) []string {
	if room < 2 || max < 1 {
		return nil
	}
	// Trailing spaces are the caret's, not the wrapper's: strings.Fields eats
	// them, and then pressing space would not move the caret.
	body := strings.TrimRight(s, " ")
	tail := s[len(body):]
	if len(tail) > room-1 {
		tail = tail[:room-1]
	}
	lines := dxWrap(body, room)
	// The caret needs a cell of its own. When the last line has no room left for
	// it, it starts the next one rather than hanging outside the column.
	if dispWidth(lines[len(lines)-1])+len(tail)+1 > room {
		lines = append(lines, "")
	}
	lines[len(lines)-1] += tail
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}

	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = fg(cFg, ln)
	}
	// An inactive field keeps the caret's column as a space, so the fields stay
	// in line whichever one owns the keyboard.
	if active {
		out[len(out)-1] += paint(cSurface, cFg, " ")
	} else {
		out[len(out)-1] += " "
	}
	return out
}

// dxWrap greedily word-wraps s to w columns, always returning at least one
// line. A single word too long for the column is broken rather than allowed to
// overhang: this is a text field, and text that runs outside its column would
// paint over the pane beside it.
func dxWrap(s string, w int) []string {
	if w < 1 {
		return []string{""}
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		for dispWidth(word) > w {
			if cur != "" {
				lines, cur = append(lines, cur), ""
			}
			head := dxCut(word, w)
			lines = append(lines, head)
			word = word[len(head):]
		}
		switch {
		case cur == "":
			cur = word
		case dispWidth(cur)+1+dispWidth(word) <= w:
			cur += " " + word
		default:
			lines, cur = append(lines, cur), word
		}
	}
	if cur != "" || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}

// dxCut is the first w columns of s, cut on a rune boundary. Unlike
// truncate it adds no ellipsis: the rest of the word is about to be printed on
// the next line.
func dxCut(s string, w int) string {
	out := ""
	for _, r := range s {
		if dispWidth(out+string(r)) > w {
			break
		}
		out += string(r)
	}
	if out == "" {
		// A single rune wider than the column: emit it anyway, or the caller
		// loops forever on a word it can never shorten.
		for _, r := range s {
			return string(r)
		}
	}
	return out
}

// dxRepoLines is the filtered repo list, windowed so the cursor stays on
// screen. room is the height it may use; with none it renders nothing, which is
// honest on a very short terminal — the count on the WHERE line still says how
// many repos matched.
func (m model) dxRepoLines(inner, room int) []string {
	if room < 1 {
		return nil
	}
	rows := m.dxRows()
	if len(rows) == 0 {
		return []string{dxHintRow(inner, m.dxRepoNone())}
	}
	sel := clampCursor(m.dxRepo, len(rows))
	start, end := window(sel, len(rows), room)
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, dxRepoLine(inner, rows[i], i == sel))
	}
	return out
}

// dxRepoLine is one candidate repo.
//
// Same one-flex-column discipline as the other lists (see cqRestRow): every
// proportional column is a fixed cell and exactly one column flexes, so the row
// stays column-exact. The columns shed right to left as the pane narrows —
// commit age first, then the in-flight count — because the repo name is the
// thing being picked and the product is what it is scanned by.
func dxRepoLine(inner int, r dxRepoRow, on bool) string {
	indent := dxIndent
	if inner < dxNarrowW {
		indent = pad
	}
	width := maxi(1, inner-indent)

	bg, mark, nameHex := cTransparent, " ", cMid
	if on {
		bg, mark, nameHex = cSel, "▸", cWhite
	}
	// The count is blank rather than "0 out" when nothing is running there, and
	// blank again when the pre-snapshot fallback simply does not know.
	out := ""
	if r.out > 0 {
		out = itoa(r.out) + " out"
	}

	segs := []seg{c(mark, 2, cMid), c("", dxGapW, "")}
	if width >= 44 {
		segs = append(segs, c(truncate(r.product, 14), 14, cFaint), c("", dxGapW, ""))
	}
	segs = append(segs, flexc(r.repo, nameHex))
	if width >= 40 {
		segs = append(segs, c("", dxGapW, ""), cr(out, 9, cFaint))
	}
	if width >= 56 {
		segs = append(segs, c("", dxGapW, ""), cr(r.last, 10, cFaint))
	}
	return flG(blank(indent) + row(width, bg, segs...))
}

// dxFeatureName is the name the dispatch is filed under: the title, as typed.
// It is empty when the title cannot become a branch at all, which is what
// dxSubmit refuses on.
//
// It used to read WHAT back through the slug — a 75-word name, a 91-character
// slug and a 96-character tmux session were what a pasted paragraph produced
// otherwise — and that round-trip is what made the name and the brief one
// string, which is the whole of the truncation dxDispatch describes. A title is
// already short, and Slugify caps the slug at dispatch.SlugWords, so the name
// is kept as it was written: the words the human chose, not the words that
// survived being turned into a path.
func dxFeatureName(title string) string {
	title = strings.TrimSpace(title)
	if dispatchpkg.Slugify(title) == "" {
		return ""
	}
	return title
}
