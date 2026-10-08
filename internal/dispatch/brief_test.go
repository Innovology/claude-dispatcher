package dispatch

import (
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

// adoptedRec is a record as Adopt leaves one, at the status the argument is
// about.
func adoptedRec(id string, st state.Status, at time.Time) *state.Dispatch {
	return &state.Dispatch{
		ID: id, Feature: id, Slug: id, Status: st,
		TmuxSession: "disp-" + id, AdoptedAt: &at,
	}
}

// A dispatched session is told to end its message on the question it needs
// answered; a session started outside the cockpit never saw that and cannot be
// handed it in a prompt somebody else typed hours ago. So it is sent as a
// message — but only into a session that is at its prompt.
func TestBriefGoesToAnAdoptedSessionAtItsPrompt(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	now := time.Now()
	typed := withRetrySeams(t, true, false, true)

	d := adoptedRec("orchestrator", state.StatusNeedsInput, now)
	saveAll(t, d)

	if got := DeliverBriefs(state.LoadAll(), now); got != 1 {
		t.Fatalf("delivered %d briefs, want 1", got)
	}
	if len(*typed) != 1 || (*typed)[0] != "disp-orchestrator: "+AdoptBrief {
		t.Errorf("typed %q", *typed)
	}
	// Spent once, however many polls follow.
	if got := DeliverBriefs(state.LoadAll(), now); got != 0 {
		t.Errorf("a second poll sent the brief again (%d)", got)
	}
	if recordOnDisk(t, "orchestrator").AdoptBriefAt == nil {
		t.Error("the record does not say the brief was sent")
	}
}

// The states it must not type into, and the one that matters most: a blocked
// session is sitting on a menu, and typing at a menu picks one of its options.
func TestBriefWaitsForASafeMoment(t *testing.T) {
	for name, st := range map[string]state.Status{
		"on a menu or a permission prompt": state.StatusBlocked,
		"mid-turn":                         state.StatusWorking,
		"finished":                         state.StatusDone,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
			now := time.Now()
			typed := withRetrySeams(t, true, false, true)
			saveAll(t, adoptedRec("orchestrator", st, now))

			if got := DeliverBriefs(state.LoadAll(), now); got != 0 {
				t.Errorf("delivered %d briefs into a session that is %s", got, name)
			}
			if len(*typed) != 0 {
				t.Errorf("typed into a session that is %s: %q", name, *typed)
			}
			if recordOnDisk(t, "orchestrator").AdoptBriefAt != nil {
				t.Error("the brief was marked spent without being sent")
			}
		})
	}
}

// Claude having exited is the other way text lands somewhere it should not:
// in the shell behind the pane. An unknown answer is a skip for the same
// reason the retry treats it as one.
func TestBriefNeverTypesIntoAPaneWithoutClaude(t *testing.T) {
	for name, tc := range map[string]struct{ idle, known bool }{
		"claude has exited":      {true, true},
		"the session cannot say": {false, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
			now := time.Now()
			typed := withRetrySeams(t, true, tc.idle, tc.known)
			saveAll(t, adoptedRec("orchestrator", state.StatusNeedsInput, now))

			if got := DeliverBriefs(state.LoadAll(), now); got != 0 || len(*typed) != 0 {
				t.Errorf("%s: delivered %d, typed %q", name, got, *typed)
			}
		})
	}
}

// A dispatched record is never briefed: it carries the contract in its prompt,
// and a second copy arriving as a message would be the cockpit talking over
// the brief the human wrote.
func TestADispatchedRecordIsNeverBriefed(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	now := time.Now()
	typed := withRetrySeams(t, true, false, true)
	d := adoptedRec("dispatched", state.StatusNeedsInput, now)
	d.AdoptedAt = nil // an ordinary dispatch
	saveAll(t, d)

	if got := DeliverBriefs(state.LoadAll(), now); got != 0 || len(*typed) != 0 {
		t.Errorf("briefed a dispatched record: %d, %q", got, *typed)
	}
	// And a released one is nobody's to brief.
	rel := adoptedRec("released", state.StatusNeedsInput, now)
	rel.Release(now)
	if BriefPending(rel) {
		t.Error("a released record is still owed a brief")
	}
}

// The brief must be sendable as one line of text: SendKeys types it literally,
// and a newline in the middle would submit half a sentence.
func TestBriefIsOneLine(t *testing.T) {
	for _, r := range AdoptBrief {
		if r == '\n' || r == '\r' {
			t.Fatal("the brief contains a line break, which would submit it half-typed")
		}
	}
	if len(AdoptBrief) > 600 {
		t.Errorf("the brief is %d bytes — it arrives mid-work and every line costs context", len(AdoptBrief))
	}
}
