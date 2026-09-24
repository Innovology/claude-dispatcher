package dispatch

import (
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

// withRetrySeams stands a fake supervisor up for the retry: which sessions are
// live, what SessionIdle says about claude in them, and a record of every
// keystroke that would have been typed.
func withRetrySeams(t *testing.T, alive bool, idle, known bool) *[]string {
	t.Helper()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	prevAlive, prevIdle, prevReady, prevSend := sessionAlive, sessionIdle, supervisorReady, sendKeys
	t.Cleanup(func() {
		sessionAlive, sessionIdle, supervisorReady, sendKeys = prevAlive, prevIdle, prevReady, prevSend
	})
	sessionAlive = func(string) bool { return alive }
	sessionIdle = func(string) (bool, bool) { return idle, known }
	supervisorReady = func() bool { return true }
	var typed []string
	sendKeys = func(name, text string) error {
		typed = append(typed, name+": "+text)
		return nil
	}
	return &typed
}

func failedRec(id, errKind string, at time.Time, retries int) *state.Dispatch {
	return &state.Dispatch{
		ID: id, Feature: id, TmuxSession: "disp-" + id,
		Status:  state.StatusNeedsInput,
		Failure: &state.Failure{Error: errKind, At: at, Retries: retries},
	}
}

func onDisk(t *testing.T, id string) *state.Dispatch {
	t.Helper()
	for _, d := range state.LoadAll() {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("no record %s", id)
	return nil
}

func TestRetryFailedFollowsTheBackoff(t *testing.T) {
	typed := withRetrySeams(t, true, false, true)
	t0 := time.Now().Add(-30 * time.Second)
	rec := failedRec("a", "overloaded", t0, 0)
	saveAll(t, rec)

	if n := RetryFailed(state.LoadAll(), t0.Add(59*time.Second)); n != 0 {
		t.Fatalf("retried %d before the first step was due", n)
	}
	if n := RetryFailed(state.LoadAll(), t0.Add(time.Minute)); n != 1 {
		t.Fatalf("want one retry at the first step, got %d", n)
	}
	if len(*typed) != 1 || (*typed)[0] != "disp-a: continue" {
		t.Fatalf("typed %q", *typed)
	}
	got := onDisk(t, "a")
	if got.Failure.Retries != 1 || got.Failure.RetriedAt == nil {
		t.Fatalf("retry not counted on the record: %+v", got.Failure)
	}
	if got.StatusReason != "retrying after an API error (1 of 3)" {
		t.Errorf("reason %q", got.StatusReason)
	}
	// Same poll again: the step is spent, the next one is five minutes out.
	if n := RetryFailed(state.LoadAll(), t0.Add(2*time.Minute)); n != 0 {
		t.Fatalf("second retry fired early")
	}
	if n := RetryFailed(state.LoadAll(), t0.Add(5*time.Minute)); n != 1 {
		t.Fatalf("second step not taken")
	}
}

func TestRetryFailedStopsAtTheCap(t *testing.T) {
	typed := withRetrySeams(t, true, false, true)
	saveAll(t, failedRec("a", "overloaded", time.Now().Add(-time.Hour), len(RetryBackoff)))
	if n := RetryFailed(state.LoadAll(), time.Now()); n != 0 || len(*typed) != 0 {
		t.Fatalf("typed past the cap: %q", *typed)
	}
}

// Only a transient error is retried: a usage limit or a credential is the
// human's to fix, and typing at it again buries it.
func TestRetryFailedLeavesTheHumansErrorsAlone(t *testing.T) {
	typed := withRetrySeams(t, true, false, true)
	old := time.Now().Add(-time.Hour)
	saveAll(t,
		failedRec("limit", "rate_limit", old, 0),
		failedRec("auth", "authentication_failed", old, 0),
		failedRec("billing", "billing_error", old, 0),
	)
	if n := RetryFailed(state.LoadAll(), time.Now()); n != 0 {
		t.Fatalf("retried a human's error: %q", *typed)
	}
}

func TestRetryFailedNeverTypesIntoAShell(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	for _, c := range []struct {
		name               string
		alive, idle, known bool
	}{
		{"session gone", false, false, true},
		{"claude exited, shell in the pane", true, true, true},
		{"cannot see into the pane", true, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			typed := withRetrySeams(t, c.alive, c.idle, c.known)
			saveAll(t, failedRec("a", "overloaded", old, 0))
			if n := RetryFailed(state.LoadAll(), time.Now()); n != 0 || len(*typed) != 0 {
				t.Fatalf("typed %q", *typed)
			}
			if onDisk(t, "a").Failure.Retries != 0 {
				t.Fatal("a skipped retry must not spend a step")
			}
		})
	}
}

func TestRetryFailedSkipsWhatTheHumanHasTaken(t *testing.T) {
	typed := withRetrySeams(t, true, false, true)
	old := time.Now().Add(-time.Hour)
	parked := failedRec("parked", "overloaded", old, 0)
	now := time.Now()
	parked.ParkedAt, parked.ParkedReason = &now, "on it myself"
	working := failedRec("answered", "overloaded", old, 0)
	working.Status = state.StatusWorking // the human already typed at it
	saveAll(t, parked, working)
	if n := RetryFailed(state.LoadAll(), time.Now()); n != 0 {
		t.Fatalf("typed %q", *typed)
	}
}

// The screening read can be stale: a hook that lands between it and the send
// (the human answering the session themselves) must win.
func TestRetryFailedRechecksUnderTheLock(t *testing.T) {
	typed := withRetrySeams(t, true, false, true)
	rec := failedRec("a", "overloaded", time.Now().Add(-time.Hour), 0)
	saveAll(t, rec)
	stale := state.LoadAll()
	answered := onDisk(t, "a")
	answered.Status = state.StatusWorking
	saveAll(t, answered)
	if n := RetryFailed(stale, time.Now()); n != 0 {
		t.Fatalf("typed over the human: %q", *typed)
	}
}

func TestRetryPending(t *testing.T) {
	t0 := time.Now()
	rec := failedRec("a", "overloaded", t0, 0)
	for _, c := range []struct {
		at   time.Duration
		want bool
	}{
		{0, true},                          // scheduled
		{time.Minute, true},                // due, the poll has not come round
		{time.Minute + RetryGrace - 1, true},
		{time.Minute + RetryGrace, false}, // overdue: it is not coming
	} {
		if got := RetryPending(rec, t0.Add(c.at)); got != c.want {
			t.Errorf("at +%v: got %v want %v", c.at, got, c.want)
		}
	}
	if RetryPending(failedRec("b", "rate_limit", t0, 0), t0) {
		t.Error("a usage limit is never pending")
	}
	if RetryPending(failedRec("c", "overloaded", t0, len(RetryBackoff)), t0) {
		t.Error("a spent retry budget is not pending")
	}
}
