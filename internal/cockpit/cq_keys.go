package cockpit

// cq_keys.go is the triage lens's whole interaction: the derived queue the view
// reads, and the key state machine that drives it. It replaces the v2 floor's
// list/detail navigation entirely — there is no cursor, no filter and no marks,
// because there is no list to move through. You act on the item at the head,
// skip it, or type the next dispatch.
//
// The queue itself is never stored. cq.go rebuilds cqItems from the real
// records on every poll and every state-file change, so anything the model kept
// would be stale within seconds. What the model does own is what the human did
// to it: the order they left it in, what they have already acted on, and what
// they are typing.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// cqUndoEntry is the last row `u` can put back. It restores the queue row only:
// the act's command has already run, so `u` un-hides an item, it does not
// un-kill a session or un-merge a PR.
type cqUndoEntry struct{ id, label string }

// ---- derived state ----------------------------------------------------------

// cqQueue is the live queue in display order: the ids the user has arranged
// first, then anything that has arrived since, in the collector's urgency
// order. Items acted on this session are suppressed until their record actually
// leaves the queue — the kill or the merge takes a moment to land, and without
// this the row the user just cleared would reappear on the next 5s refresh.
func (m model) cqQueue() []cqItem {
	byID := make(map[string]cqItem, len(cqItems))
	for _, it := range cqItems {
		byID[it.id] = it
	}
	out := make([]cqItem, 0, len(cqItems))
	placed := make(map[string]bool, len(cqItems))
	for _, id := range m.cqOrder {
		if it, ok := byID[id]; ok && !placed[id] && !m.cqSuppressed[id] {
			out = append(out, it)
			placed[id] = true
		}
	}
	for _, it := range cqItems {
		if !placed[it.id] && !m.cqSuppressed[it.id] {
			out = append(out, it)
		}
	}
	return out
}

// cqCurrent is the ask under the cursor: the head of the queue, or false when
// there is nothing left to answer.
func (m model) cqCurrent() (cqItem, bool) {
	q := m.cqQueue()
	if len(q) == 0 {
		return cqItem{}, false
	}
	return q[0], true
}

// cqPromptOn reports whether the dispatch form owns the keyboard — either
// because `d` opened it, or because a clear queue leaves nothing else to do.
func (m model) cqPromptOn() bool {
	return m.cqFlash == "" && !m.cqWork && (m.cqDispatch || len(m.cqQueue()) == 0)
}

// snapPanes returns both scroll panes to the top when the head of the queue has
// changed. An offset is a position inside one item's diff and one queue tail;
// carried onto the next ask it would open the pane part-way down a document the
// human has not seen the start of. Nothing moves while the head is the same, so
// a poll that changed nothing leaves the reader where they were.
func (m model) snapPanes() model {
	id := ""
	if it, ok := m.cqCurrent(); ok {
		id = it.id
	}
	if id != m.cqHeadID {
		m.cqHeadID, m.cqEvScroll, m.cqRestScroll = id, 0, 0
	}
	return m
}

// cqReconcile folds a fresh snapshot into the user's ordering: ids that have
// left the queue are dropped from the order, from the suppressed set — which is
// what bounds it — and from the undo, which can no longer put anything back.
func (m model) cqReconcile() model {
	live := make(map[string]bool, len(cqItems))
	for _, it := range cqItems {
		live[it.id] = true
	}
	order := make([]string, 0, len(m.cqOrder))
	for _, id := range m.cqOrder {
		if live[id] {
			order = append(order, id)
		}
	}
	m.cqOrder = order
	for id := range m.cqSuppressed {
		if !live[id] {
			delete(m.cqSuppressed, id)
		}
	}
	if m.cqUndo != nil && !live[m.cqUndo.id] {
		m.cqUndo = nil
	}
	return m
}

// ---- acting -----------------------------------------------------------------

// cqRun runs the act behind a key. The queue offers only acts with a real
// command behind them (see cqActs), so anything unrecognised here is a display
// row and does nothing.
func (m model) cqRun(it cqItem, a cqAct) (model, tea.Cmd) {
	switch a.k {
	case "⏎":
		return m.attach(it.title)
	case "y":
		// On a review item y means "merge it", which is a real squash-merge on
		// the forge; everywhere else it only marks the record done.
		if it.kind == "review" {
			return m, shipCmd(it.title)
		}
		return m, markDoneCmd(it.title)
	case "x":
		return m, killCmd([]string{it.title})
	}
	return m, nil
}

// cqStartFlash holds an act's confirmation on screen for cqFlashLinger.
//
// The command fires now, not when the flash expires: a tick can be superseded
// or dropped, and an action the human has been told ran must never turn out not
// to have. The real notice from actionMsg lands after the flash and replaces
// it, including failures like "merge failed: …".
func (m model) cqStartFlash(it cqItem, a cqAct) (model, tea.Cmd) {
	m.cqFlashSeq++
	seq := m.cqFlashSeq
	m.cqFlash, m.cqFlashKeep, m.cqFlashID = a.ok, a.keep, it.id
	mm, run := m.cqRun(it, a)
	return mm, tea.Batch(run, tea.Tick(cqFlashLinger, func(time.Time) tea.Msg {
		return cqFlashMsg{seq: seq}
	}))
}

// cqFlashDone ends the confirmation: unless the act keeps the item, it leaves
// the queue, the handled count goes up and `u` can put it back. It clears by
// id, never by position, so a refresh that reordered the queue mid-flash cannot
// clear the wrong row.
func (m model) cqFlashDone() (model, tea.Cmd) {
	id, keep := m.cqFlashID, m.cqFlashKeep
	label := m.cqFlash
	m.cqFlash, m.cqFlashKeep, m.cqFlashID = "", false, ""
	m.cqFlashSeq++ // retire the generation; any late tick is now stale
	if keep {
		return m, nil
	}
	m.cqOrder = cqWithout(m.cqOrder, id)
	m.cqSuppressed[id] = true
	m.cqCleared++
	m.cqUndo = &cqUndoEntry{id: id, label: label}
	return m, nil
}

func cqWithout(ids []string, drop string) []string {
	out := ids[:0:0]
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

// ---- the key state machine ---------------------------------------------------

// isLensDigit reports whether k selects one of the eight lenses.
func isLensDigit(k string) bool { return len(k) == 1 && k[0] >= '1' && k[0] <= '8' }

// updateFloorQueue is the triage lens's whole key surface. handled is false only
// for the keys allowed to leave this screen (1–8, ':', 'u', '?', 'q'), which
// handleKey then routes as usual; nothing else escapes, because the v2 list keys
// (/, space, F, D, tab, r, t, M, p) went with the list. j/k survived, but they
// scroll a pane now rather than move a cursor through rows.
func (m model) updateFloorQueue(k string) (model, tea.Cmd, bool) {
	// A flash is a promise that something happened. Nothing gets through it.
	if m.cqFlash != "" {
		return m, nil, true
	}

	if m.cqPromptOn() {
		// An untouched form is not a trap: with nothing typed, the navigation
		// keys still reach their handlers. The moment there is a filter or a
		// sentence they are letters again — `w` belongs in a sentence more often
		// than it means "working".
		navKey := isLensDigit(k) || k == ":" || k == "w"
		if m.dxTouched() || !navKey {
			mm, cmd := m.dxKey(k)
			return mm, cmd, true
		}
	}

	// `w` shows what is running unattended. cqDispatch is deliberately left
	// alone: cqPromptOn also requires !cqWork, so `w` hides the prompt and a
	// second `w` brings back exactly the draft you left.
	if k == "w" {
		m.cqWork = !m.cqWork
		m.cqWorkCursor = 0
		return m, nil, true
	}
	if m.cqWork {
		flat := cqWorkFlat()
		switch k {
		case "esc":
			m.cqWork = false
			return m, nil, true
		case "j", "down":
			m.cqWorkCursor = mini(m.cqWorkCursor+1, maxi(len(flat)-1, 0))
			return m, nil, true
		case "k", "up":
			m.cqWorkCursor = maxi(m.cqWorkCursor-1, 0)
			return m, nil, true
		case "enter":
			if len(flat) > 0 {
				mm, cmd := m.attach(flat[clampCursor(m.cqWorkCursor, len(flat))].feature)
				return mm, cmd, true
			}
			return m, nil, true
		case "x":
			if len(flat) > 0 {
				feat := flat[clampCursor(m.cqWorkCursor, len(flat))].feature
				m.notice = "killing \"" + feat + "\"…"
				return m, killCmd([]string{feat}), true
			}
			return m, nil, true
		}
		if !isLensDigit(k) && k != ":" {
			return m, nil, true
		}
	}

	// The item view's two scroll panes. They sit here — under the working view's
	// own j/k and over `d` — exactly as the design's handler orders them, so a
	// key means one thing per mode.
	//
	// Both clamp at the bottom of what is actually showable, not at the line
	// count: a pane runs out of content a screenful before it runs out of lines,
	// and an offset allowed past that would leave the next several k presses
	// doing nothing visible.
	if _, ok := m.cqCurrent(); ok {
		switch k {
		case "j", "down", "k", "up", "J", "K":
			evMax, restMax := m.cqScrollMax()
			switch k {
			case "j", "down":
				m.cqEvScroll = mini(m.cqEvScroll+1, evMax)
			case "k", "up":
				m.cqEvScroll = maxi(mini(m.cqEvScroll, evMax)-1, 0)
			case "J":
				m.cqRestScroll = mini(m.cqRestScroll+1, restMax)
			case "K":
				m.cqRestScroll = maxi(mini(m.cqRestScroll, restMax)-1, 0)
			}
			return m, nil, true
		}
	}

	if k == "d" {
		return m.dxOpen(""), nil, true
	}

	if it, ok := m.cqCurrent(); ok {
		if k == "s" {
			// Skip rotates the head to the back. The order has to be seeded from
			// the queue as displayed, or the rotation would be relative to an
			// order the human never saw.
			q := m.cqQueue()
			ids := make([]string, 0, len(q))
			for _, x := range q[1:] {
				ids = append(ids, x.id)
			}
			m.cqOrder = append(ids, q[0].id)
			return m, nil, true
		}
		for _, a := range it.acts {
			if a.ok == "" || !cqActKeyMatches(a.k, k) {
				continue
			}
			mm, cmd := m.cqStartFlash(it, a)
			return mm, cmd, true
		}
	}

	// Only navigation leaves this screen. ',', '+', 'tab' and every v2 list key
	// are swallowed: the palette is the way to settings and to a repo-first
	// dispatch, and the prompt is the way to a new one.
	if isLensDigit(k) || k == ":" || k == "u" || k == "?" || k == "q" {
		return m, nil, false
	}
	return m, nil, true
}

// cqActKeyMatches maps a terminal key onto an act's key. The design writes the
// attach act as ⏎, which arrives as "enter".
func cqActKeyMatches(actKey, k string) bool {
	return actKey == k || (actKey == "⏎" && k == "enter")
}
