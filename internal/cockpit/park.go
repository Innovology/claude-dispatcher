package cockpit

// park.go is the parking shelf's cockpit side: the reason input that opens
// over the triage lens, and the two record writes.
//
// Parking is a human annotation on the record (state.Dispatch.ParkedReason /
// ParkedAt), never a Status: the session has asked something the human cannot
// answer right now, and that is a fact about the human, not the machine.
// Status stays with the hooks, which keep reporting the truth underneath —
// the shelf survives every lifecycle event except the one that dissolves it,
// a prompt reaching the session (hookcmd clears the pair on UserPromptSubmit,
// because someone just answered the question it was parked on).

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/state"
)

// parkTarget is what the park input is about. It is captured when the input
// opens, because the input outlives the cursor that opened it: the table keeps
// rebuilding underneath, and a target re-read from the cursor could park a
// different dispatcher than the one the human is reading about.
type parkTarget struct{ id, feature string }

// updatePark drives the reason input while it owns the keyboard.
//
// The reason is the point of the feature — the parked group shows it, and a
// shelf full of unexplained rows is just a second history — so enter with
// nothing typed parks nothing and says why, rather than silently closing.
func (m model) updatePark(k string) (model, tea.Cmd) {
	next, submit, cancel := applyEdit(m.parkText, k, m.key)
	if cancel {
		m.parkOpen, m.parkText, m.parkAt = false, "", nil
		return m, nil
	}
	if submit {
		t := m.parkAt
		reason := strings.Join(strings.Fields(m.parkText), " ")
		if reason == "" {
			m.notice = "say why — the reason is what the parked group shows"
			return m, nil
		}
		m.parkOpen, m.parkText, m.parkAt = false, "", nil
		if t == nil {
			return m, nil
		}
		// The cursor stays at its position rather than following the row to
		// the bottom of the table: parking is putting the thing down.
		m.fleetSelID = ""
		return m, parkCmd(t.id, reason)
	}
	m.parkText = next
	return m, nil
}

// viewPark renders the reason input: what is being parked, what parking does,
// and the one line to type.
func (m model) viewPark(w, h int) string {
	t := m.parkAt
	if t == nil {
		return ""
	}
	cw := w - 2*pad
	if cw < 10 {
		cw = w
	}
	var out []string
	out = append(out, fg(cDim, "park this dispatcher"))
	out = append(out, line(t.feature, cw, cWhite, ""))
	explain := "it drops to the parked group at the bottom of the fleet, out of the counts, " +
		"until you take it back up — p on its row brings it back, and so does answering it"
	for _, ln := range productWrap(explain, cw) {
		out = append(out, fg(cFaint, ln))
	}
	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	out = append(out, fg(cAmber, "why ")+fg(cWhite, m.parkText+"▏"))
	out = append(out, fg(cFaint, "enter parks it · esc cancels"))
	return clampLines(gutter(strings.Join(out, "\n"), pad), h)
}

// parkCmd shelves the record under the human's reason.
func parkCmd(id, reason string) tea.Cmd {
	return func() tea.Msg {
		rec := recordByID(id)
		if rec == nil {
			return actionMsg{notice: "no dispatch record to park"}
		}
		now := time.Now()
		rec.ParkedReason, rec.ParkedAt = reason, &now
		_ = state.Save(rec)
		return actionMsg{notice: "parked \"" + rec.Feature + "\" · " + reason}
	}
}

// unparkCmd takes the record back up: the annotation is cleared and the next
// snapshot re-ranks the row from its real status like any other.
func unparkCmd(id string) tea.Cmd {
	return func() tea.Msg {
		rec := recordByID(id)
		if rec == nil {
			return actionMsg{notice: "no dispatch record to unpark"}
		}
		rec.ParkedReason, rec.ParkedAt = "", nil
		_ = state.Save(rec)
		return actionMsg{notice: "\"" + rec.Feature + "\" back on the fleet"}
	}
}
