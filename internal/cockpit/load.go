package cockpit

// load.go owns when the cockpit rebuilds its snapshot, and which rebuild is
// allowed to reach the screen.
//
// Every lens draws from package vars that applySnapshot swaps wholesale, and a
// load runs off the UI goroutine. Nothing used to sit between the two: an
// fsnotify event, the poll, a finished action and a jump-in each fired a load
// of their own the moment they arrived, however many were already out, and
// whichever returned last won the table.
//
// That is the disappearing dispatch. A load takes seconds — measured on a real
// portfolio, 4.5s warm and 61s on the first, cold pass — while a hook event
// from any session writes a record and starts another one. So when a dispatch
// lands, a load that read the records BEFORE it existed is routinely still in
// flight; it returns afterwards, publishes a fleet with no such dispatcher in
// it, and the row the human just made is gone. It comes back on the next load
// that happens to be fresh, or not until the poll a minute later — and on an
// otherwise empty fleet the triage lens falls back to the dispatch form, so
// what the human sees is the form they just submitted, blank, as though
// nothing had happened.
//
// Two rules fix it, and both are needed:
//
//   - one load at a time. A request that arrives while a load is out is
//     remembered, not started, and runs when that load lands. Requests
//     collapse: ten records changing during one load is one reload, not ten.
//   - a load carries the number it was started with, and a snapshot older than
//     the one already applied is dropped rather than published. Coalescing
//     makes overlap rare; this makes it harmless when the escape hatch below
//     lets it happen anyway.
//
// What this deliberately does NOT do is make the pending placeholder wait for
// its own record — a snapshot that read the disk after the launch reported
// success is what retires it (see prunePending), and that is a fact about the
// snapshot rather than about how many are in flight.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// loadKind is what a queued request would run. The zero value is "nothing
// queued", and the order matters: a recheck outranks a plain reload, because it
// re-reads the forge and sweeps session liveness first (see recheckCmd) and a
// plain reload arriving while one is queued must not downgrade it.
type loadKind int

const (
	loadNone loadKind = iota
	loadPlain
	loadRecheck
)

// loadStuckAfter is how long a load may be out before another is allowed to
// start beside it.
//
// One-at-a-time is a rule about a load that is going to land. A load that never
// does — a wedged subprocess, a `git fetch` against a host that is black-holing
// packets — must not freeze the cockpit's data for the rest of the session,
// which is what an unconditional latch would do. The window is far longer than
// any real load (the slowest measured is a cold first pass at about a minute),
// so crossing it means something is wrong rather than slow; the sequence guard
// then keeps the late one from overwriting anything newer when it finally
// arrives.
const loadStuckAfter = 5 * time.Minute

// requestLoad asks for a rebuild. It returns the command to run, which is nil
// when a load is already out — the request is remembered on the model and the
// landing snapshot starts it.
//
// Every caller goes through here. A load started outside it would carry no
// sequence number and could publish over a newer table.
func (m model) requestLoad(kind loadKind) (model, tea.Cmd) {
	if m.cfg == nil || kind == loadNone {
		return m, nil
	}
	if m.loadBusy && time.Since(m.loadStarted) < loadStuckAfter {
		if kind > m.loadNext {
			m.loadNext = kind
		}
		return m, nil
	}
	m.loadSeq++
	m.loadBusy, m.loadStarted = true, time.Now()
	if kind == loadRecheck {
		return m, recheckCmd(m.cfg, m.loadSeq)
	}
	return m, loadSnapshotCmd(m.cfg, m.loadSeq)
}

// loadLanded books a snapshot in: it reports whether this one is fresh enough
// to publish, and returns the command for whatever was asked for while it was
// out.
//
// A snapshot with no sequence number (0) always publishes: those come from a
// test driving Update directly, and the guard is about racing loads, not about
// refusing data.
func (m model) loadLanded(s snapshot) (model, bool, tea.Cmd) {
	m.loadBusy = false
	fresh := s.seq >= m.loadApplied
	if fresh {
		m.loadApplied = s.seq
	}
	next := m.loadNext
	m.loadNext = loadNone
	mm, cmd := m.requestLoad(next)
	return mm, fresh, cmd
}
