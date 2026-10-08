package cockpit

// cq_ask.go is what a stopped dispatcher is actually asking, in its own words,
// and what an API error has done to one — the two facts a waiting row used to
// leave the human to attach and find out.
//
// Both quote. cqAsk picks a sentence out of the session's own last message and
// never rewrites it; cqFailSignal names Claude Code's own error category and the
// retry the cockpit has scheduled. Neither guesses: a message with no ask in it
// yields "", and the row falls back to saying the turn finished.

import (
	"time"

	"claude-dispatcher/internal/ask"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// cqAsk is the question a stopped turn ended on, verbatim, or "" when the
// message asks nothing — see ask.Of, which the status command shares so the
// table and a steward reading the fleet can never quote different asks.
func cqAsk(said string) string { return ask.Of(said) }

// cqErrorName is Claude Code's error category in words.
func cqErrorName(f *state.Failure) string { return dispatchpkg.ErrorName(f) }

// cqFailSignal is the SIGNAL clause for a dispatcher an API error stopped — see
// dispatch.FailureSummary, which the status command shares.
func cqFailSignal(rec *state.Dispatch, now time.Time) string {
	return dispatchpkg.FailureSummary(rec, now)
}

// cqFailLead is the detail panel's sentence for a stopped-by-error row: the
// error in Claude Code's words, with its detail when it sent one.
func cqFailLead(rec *state.Dispatch) string {
	f := rec.Failure
	lead := "An API error ended its turn: " + cqErrorName(f)
	if f.Detail != "" {
		lead += " (" + f.Detail + ")"
	}
	if !f.Transient() {
		return lead + ". This one is yours to fix; retrying would not get past it."
	}
	return lead + "."
}
