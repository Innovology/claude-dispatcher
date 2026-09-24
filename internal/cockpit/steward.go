package cockpit

// steward.go is the cockpit's door to the steward session (internal/steward):
// the palette's "steward" starts it when it is not running and hands the
// terminal to it either way, because the steward's own session is where it says
// how the fleet is going; "stop steward" ends it.

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
	stewardStart = steward.Start
	stewardStop  = steward.Stop
)

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
	stewardOn = true
	return m.attachSession(steward.Session)
}
