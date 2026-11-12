package steward

import (
	"os"
	"path/filepath"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// LegacySession is the machine-wide steward an earlier build ran (ADR 0020):
// one session for every product, kept running by a switch. Nothing starts it
// any more, but one already running carries the old brief — `status --next`
// over every product, with reply and note allowed — and would go on answering
// waits beside the product stewards: the two leads coordinating one product
// that ADR 0029 rules out. And with its switch gone from the cockpit, nothing
// else would ever stop it.
const LegacySession = "disp_steward"

// legacySwitch is that steward's switch: present meant on.
func legacySwitch() string { return filepath.Join(state.Dir(), "steward", "enabled") }

// RetireLegacy ends the machine-wide steward if it is running and removes its
// switch, and reports whether there was anything to retire. Its folder stays:
// it holds a brief and a transcript key, nothing that matters.
func RetireLegacy() (bool, error) {
	retired := false
	if err := os.Remove(legacySwitch()); err == nil {
		retired = true
	}
	s := supervisor.Session{Name: LegacySession}
	if available() && hasSession(s) {
		if err := killSession(s); err != nil {
			return retired, err
		}
		retired = true
	}
	return retired, nil
}
