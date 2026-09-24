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
