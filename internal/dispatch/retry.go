package dispatch

import (
	"fmt"
	"time"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// RetryText is what a retry types at the session: the word the human typed
// after 20 of the 24 API-error turns measured in the transcripts (ADR 0018).
const RetryText = "continue"

// RetryBackoff is how long after a failure each successive retry waits. Its
// length is the cap: once every step has been tried the failure is the
// human's, because an API that has refused three times across twenty minutes
// is not a blip, and a session typed at forever would bury the one fact the row
// is there to tell them.
//
// The first step is a minute, not immediate: the 529s in the transcripts came
// in runs, and a retry into the same overload fails the same way and spends a
// step doing it. The cockpit polls once a minute, so a step is honoured to the
// nearest poll, never early.
var RetryBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

// sendKeys is a seam: tests assert what would be typed without a tmux server.
var sendKeys = supervisor.SendKeys

// RetryDue reports whether d is a stopped turn a retry should be sent to now,
// on the record alone. It is the screen RetryFailed re-asks under the lock, and
// what the cockpit reads to say when the next one is coming.
//
// Parked is excluded on purpose: the human shelved it, and a shelf is a
// statement that they are handling it — a machine typing into it behind their
// back would be exactly what parking exists to stop.
func RetryDue(d *state.Dispatch, now time.Time) (due bool, at time.Time) {
	if d.Status != state.StatusNeedsInput || !d.Failure.Transient() || d.Parked() {
		return false, time.Time{}
	}
	n := d.Failure.Retries
	if n >= len(RetryBackoff) {
		return false, time.Time{}
	}
	at = d.Failure.At.Add(RetryBackoff[n])
	return !now.Before(at), at
}

// RetryGrace is how overdue a scheduled retry may be before the screen stops
// promising it. The poll is a minute, so two of them is a retry that should
// have happened and did not — the pane could not be seen into, the session was
// busy with something else — and a row saying "retrying" over a retry that is
// never coming is the stall this exists to end, wearing a better excuse.
const RetryGrace = 2 * time.Minute

// RetryPending reports whether the machine still has d in hand: its turn
// ended on a transient error, a retry is scheduled, and that retry is not
// overdue past RetryGrace. While it is, the dispatcher is not the human's —
// the cockpit files it with the running rows, saying when the retry comes.
func RetryPending(d *state.Dispatch, now time.Time) bool {
	due, at := RetryDue(d, now)
	if at.IsZero() {
		return false
	}
	return !due || now.Before(at.Add(RetryGrace))
}

// RetryFailed types "continue" into every session whose last turn a transient
// API error ended, on the RetryBackoff schedule. It is the one part of keeping
// a dispatcher going that the cockpit does rather than the session: Claude
// Code's own long-running machinery (background tasks, Monitor, subagents)
// only runs inside a turn, and StopFailure is fire-and-forget — the hook's
// output is ignored — so once an API error has ended the turn, nothing inside
// the session will start another. Something outside has to type.
//
// It types only where claude is provably still at its prompt: the session is
// live and SessionIdle says claude has NOT exited. An unknown answer is a skip,
// never a send, because text typed into a pane whose claude has gone lands in
// the shell. The retry is counted and saved before the keys go, under the
// hook's lock and against a fresh read of the record, so a hook that landed
// since the screening (the human answering it themselves, say) wins, and a
// send that fails still spends a step rather than retrying in a tight loop.
func RetryFailed(ds []*state.Dispatch, now time.Time) (sent int) {
	if !supervisorReady() {
		return 0
	}
	for _, d := range ds {
		if due, _ := RetryDue(d, now); !due || d.TmuxSession == "" {
			continue
		}
		if !sessionAlive(d.TmuxSession) {
			continue
		}
		if idle, known := sessionIdle(d.TmuxSession); idle || !known {
			continue
		}
		rec, ok := claimRetry(d.ID, now)
		if !ok {
			continue
		}
		if sendKeys(rec.TmuxSession, RetryText) == nil {
			sent++
		}
	}
	return sent
}

// claimRetry re-reads the record under the hook lock, re-checks it is still
// due, and saves the retry before anything is typed.
func claimRetry(id string, now time.Time) (*state.Dispatch, bool) {
	release := state.Lock()
	defer release()
	var rec *state.Dispatch
	for _, d := range state.LoadAll() {
		if d.ID == id {
			rec = d
			break
		}
	}
	if rec == nil {
		return nil, false
	}
	if due, _ := RetryDue(rec, now); !due {
		return nil, false
	}
	f := rec.Failure
	f.Retries++
	f.RetriedAt = &now
	rec.StatusReason = fmt.Sprintf("retrying after an API error (%d of %d)", f.Retries, len(RetryBackoff))
	if state.Save(rec) != nil {
		return nil, false
	}
	state.AppendEvent(state.Event{
		Event:        state.EventRetried,
		DispatcherID: rec.ID,
		SessionID:    rec.SessionID,
		Reason:       f.Error,
	})
	return rec, true
}
