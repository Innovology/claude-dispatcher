package cockpit

// steward.go is the cockpit's side of the steward (internal/steward).
//
// The steward is meant to be a switch, not a session the human runs: t on the
// triage table turns it on or off in the background, the headline says so, and
// the poll keeps a switched-on steward running (steward.Ensure). What it does
// shows up where the human already looks — its notes and answers on their
// rows. Its session is there to read, not to operate: the palette's "steward"
// jumps into it, and "stop steward" is the same as t while it is on.

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/steward"
)

// stewardStartedMsg is a start attempt coming back. A steward that was already
// running is not an error here: the human asked to see it.
type stewardStartedMsg struct{ err error }

// stewardStartCmd, stewardStop and the attach are seams so a test can drive
// the palette without a supervisor.
var (
	stewardStart      = steward.Start
	stewardStop       = steward.Stop
	stewardSwitchedOn = steward.Enabled
)

// stewardToggledMsg is the switch coming back, with what to say.
type stewardToggledMsg struct {
	on     bool
	notice string
}

// stewardToggleCmd flips the steward: off if it is switched on, on otherwise.
// Nothing is handed over — the point of the switch is that the human need not
// look at the steward to have one.
func stewardToggleCmd() tea.Cmd {
	return func() tea.Msg {
		if stewardSwitchedOn() {
			// "not running" is fine here: the switch is off either way.
			_ = stewardStop()
			return stewardToggledMsg{on: false, notice: "steward off · every stop is yours again"}
		}
		switch err := stewardStart(); {
		case errors.Is(err, steward.ErrUntrusted):
			return stewardToggledMsg{on: true,
				notice: "steward on, but waiting on claude's trust prompt · : steward to accept it once"}
		case err != nil && !errors.Is(err, steward.ErrRunning):
			return stewardToggledMsg{on: false, notice: "steward: " + err.Error()}
		}
		return stewardToggledMsg{on: true,
			notice: "steward on · it answers what the brief settles and notes the rest on your rows"}
	}
}

// onStewardToggled takes the switch's new position at once — the headline and
// the footer should not wait for the next load to agree with the key just
// pressed — and reloads so the rest of the table catches up.
func (m model) onStewardToggled(msg stewardToggledMsg) (model, tea.Cmd) {
	stewardEnabled, stewardOn = msg.on, msg.on
	m.notice = msg.notice
	return m.requestLoad(loadPlain)
}

// stewardEnabledNow is the switch as the last load (or the last toggle) saw
// it — what the screen draws, without a file read per frame.
func stewardEnabledNow() bool { return stewardEnabled }

// stewardToggleVerb is what t does right now, for the footer and the flash.
func stewardToggleVerb() string {
	if stewardEnabledNow() {
		return "stop steward"
	}
	return "start steward"
}

// stewardClause is the headline's word on the steward: nothing while it is
// off, "steward on" while it runs, and "steward starting" in the moment a
// switched-on steward is down — the poll brings it back.
func stewardClause() string {
	switch {
	case !stewardEnabledNow():
		return ""
	case stewardOn:
		return "steward on"
	}
	return "steward starting"
}

func stewardStartCmd() tea.Cmd {
	return func() tea.Msg { return stewardStartedMsg{err: stewardStart()} }
}

func stewardStopCmd() tea.Cmd {
	return func() tea.Msg {
		if err := stewardStop(); err != nil {
			return actionMsg{notice: err.Error()}
		}
		return actionMsg{notice: "steward stopped · dispatchers are yours alone again"}
	}
}

// onStewardStarted attaches to the steward, or says why it could not start.
func (m model) onStewardStarted(msg stewardStartedMsg) (model, tea.Cmd) {
	// Untrusted still attaches: the trust dialog is on the screen it opens.
	if msg.err != nil && !errors.Is(msg.err, steward.ErrRunning) && !errors.Is(msg.err, steward.ErrUntrusted) {
		m.notice = "steward: " + msg.err.Error()
		return m, nil
	}
	stewardOn, stewardEnabled = true, true
	return m.attachSession(steward.Session)
}
