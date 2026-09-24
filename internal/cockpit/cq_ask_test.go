package cockpit

import (
	"testing"
	"time"

	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// The fixtures are real turn endings from the transcripts ADR 0017 measured,
// each followed in the log by the human attaching to type a one-line answer.
func TestCqAsk(t *testing.T) {
	cases := []struct{ name, said, want string }{
		{"question closes the message",
			"PR #70 is still open, all checks green, and `mergeStateStatus` is `BLOCKED`.\n\nWant me to merge it now?",
			"Want me to merge it now?"},
		{"invitation without a question mark",
			"I've left the merge to you — main auto-deploys, and that's your call rather than mine. Say the word and I'll merge it.",
			"Say the word and I'll merge it."},
		{"ask above a trailing caveat",
			"Want me to wire both in? It's small enough to be one PR.",
			"Want me to wire both in?"},
		{"emphasis stripped, list item split from its lead",
			"Two options:\n\n**Say the word.**\n- Env-var cutover — set `ATLAS_*` on both apps.",
			"Say the word."},
		{"a report asks nothing",
			"PR #700 is open. Watching CI.\n\nProduction right now: core on `a7761e3e`, serving 200.",
			""},
		{"a question far above the close is one the message went past",
			"Should we split it?\n\nPara two.\n\nPara three.\n\nPara four. Done.",
			""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := cqAsk(c.said); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestCqFailSignal(t *testing.T) {
	now := time.Now()
	rec := func(status state.Status, errKind string, at time.Time, retries int) *state.Dispatch {
		return &state.Dispatch{Status: status, Failure: &state.Failure{Error: errKind, At: at, Retries: retries}}
	}
	steps := len(dispatchpkg.RetryBackoff)
	cases := []struct {
		name string
		rec  *state.Dispatch
		want string
	}{
		{"no failure", &state.Dispatch{Status: state.StatusNeedsInput}, ""},
		{"retry scheduled", rec(state.StatusNeedsInput, "overloaded", now.Add(-20*time.Second), 0),
			"api error · overloaded · retry 1 of 3 in 40s"},
		{"retry due", rec(state.StatusNeedsInput, "server_error", now.Add(-61*time.Second), 0),
			"api error · server error · retry 1 of 3 now"},
		{"retry sent, going again", rec(state.StatusWorking, "overloaded", now, 1),
			"retried after overloaded · 1 of 3"},
		{"human answered it", rec(state.StatusWorking, "overloaded", now, 0), ""},
		{"spent", rec(state.StatusNeedsInput, "overloaded", now.Add(-time.Hour), steps),
			"api error · overloaded · 3 retries spent"},
		{"the human's to fix", rec(state.StatusNeedsInput, "rate_limit", now, 0),
			"api error · rate limit"},
	}
	for _, c := range cases {
		if got := cqFailSignal(c.rec, now); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// A pending retry files the dispatcher with the running rows; once it is spent
// or overdue, the dispatcher is the human's again.
func TestFloorStateRetryPending(t *testing.T) {
	pending := &state.Dispatch{Status: state.StatusNeedsInput,
		Failure: &state.Failure{Error: "overloaded", At: time.Now()}}
	if st := floorState(pending); st != "working" {
		t.Errorf("pending retry: got %q want working", st)
	}
	spent := &state.Dispatch{Status: state.StatusNeedsInput,
		Failure: &state.Failure{Error: "overloaded", At: time.Now().Add(-time.Hour), Retries: 3}}
	if st := floorState(spent); st != "needs" {
		t.Errorf("spent retries: got %q want needs", st)
	}
	limit := &state.Dispatch{Status: state.StatusNeedsInput,
		Failure: &state.Failure{Error: "rate_limit", At: time.Now()}}
	if st := floorState(limit); st != "needs" {
		t.Errorf("usage limit: got %q want needs", st)
	}
}
