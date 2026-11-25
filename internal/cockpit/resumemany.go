package cockpit

// resumemany.go is R on the triage table: several finished dispatchers
// reopened at once, nothing attached (ADR 0036).
//
// ⏎ on a finished row resumes it and hands the terminal to it, which is right
// for the one dispatcher you want to look at. It is the wrong answer for ten a
// reboot took down together, where the ten resumes each cost a jump in and a
// way back out. So `space` marks the rows ⏎ would resume, and R resumes every
// marked one (or the row under the cursor, with nothing marked) through
// dispatch.ResumeMany, after one line the human may type to say to each of
// them — "continue" is the usual one. The rows turn as each one lands, the
// footer counts them in, and the marks are the record of the outcome: a
// dispatcher that came back loses its mark, and one that did not keeps it and
// carries the reason on its row, because the footer has one line and a batch
// can fail more ways than that.

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// manyTarget is one dispatcher R is about, held from the moment the input
// opened, as replyTarget and parkTarget are: the table rebuilds underneath it,
// and a target re-read from the marks could resume what the human never saw
// listed.
type manyTarget struct {
	id, feature, repo string
	// fresh is a launch that never ran: ResumeMany starts it again with its
	// brief, and any line typed goes after that brief rather than being said
	// to a conversation.
	fresh bool
}

// manyRun is a batch under way: how many it was asked to bring back and what
// has come of each so far. seq ties its messages to it.
type manyRun struct {
	seq    int
	total  int
	landed int
	back   int // resumed or started again
	live   int // its claude was still running, so nothing was started
	failed []string
	// said is whether a line went with the batch, which a dispatcher found
	// still running was not given.
	said bool
}

// manyResultMsg is one dispatcher's outcome, carrying the channel the rest
// arrive on; manyDoneMsg says the channel is drained.
type manyResultMsg struct {
	seq int
	res dispatchpkg.ResumeResult
	ch  <-chan dispatchpkg.ResumeResult
}

type manyDoneMsg struct{ seq int }

// cqResumesOn is whether act a on row r resumes it rather than attaching: ⏎
// on a finished row, held or history, or on a parked one whose session is gone
// — cqActs has already decided which from the record, so the act's own verb
// says. It is the rule cqRun's ⏎ follows and the marks ask (cqResumable), so a
// row can be marked exactly when its own ⏎ would resume it.
func cqResumesOn(r fleetRow, a cqAct) bool {
	if isPendingID(r.id) || a.k != "⏎" {
		return false
	}
	switch r.kind {
	case "past", "done":
		return true
	case "parked":
		return a.d == "resume"
	}
	return false
}

// cqResumable is whether the row's own ⏎ resumes it.
func cqResumable(r fleetRow) bool {
	for _, a := range r.acts {
		if a.k == "⏎" {
			return cqResumesOn(r, a)
		}
	}
	return false
}

// fleetMark toggles the mark on the row under the cursor. A row ⏎ would attach
// to is not marked, and the notice says why: marking a running dispatcher for a
// resume would promise something R cannot do with it.
func (m model) fleetMark() model {
	r, ok := m.fleetSel()
	if !ok {
		return m
	}
	if !cqResumable(r) {
		m.notice = "\"" + r.feature + "\" is still running — only a dispatcher whose session is over can be marked to resume"
		return m
	}
	if m.fleetMarked == nil {
		m.fleetMarked = map[string]bool{}
	}
	if m.fleetMarked[r.id] {
		delete(m.fleetMarked, r.id)
		delete(m.manyFailed, r.id)
	} else {
		m.fleetMarked[r.id] = true
	}
	return m
}

// fleetUnmarkAll drops every mark, and with them the reasons the last batch
// left on the rows it could not bring back.
func (m model) fleetUnmarkAll() model {
	if len(m.fleetMarked) == 0 {
		return m
	}
	m.fleetMarked, m.manyFailed = map[string]bool{}, nil
	m.notice = "marks cleared"
	return m
}

// manyTargets is what R would act on: every marked row that can still be
// resumed, in the order the tables draw them — the live table, then history —
// or, with nothing marked, the row under the cursor.
func (m model) manyTargets() []manyTarget {
	var out []manyTarget
	add := func(r fleetRow) {
		rec := recordByID(r.id)
		out = append(out, manyTarget{
			id: r.id, feature: r.feature, repo: r.repo,
			fresh: rec != nil && dispatchpkg.NeverStarted(rec),
		})
	}
	if len(m.fleetMarked) > 0 {
		for _, rows := range [][]fleetRow{m.fleetAll(), m.fleetPast()} {
			for _, r := range rows {
				if m.fleetMarked[r.id] && cqResumable(r) {
					add(r)
				}
			}
		}
		return out
	}
	if r, ok := m.fleetSel(); ok && cqResumable(r) {
		add(r)
	}
	return out
}

// manyOpenFor opens the line R asks for before it resumes anything.
func (m model) manyOpenFor() model {
	if m.manyRun != nil {
		m.notice = "a resume is still under way · " + manyProgress(m.manyRun)
		return m
	}
	t := m.manyTargets()
	if len(t) == 0 {
		m.notice = "nothing to resume — space marks a dispatcher whose session is over, R resumes the marked ones"
		return m
	}
	m.manyOpen, m.manyText, m.manyAt = true, "", t
	return m
}

// updateMany drives the input while it owns the keyboard. Enter with nothing
// typed is a real answer — reopen them and say nothing — as it is on the
// product panel's resume.
func (m model) updateMany(k string) (model, tea.Cmd) {
	next, submit, cancel := applyEdit(m.manyText, k, m.key)
	if cancel {
		m.manyOpen, m.manyText, m.manyAt = false, "", nil
		return m, nil
	}
	if !submit {
		m.manyText = next
		return m, nil
	}
	targets, text := m.manyAt, strings.TrimSpace(m.manyText)
	m.manyOpen, m.manyText, m.manyAt = false, "", nil
	if len(targets) == 0 {
		return m, nil
	}
	m.manySeq++
	m.manyRun = &manyRun{seq: m.manySeq, total: len(targets), said: text != ""}
	for _, t := range targets {
		delete(m.manyFailed, t.id)
	}
	m.notice = manyProgress(m.manyRun)
	return m, manyStartCmd(m.manySeq, targets, text)
}

// manyStartCmd runs the batch behind the cockpit and hands back the first
// outcome; manyNext hands back each one after. Outcomes arrive as they land,
// so a row turns when its own session is up rather than when the slowest of
// them is.
//
// It resumes the records as they are on disk when it starts, never the
// snapshot's: a load can be older than a resume that has landed since, and
// Resume decides what to do from the record it is handed. They are also then
// the batch's own, so the session Resume writes onto each is not written into
// a record the screen is drawing.
func manyStartCmd(seq int, targets []manyTarget, text string) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan dispatchpkg.ResumeResult, len(targets))
		go func() {
			defer close(ch)
			byID := map[string]*state.Dispatch{}
			for _, d := range state.LoadAll() {
				byID[d.ID] = d
			}
			recs := make([]*state.Dispatch, 0, len(targets))
			for _, t := range targets {
				if d := byID[t.id]; d != nil {
					recs = append(recs, d)
					continue
				}
				ch <- dispatchpkg.ResumeResult{ID: t.id, Feature: t.feature, Err: errors.New("its record is gone")}
			}
			resumeManyRun(recs, text, func(r dispatchpkg.ResumeResult) { ch <- r })
		}()
		return manyNext(seq, ch)()
	}
}

// resumeManyRun is dispatch.ResumeMany, as a seam for tests.
var resumeManyRun = dispatchpkg.ResumeMany

func manyNext(seq int, ch <-chan dispatchpkg.ResumeResult) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return manyDoneMsg{seq: seq}
		}
		return manyResultMsg{seq: seq, res: r, ch: ch}
	}
}

// onManyResult folds one outcome in: the mark comes off a dispatcher that is
// back (or was never gone), and stays, with the reason, on one that is not.
// Each outcome asks for a load, so its row turns now; load.go collapses the
// requests, so ten outcomes are not ten loads.
func (m model) onManyResult(msg manyResultMsg) (model, tea.Cmd) {
	next := manyNext(msg.seq, msg.ch)
	run := m.manyRun
	if run == nil || run.seq != msg.seq {
		return m, next
	}
	r := msg.res
	run.landed++
	switch {
	case r.Err != nil:
		cause := firstLine(r.Err.Error())
		run.failed = append(run.failed, "\""+r.Feature+"\": "+cause)
		if m.manyFailed == nil {
			m.manyFailed = map[string]string{}
		}
		m.manyFailed[r.ID] = cause
	case r.Mode == dispatchpkg.ResumeLive:
		run.live++
		delete(m.fleetMarked, r.ID)
	default:
		run.back++
		delete(m.fleetMarked, r.ID)
	}
	m.notice = manyProgress(run)
	mm, load := m.requestLoad(loadPlain)
	return mm, tea.Batch(load, next)
}

// onManyDone says how the batch went, in the order the human needs it: what
// came back, what was already running, and what did not — which is still
// marked, so the footer's one line is never the only account of it.
func (m model) onManyDone(msg manyDoneMsg) (model, tea.Cmd) {
	run := m.manyRun
	if run == nil || run.seq != msg.seq {
		return m, nil
	}
	m.manyRun = nil
	m.notice = manySummary(run)
	return m.requestLoad(loadPlain)
}

// manyProgress is the footer while a batch runs.
func manyProgress(run *manyRun) string {
	s := "resuming " + itoa(run.total) + " · " + itoa(run.landed) + " landed"
	if n := len(run.failed); n > 0 {
		s += " · " + itoa(n) + " did not resume"
	}
	return s + "…"
}

// manySummary is the footer once the batch is over.
func manySummary(run *manyRun) string {
	var parts []string
	if run.back > 0 || run.back+run.live+len(run.failed) == 0 {
		parts = append(parts, "resumed "+itoa(run.back)+" of "+itoa(run.total))
	}
	if run.live > 0 {
		clause := itoa(run.live) + " still running"
		if run.said {
			clause += ", the line not sent to " + plural(run.live, "it", "them")
		}
		parts = append(parts, clause)
	}
	if n := len(run.failed); n > 0 {
		parts = append(parts, itoa(n)+" did not and "+plural(n, "is", "are")+" still marked · "+
			strings.Join(run.failed, " · "))
	}
	return strings.Join(parts, " · ")
}

// manyMarkedCount is how many marks R would act on now: those still on a row
// that can be resumed.
func (m model) manyMarkedCount() int {
	if len(m.fleetMarked) == 0 {
		return 0
	}
	n := 0
	for _, rows := range [][]fleetRow{m.fleetAll(), m.fleetPast()} {
		for _, r := range rows {
			if m.fleetMarked[r.id] && cqResumable(r) {
				n++
			}
		}
	}
	return n
}

// manyPrune drops the marks on rows that can no longer be resumed — resumed
// some other way, or gone — and their reasons with them. Called as each
// snapshot lands, by cqReconcile.
func (m model) manyPrune() model {
	if len(m.fleetMarked) == 0 {
		return m
	}
	ok := make(map[string]bool, len(fleet))
	for _, r := range fleet {
		if cqResumable(r) {
			ok[r.id] = true
		}
	}
	for id := range m.fleetMarked {
		if !ok[id] {
			delete(m.fleetMarked, id)
			delete(m.manyFailed, id)
		}
	}
	return m
}

// viewMany is the input: how many, which ones, what will happen, and the line.
func (m model) viewMany(w, h int) string {
	cw := w - 2*pad
	if cw < 10 {
		cw = w
	}
	t := m.manyAt
	var out []string
	lead := "resume " + itoa(len(t)) + " dispatchers"
	if len(t) == 1 {
		lead = "resume this dispatcher"
	}
	out = append(out, fg(cDim, lead))
	// As many names as the screen has room for beside the rest of the input,
	// and a count of the others: the list is a check of what is about to
	// start, not the place to read every one.
	room := maxi(1, h-8)
	for i, x := range t {
		if i == room && len(t) > room+1 {
			out = append(out, fg(cFaint, "+ "+itoa(len(t)-room)+" more"))
			break
		}
		tail := "  " + x.repo
		if x.fresh {
			tail += " · never ran, starts again with its brief"
		}
		tail = truncate(tail, maxi(0, cw-4))
		name := truncate(x.feature, maxi(1, cw-dispWidth(tail)))
		out = append(out, fg(cWhite, name)+fg(cFaint, tail))
	}
	explain := "each reopens in its own worktree, on the account it ran on, and nothing is attached — " +
		"the rows show them coming back. Type a line to say to each one, or leave it empty to just reopen them."
	for _, ln := range productWrap(explain, cw) {
		out = append(out, fg(cFaint, ln))
	}
	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	out = append(out, fg(cAmber, "say ")+fg(cWhite, m.manyText+"▏"))
	out = append(out, fg(cFaint, "enter resumes them · esc cancels"))
	return clampLines(gutter(strings.Join(out, "\n"), pad), h)
}

// manyFooter is what the footer says about marks: the key that marks the row
// under the cursor when it can take one, and, once anything is marked, what R
// will do and how to let go of them. Keys are named from the keymap, so a
// rebind shows here.
func (m model) manyFooter() []string {
	var out []string
	if r, ok := m.fleetSel(); ok && cqResumable(r) {
		verb := "mark"
		if m.fleetMarked[r.id] {
			verb = "unmark"
		}
		out = append(out, prettyKey(m.keys.KeyFor("fleet.mark"))+" "+verb)
	}
	if n := m.manyMarkedCount(); n > 0 {
		out = append(out,
			prettyKey(m.keys.KeyFor("fleet.resume"))+" resume "+itoa(n)+" marked",
			prettyKey(m.keys.KeyFor("fleet.unmark"))+" clear marks")
	}
	return out
}
