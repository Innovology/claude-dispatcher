package cockpit

// cq_keys.go is the triage lens's whole interaction: the cursor over the fleet
// table, the filter, and the acts that answer the row under it.
//
// The table itself is never stored. fleet.go rebuilds it from the real records
// on every poll and every state-file change, so anything the model kept would
// be stale within seconds. What the model does own is what the human did to it:
// which row they are on, the order they left it in, what they have already
// acted on, what they are filtering by, and what they are typing.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// cqUndoEntry is the last row `u` can put back. It restores the table row only:
// the act's command has already run, so `u` un-hides a dispatcher, it does not
// un-kill a session or un-merge a PR.
type cqUndoEntry struct{ id, label string }

// ---- derived state ----------------------------------------------------------

// cqPromptOn reports whether the dispatch form owns the keyboard — either
// because `d` opened it, or because nothing is in flight and there is nothing
// else to do.
func (m model) cqPromptOn() bool {
	return m.cqFlash == "" && (m.cqDispatch || len(m.fleetAll()) == 0)
}

// fleetSync keeps the cursor on the row it was on.
//
// This is a correctness requirement the design does not have: it never reloads,
// while this cockpit rebuilds the fleet on every 5s poll and every fsnotify
// event, and a rank that changed under a cursor held by index alone would move
// the selection under the reader's hands — straight onto a row `x` would kill.
// The id is authoritative; the index is only the fallback for a row that has
// genuinely left the table.
func (m model) fleetSync() model {
	rows := m.fleetRows()
	if len(rows) == 0 {
		m.fleetCursor, m.fleetSelID = 0, ""
		return m
	}
	for i, r := range rows {
		if m.fleetSelID != "" && r.id == m.fleetSelID {
			m.fleetCursor = i
			return m
		}
	}
	m.fleetCursor = clampCursor(m.fleetCursor, len(rows))
	m.fleetSelID = rows[m.fleetCursor].id
	return m
}

// fleetTo moves the cursor to an absolute row and re-keys it to that row's id.
func (m model) fleetTo(i int) model {
	rows := m.fleetRows()
	if len(rows) == 0 {
		m.fleetCursor, m.fleetSelID = 0, ""
		return m
	}
	m.fleetCursor = clampCursor(i, len(rows))
	m.fleetSelID = rows[m.fleetCursor].id
	return m
}

// fleetSetFilter switches what the table shows and starts again at the top: the
// first row of a narrowed table is the most urgent thing in it, which is the
// reason for narrowing.
func (m model) fleetSetFilter(f string) model {
	m.cqFilter = f
	m.fleetSelID = ""
	return m.fleetTo(0)
}

// fleetNextFilter is the cycle `f` walks, wrapping back to "all".
func fleetNextFilter(cur string) string {
	for i, f := range fleetFilters {
		if f == cur {
			return fleetFilters[(i+1)%len(fleetFilters)]
		}
	}
	return fleetFilters[0]
}

// cqReconcile folds a fresh snapshot into the user's ordering: ids that have
// left the fleet are dropped from the order, from the suppressed set — which is
// what bounds it — and from the undo, which can no longer put anything back.
func (m model) cqReconcile() model {
	live := make(map[string]bool, len(fleet))
	for _, r := range fleet {
		live[r.id] = true
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

// cqRun runs the act behind a key. The table offers only acts with a real
// command behind them (see cqActs), so anything unrecognised here is a display
// row and does nothing.
func (m model) cqRun(r fleetRow, a cqAct) (model, tea.Cmd) {
	switch a.k {
	case "⏎":
		return m.attach(r.feature)
	case "y":
		// On a review row y means "merge it", which is a real squash-merge on
		// the forge; everywhere else it only marks the record done.
		if r.ask == "review" {
			return m, shipCmd(r.feature)
		}
		return m, markDoneCmd(r.feature)
	case "x":
		return m, killCmd([]string{r.feature})
	}
	return m, nil
}

// cqStartFlash holds an act's confirmation on screen for cqFlashLinger.
//
// The command fires now, not when the flash expires: a tick can be superseded
// or dropped, and an action the human has been told ran must never turn out not
// to have. The real notice from actionMsg lands after the flash and replaces
// it, including failures like "merge failed: …".
func (m model) cqStartFlash(r fleetRow, a cqAct) (model, tea.Cmd) {
	m.cqFlashSeq++
	seq := m.cqFlashSeq
	m.cqFlash, m.cqFlashKeep, m.cqFlashID = a.ok, a.keep, r.id
	mm, run := m.cqRun(r, a)
	return mm, tea.Batch(run, tea.Tick(cqFlashLinger, func(time.Time) tea.Msg {
		return cqFlashMsg{seq: seq}
	}))
}

// cqFlashDone ends the confirmation: unless the act keeps the row, it leaves
// the table, the handled count goes up and `u` can put it back. It clears by
// id, never by position, so a refresh that reordered the table mid-flash cannot
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
	// The row under the cursor has just gone; re-key onto whatever slid up into
	// its place rather than following the departed id.
	m.fleetSelID = ""
	return m.fleetSync(), nil
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

// isLensDigit reports whether k selects one of the lenses.
func isLensDigit(k string) bool { return len(k) == 1 && k[0] >= '1' && k[0] <= '6' }

// updateFloorQueue is the triage lens's whole key surface. handled is false only
// for the keys allowed to leave this screen (the lens digits, ':', 'u', '?',
// 'q'), which handleKey then routes as usual; nothing else escapes, because the
// v2 list keys (/, space, F, D, tab, r, t, M, p) went with the list.
func (m model) updateFloorQueue(k string) (model, tea.Cmd, bool) {
	// A flash is a promise that something happened. Nothing gets through it.
	if m.cqFlash != "" {
		return m, nil, true
	}

	if m.cqPromptOn() {
		// An untouched form is not a trap: with nothing typed, the navigation
		// keys still reach their handlers. The moment there is a filter or a
		// sentence they are letters again — `w` belongs in a sentence more often
		// than it means "running".
		//
		// `d` is in that set because it is the key that OPENS this form, and the
		// footer advertises it. Swallowing it typed a letter into the repo
		// filter instead: press d twice, or press it at all with nothing in
		// flight (where the form is already up), and the repo list silently
		// narrowed to the repos containing "d" while nothing appeared to happen.
		// Falling through re-opens the form, which on an untouched one is a no-op.
		navKey := isLensDigit(k) || k == ":" || k == "d"
		if m.dxTouched() || !navKey {
			mm, cmd := m.dxKey(k)
			return mm, cmd, true
		}
	}

	switch k {
	case "j", "down":
		return m.fleetTo(m.fleetCursor + 1), nil, true
	case "k", "up":
		return m.fleetTo(m.fleetCursor - 1), nil, true
	case "g":
		return m.fleetTo(0), nil, true
	case "G":
		return m.fleetTo(len(m.fleetRows()) - 1), nil, true
	case "f":
		return m.fleetSetFilter(fleetNextFilter(m.fleetFilter())), nil, true
	case "d":
		return m.dxOpen(""), nil, true
	}

	if r, ok := m.fleetSel(); ok {
		// Skip sends a row to the back so the next thing comes up under the
		// cursor; it is queue rotation, not work. A running dispatcher is not
		// asking for anything, so there is nothing to skip past.
		if k == "s" && r.kind == "queue" {
			// The order is seeded from the table as displayed — unfiltered, so a
			// narrowed view cannot silently reorder the rows it is hiding.
			all := m.fleetAll()
			ids := make([]string, 0, len(all))
			for _, x := range all {
				if x.id != r.id {
					ids = append(ids, x.id)
				}
			}
			m.cqOrder = append(ids, r.id)
			m.fleetSelID = ""
			return m.fleetSync(), nil, true
		}
		for _, a := range r.acts {
			if a.ok == "" || !cqActKeyMatches(a.k, k) {
				continue
			}
			mm, cmd := m.cqStartFlash(r, a)
			return mm, cmd, true
		}
	}

	// Only navigation leaves this screen. ',', '+', 'tab' and every v2 list key
	// are swallowed: the palette is the way to settings and to a repo-first
	// dispatch, and the form is the way to a new one.
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
