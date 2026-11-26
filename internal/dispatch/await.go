package dispatch

// await.go is the wait a caller makes before it says a launch worked. The
// supervisor accepting a session is the pane existing, not claude running in
// it, and a caller that reports success on the first is how a handoff told the
// human it had worked while its claude lay dead in the pane (ADR 0038).

import (
	"fmt"
	"time"

	"claude-dispatcher/internal/state"
)

// StartOutcome is what a caller that has just launched or resumed a session can
// say about it.
type StartOutcome int

const (
	// StartUnconfirmed: neither of the others within the wait. The session is
	// there and claude in it has not reported in — starting slowly, or stopped
	// on a startup prompt that no hook reports (LaunchStalled).
	StartUnconfirmed StartOutcome = iota
	// StartConfirmed: claude reported in. A hook moved the record off
	// launching, which only a session running in it can do.
	StartConfirmed
	// StartFailed: the record ended without claude staying up — the session
	// went (ReconcileSessions), claude died at its shell (diedStarting), or it
	// reported in and ended within the wait. The record's reason says which.
	StartFailed
)

// StartWait is how long a caller waits for the verdict before saying it has
// none. Past LaunchStallAfter, so a claude still silent at the end of it is one
// the cockpit itself already shows as stopped on a startup prompt; a claude
// starting normally reports in within a few seconds of its pane opening.
const StartWait = LaunchStallAfter + 10*time.Second

// awaitPoll is how often AwaitStart looks. Each look is the sweep for one
// record: a listing of its server, and a pane check once past StartSettle.
var awaitPoll = 250 * time.Millisecond

// AwaitStart waits, up to within, for the dispatch id's claude to report in or
// to be seen to have died, and returns which with the record as it then stood.
//
// It decides nothing of its own. Each look is ReconcileSessions over the one
// record, the same sweep the cockpit makes on every load, so what a caller is
// told here and what the cockpit's row says are one verdict reached once —
// and a dead launch is retired, its pane's words on the record, whether or not
// any cockpit is open to do it. A record that cannot be found is a failure:
// there is nothing to have started.
func AwaitStart(id string, within time.Duration) (StartOutcome, *state.Dispatch) {
	deadline := time.Now().Add(within)
	for {
		d := recordByID(id)
		if d == nil {
			return StartFailed, nil
		}
		if d.Status == state.StatusLaunching {
			ReconcileSessions([]*state.Dispatch{d})
		}
		switch d.Status {
		case state.StatusLaunching:
		case state.StatusExited:
			return StartFailed, d
		default:
			return StartConfirmed, d
		}
		if !time.Now().Before(deadline) {
			return StartUnconfirmed, d
		}
		time.Sleep(awaitPoll)
	}
}

// StartReport is the clause a caller prints for an outcome, so every command
// that waits says the same thing about the same verdict. waited is the wait it
// made, for the one outcome that is about time running out.
func StartReport(o StartOutcome, d *state.Dispatch, waited time.Duration) string {
	switch {
	case d == nil:
		return "its record is gone, so nothing can say it started"
	case o == StartConfirmed:
		return "claude reported in"
	case o == StartFailed:
		return "it did not stay up: " + d.StatusReason
	}
	return fmt.Sprintf("not confirmed: claude has not reported in after %s — it may be stopped on a startup prompt in session %s",
		waited.Round(time.Second), d.TmuxSession)
}

// recordByID is the record with id as it is on disk now, or nil.
func recordByID(id string) *state.Dispatch {
	for _, d := range state.LoadAll() {
		if d.ID == id {
			return d
		}
	}
	return nil
}
