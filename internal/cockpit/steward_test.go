package cockpit

import (
	"errors"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/steward"
)

// A wait the steward has read leads with its note; one it has not falls back
// to the dispatcher's own ask; a note about an earlier wait says nothing.
func TestQueueSignalLeadsWithTheStewardsNote(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Minute)
	rec := &state.Dispatch{Status: state.StatusNeedsInput, WaitingSince: &earlier,
		Said: "Want me to merge it?", StewardNote: "yours: merge #71 — CI green", StewardNoteAt: &now}
	if got := cqQueueSignal(rec, "review"); got != "steward · yours: merge #71 — CI green" {
		t.Errorf("signal = %q", got)
	}
	rec.WaitingSince = &now
	rec.StewardNoteAt = &earlier
	if got := cqQueueSignal(rec, "review"); got != "Want me to merge it?" {
		t.Errorf("a stale note must not speak for a new wait: %q", got)
	}
}

func TestOnStewardStarted(t *testing.T) {
	m := newModel()
	mm, cmd := m.onStewardStarted(stewardStartedMsg{err: errors.New("tmux is not available")})
	if cmd != nil || !strings.Contains(mm.notice, "tmux is not available") {
		t.Fatalf("a failed start must say why and hand nothing over: %q", mm.notice)
	}
	// Already running, or started untrusted, both go on to the handover; with
	// no real session here the handover reports there is nothing to attach to,
	// which is enough to show it was attempted.
	for _, err := range []error{steward.ErrRunning, steward.ErrUntrusted} {
		mm, _ = m.onStewardStarted(stewardStartedMsg{err: err})
		if strings.HasPrefix(mm.notice, "steward: ") {
			t.Errorf("%v must not be reported as a failure: %q", err, mm.notice)
		}
	}
}

// t flips the steward: on when off, off when on, never handing the terminal
// over — and the headline and footer agree with the key at once.
func TestStewardToggle(t *testing.T) {
	prevStart, prevStop, prevOn := stewardStart, stewardStop, stewardSwitchedOn
	prevE, prevR := stewardEnabled, stewardOn
	t.Cleanup(func() {
		stewardStart, stewardStop, stewardSwitchedOn = prevStart, prevStop, prevOn
		stewardEnabled, stewardOn = prevE, prevR
	})
	on := false
	var started, stopped int
	stewardSwitchedOn = func() bool { return on }
	stewardStart = func() error { started++; on = true; return nil }
	stewardStop = func() error { stopped++; on = false; return nil }
	stewardEnabled, stewardOn = false, false

	if stewardClause() != "" || stewardToggleVerb() != "start steward" {
		t.Fatalf("off: clause %q verb %q", stewardClause(), stewardToggleVerb())
	}
	msg := stewardToggleCmd()().(stewardToggledMsg)
	if !msg.on || started != 1 {
		t.Fatalf("t on an off steward must start it: %+v", msg)
	}
	m, _ := newModel().onStewardToggled(msg)
	if stewardClause() != "steward on" || stewardToggleVerb() != "stop steward" || !strings.Contains(m.notice, "steward on") {
		t.Fatalf("on: clause %q verb %q notice %q", stewardClause(), stewardToggleVerb(), m.notice)
	}
	stewardOn = false // a reboot took its session; the poll will bring it back
	if stewardClause() != "steward starting" {
		t.Errorf("switched on but down: %q", stewardClause())
	}
	msg = stewardToggleCmd()().(stewardToggledMsg)
	if msg.on || stopped != 1 {
		t.Fatalf("t on a running steward must stop it: %+v", msg)
	}
	_, _ = newModel().onStewardToggled(msg)
	if stewardClause() != "" {
		t.Errorf("off again: %q", stewardClause())
	}

	stewardStart = func() error { return errors.New("tmux is not available") }
	if msg := stewardToggleCmd()().(stewardToggledMsg); msg.on || !strings.Contains(msg.notice, "tmux is not available") {
		t.Errorf("a failed start must stay off and say why: %+v", msg)
	}
}

// The footer advertises the switch in the words of what it will do, and t on
// the table flips it.
func TestFooterOffersTheStewardSwitch(t *testing.T) {
	installFleetFixture(t)
	prev := stewardEnabled
	t.Cleanup(func() { stewardEnabled = prev })
	stewardEnabled = false
	m := newModel()
	if h := m.cqFooterHelp(); !strings.Contains(h, "t start steward") {
		t.Errorf("footer = %q", h)
	}
	stewardEnabled = true
	if h := m.cqFooterHelp(); !strings.Contains(h, "t stop steward") {
		t.Errorf("footer = %q", h)
	}
	mm, cmd, _ := m.updateFloorQueue("t")
	if cmd == nil || !strings.Contains(mm.notice, "stop steward") {
		t.Errorf("t on the table must flip the switch: notice %q", mm.notice)
	}
}
