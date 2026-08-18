package cockpit

// subagents_test.go pins how a dispatcher's fan-out is reported against its
// headline: the SIGNAL cell carries a live count shaped like the ci clause,
// the meta line counts the turn, and the detail panel names the agent types.
// The facts come off the record (hookcmd's SubagentStart/SubagentStop); the
// cockpit never reads a transcript to find them.

import (
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

func TestCqFanSignal(t *testing.T) {
	if got := cqFanSignal(0); got != "" {
		t.Errorf("no fan-out must be silence, not %q", got)
	}
	if got := cqFanSignal(3); got != "fan-out · 3 live" {
		t.Errorf("cqFanSignal(3) = %q", got)
	}
}

func TestCqJoinDropsEmptyClauses(t *testing.T) {
	if got := cqJoin("", "green, unmerged"); got != "green, unmerged" {
		t.Errorf("cqJoin = %q — an absent clause must cost no punctuation", got)
	}
	if got := cqJoin("fan-out · 2 live", "ci · 1 of 3 red"); got != "fan-out · 2 live · ci · 1 of 3 red" {
		t.Errorf("cqJoin = %q", got)
	}
	if got := cqJoin("", ""); got != "" {
		t.Errorf("cqJoin of nothing = %q", got)
	}
}

func TestCqAgentsLine(t *testing.T) {
	if got := cqAgentsLine(fleetRow{}); got != "" {
		t.Errorf("most dispatchers never spread — no fan-out must say nothing, not %q", got)
	}
	r := fleetRow{subLive: []string{"Explore", "Explore"}, subDone: []string{"Plan"}}
	if got := cqAgentsLine(r); got != "2 subagents live, 1 done" {
		t.Errorf("cqAgentsLine = %q", got)
	}
	if got := cqAgentsLine(fleetRow{subLive: []string{"Explore"}}); got != "1 subagent live" {
		t.Errorf("cqAgentsLine = %q — one agent is singular", got)
	}
	if got := cqAgentsLine(fleetRow{subDone: []string{"a", "b", "c"}}); got != "fanned out 3 subagents" {
		t.Errorf("cqAgentsLine = %q — a finished fan-out reports what the turn used", got)
	}
}

func TestCqAgentsDetailNamesTheTypes(t *testing.T) {
	r := fleetRow{
		subLive: []string{"Explore", "Plan", "Explore"},
		subDone: []string{"code-reviewer"},
	}
	want := "Explore ×2, Plan live · code-reviewer done"
	if got := cqAgentsDetail(r); got != want {
		t.Errorf("cqAgentsDetail = %q, want %q", got, want)
	}
	if got := cqAgentsDetail(fleetRow{}); got != "" {
		t.Errorf("no fan-out, no line — got %q", got)
	}
}

func TestFleetFanLine(t *testing.T) {
	if got := fleetFanLine(false); got != "" {
		t.Errorf("a record from before the switch says nothing, not %q", got)
	}
	if got := fleetFanLine(true); got != "fan-out" {
		t.Errorf("fleetFanLine = %q", got)
	}
}

// fleetSubagents reads the record and nothing else, and never emits a blank
// name — a payload without a type is still an agent.
func TestFleetSubagentsSplitsTheRecord(t *testing.T) {
	at := time.Now()
	rec := &state.Dispatch{Subagents: []state.Subagent{
		{ID: "a1", Type: "Explore", StartedAt: at},
		{ID: "a2", Type: "", StartedAt: at},
		{ID: "a3", Type: "Plan", StartedAt: at, StoppedAt: &at},
	}}
	live, done := fleetSubagents(rec)
	if len(live) != 2 || live[0] != "Explore" || live[1] != "subagent" {
		t.Errorf("live = %v", live)
	}
	if len(done) != 1 || done[0] != "Plan" {
		t.Errorf("done = %v", done)
	}
}

// The SIGNAL cell reports the fan-out beside where the PR stands, the way ci
// is reported — and a running row with no PR still reports the fan-out alone.
func TestRunningRowReportsFanOutBesideThePR(t *testing.T) {
	fakeGHOnPath(t, "SUCCESS")
	at := time.Now()
	rec := &state.Dispatch{
		ID: "rec-1", Feature: "retry backoff", RepoName: "alpha-api",
		RepoPath: t.TempDir(), Branch: "feature/retry-backoff",
		Status: state.StatusWorking, PRNumber: 12, PRState: "OPEN",
		Subagents: []state.Subagent{
			{ID: "a1", Type: "Explore", StartedAt: at},
			{ID: "a2", Type: "Plan", StartedAt: at},
		},
		UpdatedAt: at,
	}
	floorBy := map[string]dispatch{"retry backoff": {feature: "retry backoff", forge: "gh"}}

	r, _ := fleetRunRow(&collectCtx{}, &snapshot{}, floorBy, map[string]int{}, rec)
	if r.signal != "fan-out · 2 live · green, unmerged" {
		t.Fatalf("signal = %q, want the fan-out beside the PR's standing", r.signal)
	}

	rec.PRNumber, rec.PRState = 0, ""
	r, _ = fleetRunRow(&collectCtx{}, &snapshot{}, floorBy, map[string]int{}, rec)
	if r.signal != "fan-out · 2 live" {
		t.Fatalf("signal = %q, want the fan-out alone when there is no PR", r.signal)
	}
}
