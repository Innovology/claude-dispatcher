package state

import (
	"strconv"
	"testing"
	"time"
)

func TestSubagentStartAndStop(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()

	if !d.SubagentStarted("a1", "Explore", at) || !d.SubagentStarted("a2", "Plan", at) {
		t.Fatal("a named start must record")
	}
	if d.SubagentsLive() != 2 || d.SubagentsDone() != 0 {
		t.Fatalf("live=%d done=%d, want 2 live", d.SubagentsLive(), d.SubagentsDone())
	}
	if !d.SubagentStopped("a1", "Explore", at) {
		t.Fatal("a stop for a known id must record")
	}
	if d.SubagentsLive() != 1 || d.SubagentsDone() != 1 {
		t.Fatalf("live=%d done=%d, want 1 and 1", d.SubagentsLive(), d.SubagentsDone())
	}
	// A second stop for the same id has nothing to add.
	if d.SubagentStopped("a1", "Explore", at) {
		t.Fatal("a repeated stop must not report a change")
	}
}

func TestSubagentStartWithoutAnIDIsDropped(t *testing.T) {
	d := &Dispatch{}
	if d.SubagentStarted("", "Explore", time.Now()) {
		t.Fatal("nothing names this subagent, so nothing can track it")
	}
	if d.SubagentStopped("", "Explore", time.Now()) {
		t.Fatal("same for a stop")
	}
	if len(d.Subagents) != 0 {
		t.Fatalf("recorded %d entries from unnameable events", len(d.Subagents))
	}
}

// A start for an id already on the record is that subagent starting over —
// reset, not duplicated, or the counts double.
func TestSubagentRestartResets(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()
	d.SubagentStarted("a1", "Explore", at)
	d.SubagentStopped("a1", "Explore", at)
	if !d.SubagentStarted("a1", "Explore", at.Add(time.Second)) {
		t.Fatal("restart must record")
	}
	if len(d.Subagents) != 1 || d.SubagentsLive() != 1 || d.SubagentsDone() != 0 {
		t.Fatalf("restart must reset the one entry: %+v", d.Subagents)
	}
}

// A stop whose start was never seen — the hook landed mid-turn — still counts,
// or the done total lies low for the rest of the turn.
func TestSubagentStopWithoutStartStillCounts(t *testing.T) {
	d := &Dispatch{}
	if !d.SubagentStopped("a1", "code-reviewer", time.Now()) {
		t.Fatal("an unmatched stop must still record")
	}
	if d.SubagentsLive() != 0 || d.SubagentsDone() != 1 {
		t.Fatalf("live=%d done=%d, want 0 and 1", d.SubagentsLive(), d.SubagentsDone())
	}
}

func TestSweepSubagents(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()
	d.SubagentStarted("a1", "Explore", at)
	d.SubagentStarted("a2", "Plan", at)
	d.SubagentStopped("a1", "Explore", at)
	if !d.SweepSubagents(at) {
		t.Fatal("a live entry was swept, so the sweep changed something")
	}
	if d.SubagentsLive() != 0 || d.SubagentsDone() != 2 {
		t.Fatalf("live=%d done=%d after sweep, want 0 and 2", d.SubagentsLive(), d.SubagentsDone())
	}
	if d.SweepSubagents(at) {
		t.Fatal("nothing left to sweep, so no change to report")
	}
}

func TestDropStoppedSubagents(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()
	d.SubagentStarted("a1", "Explore", at)
	d.SubagentStarted("a2", "Plan", at)
	d.SubagentStopped("a1", "Explore", at)
	if !d.DropStoppedSubagents() {
		t.Fatal("a stopped entry was dropped, so the drop changed something")
	}
	if len(d.Subagents) != 1 || d.Subagents[0].ID != "a2" {
		t.Fatalf("the live entry must survive the drop: %+v", d.Subagents)
	}
	// Dropping the rest empties the slice to nil so the record's JSON omits it.
	d.SubagentStopped("a2", "Plan", at)
	d.DropStoppedSubagents()
	if d.Subagents != nil {
		t.Fatalf("an emptied fan-out must marshal to nothing, got %+v", d.Subagents)
	}
}

// The cap sheds the oldest stopped entry to make room, keeping the live
// picture exact; when everything at the cap is live there is no room to make,
// and the event is dropped rather than a live entry lied away.
func TestSubagentCap(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()
	for i := 0; i < maxSubagents; i++ {
		id := "a" + strconv.Itoa(i)
		d.SubagentStarted(id, "Explore", at)
		d.SubagentStopped(id, "Explore", at)
	}
	if !d.SubagentStarted("fresh", "Plan", at) {
		t.Fatal("room must be made by shedding a stopped entry")
	}
	if len(d.Subagents) != maxSubagents || d.SubagentsLive() != 1 {
		t.Fatalf("len=%d live=%d, want len=cap live=1", len(d.Subagents), d.SubagentsLive())
	}

	all := &Dispatch{}
	for i := 0; i < maxSubagents; i++ {
		all.SubagentStarted("b"+strconv.Itoa(i), "Explore", at)
	}
	if all.SubagentStarted("overflow", "Plan", at) {
		t.Fatal("a cap full of live entries has no room, and must say so")
	}
}
