package dispatch

// color.go gives a dispatch's session its project's prompt-bar colour, so
// sessions from different repos or products can be told apart at a glance
// when the human jumps between them.
//
// Claude Code has no launch flag for it. `/color <name>` is a slash command,
// and what it sets is written into the transcript as an "agent-color" entry
// that --resume restores. The hidden --agent-color flag is a teammate's colour
// and leaves a session's bar as it was, measured on 2.1.281. So the colour is
// typed in, like the adopt brief, but not with the brief's patience: /color
// is an immediate command, and measured on 2.1.281 it applies mid-turn without
// touching the turn. It can therefore go in the moment the session has
// reported in, which is seconds after the launch, rather than at the end of
// a first turn that may run for an hour.

import (
	"time"

	"claude-dispatcher/internal/state"
)

// ColorPending reports whether d is owed its colour and could take it now, on
// the record alone. It needs a session id, because that is SessionStart: the
// REPL is up and the keys go to it rather than to a startup dialog. Never on a
// menu, where typed keys pick its options, nor once the session has ended.
// Never on an adopted session, which kept its own look before we came.
func ColorPending(d *state.Dispatch) bool {
	if d == nil || d.Color == "" || d.ColorAt != nil || d.SessionID == "" || d.AdoptedAt != nil {
		return false
	}
	switch d.Status {
	case state.StatusWorking, state.StatusNeedsInput:
		return true
	}
	return false
}

// DeliverColors types `/color <name>` into every session owed one, and reports
// how many went. Only where claude is provably still running in the pane: an
// unknown answer is a skip, since the line would otherwise land in a shell.
// Claimed under the hook lock against a fresh read, so it goes once however
// many loads race, and the stamp is spent even if the send fails — a colour is
// not worth typing at a session again and again.
func DeliverColors(ds []*state.Dispatch, now time.Time) (sent int) {
	if !supervisorReady() {
		return 0
	}
	for _, d := range ds {
		if !ColorPending(d) || d.TmuxSession == "" {
			continue
		}
		if !sessionAlive(SessionOf(d)) {
			continue
		}
		if idle, known := sessionIdle(SessionOf(d)); idle || !known {
			continue
		}
		rec, ok := claimColor(d.ID, now)
		if !ok {
			continue
		}
		if sendKeys(SessionOf(rec), "/color "+rec.Color) == nil {
			sent++
		}
	}
	return sent
}

func claimColor(id string, now time.Time) (*state.Dispatch, bool) {
	release := state.Lock()
	defer release()
	for _, d := range state.LoadAll() {
		if d.ID != id {
			continue
		}
		if !ColorPending(d) {
			return nil, false
		}
		d.ColorAt = &now
		if state.Save(d) != nil {
			return nil, false
		}
		return d, true
	}
	return nil, false
}
