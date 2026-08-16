package cockpit

// age_test.go covers the two things the triage table says about time: that its
// ages move on their own, and that there are two of them.
//
// Both were defects. Every age on the screen was formatted from the clock at
// the moment something happened to call View, and the only thing that regularly
// did was the once-a-minute poll — so a column counting in seconds stood still
// for a minute at a time and then jumped. And the one column it had answered
// "how long since it moved", which is not the same question as "how long has
// this been going" and cannot stand in for it.

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// The clock tick is a redraw and nothing else: it re-arms itself, and it leaves
// the model exactly as it found it. Anything it changed, or any load it kicked
// off, would be work running once a second for as long as the cockpit is open.
func TestAgeTickRedrawsAndDoesNothingElse(t *testing.T) {
	m := cqModel(t)
	next, cmd := m.Update(ageTickMsg{})
	if cmd == nil {
		t.Fatal("the clock tick must re-arm, or the screen ages once and then stops")
	}
	if !reflect.DeepEqual(next.(model), m) {
		t.Error("the clock tick changed the model; it is supposed to cost a render and no more")
	}
	if msg := cmd(); msg != (ageTickMsg{}) {
		t.Errorf("the tick re-armed with %#v, want another clock tick", msg)
	}
}

// The ages on the table are read from the clock every time it is drawn, not
// formatted once into the snapshot. This is the whole reported bug: a fleet
// that has not changed on disk must still say a later time a second later.
func TestAgesAreReadFromTheClockOnEveryRender(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)

	start := time.Now()
	fleet = []fleetRow{{
		id: "id-tick", kind: "run", rank: 3, product: "alpha",
		feature: "ticker", repo: "alpha-api", signal: "working", tone: "normal",
		moved: start, started: start.Add(-90 * time.Minute),
	}}
	cqLastOutput = start

	m := newModel()
	m.width, m.height = 130, 40

	first := m.viewCQ(m.width, 30)
	if !strings.Contains(first, "0s") {
		t.Fatalf("a dispatcher that just wrote should read 0s:\n%s", first)
	}

	// Nothing is reloaded and no var is touched — only the clock moves.
	time.Sleep(1100 * time.Millisecond)

	want := cqAge(start) // what the clock says now, computed the same way
	if want == "0s" {
		t.Fatalf("the test clock did not advance: cqAge = %q", want)
	}
	second := m.viewCQ(m.width, 30)
	if strings.Contains(second, "0s") {
		t.Errorf("the age is frozen at 0s a second later:\n%s", second)
	}
	if !strings.Contains(second, want) {
		t.Errorf("the age should read %q now:\n%s", want, second)
	}
	// The unattended line carries the same measurement and used to carry it as a
	// string baked into the snapshot, which froze for a whole poll.
	if !strings.Contains(cqUnattendedLine(), "last output "+want+" ago") {
		t.Errorf("the last-output clause is stale: %q, want %q", cqUnattendedLine(), want)
	}
}

// LAST and AGE are two columns because they are two facts. A session six
// seconds from its last write and three hours into its run is the row that
// proves it: one column could only ever have shown one of those numbers.
func TestTableShowsLastActivityAndDispatcherAgeSeparately(t *testing.T) {
	m := cqModel(t) // "three": moved 6s ago, started 3h ago
	out := m.viewCQ(m.width, 30)

	head := lineContaining(out, "SIGNAL")
	if !strings.Contains(head, "LAST") || !strings.Contains(head, "AGE") {
		t.Errorf("the column header names one age, not two: %q", head)
	}

	row := lineContaining(out, "three")
	if !strings.Contains(row, "6s") {
		t.Errorf("LAST should say 6s since it last wrote: %q", row)
	}
	if !strings.Contains(row, "3h") {
		t.Errorf("AGE should say the dispatcher is 3h old: %q", row)
	}
	// And the panel spells out which is which, where the table has headers to
	// do it.
	where := cqWhere(fleet[2])
	if !strings.Contains(where, "moved 6s ago") || !strings.Contains(where, "3h old") {
		t.Errorf("the panel locator = %q", where)
	}
}

// The collector must carry the instants, not their rendered ages: a string
// formatted at load time is as stale as the load, which is what the clock tick
// cannot fix.
func TestCollectFleetCarriesInstantsNotRenderedAges(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)

	now := time.Now()
	rec := &state.Dispatch{
		ID: "ager", Feature: "age of dispatcher", RepoName: "shop-api", RepoPath: dir,
		Status: state.StatusNeedsInput, StatusReason: "turn complete — waiting on you",
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-90 * time.Second),
	}
	if err := state.Save(rec); err != nil {
		t.Fatal(err)
	}

	saved := captureVars()
	defer restoreVars(saved)
	s := loadSnapshot(&config.Config{})

	var found *fleetRow
	for i := range s.fleet {
		if s.fleet[i].feature == "age of dispatcher" {
			found = &s.fleet[i]
		}
	}
	if found == nil {
		t.Fatal("the dispatcher is missing from the fleet")
	}
	if !found.started.Equal(rec.CreatedAt) {
		t.Errorf("started = %v, want the record's CreatedAt %v", found.started, rec.CreatedAt)
	}
	if found.moved.Before(rec.UpdatedAt) {
		t.Errorf("moved = %v, want at least UpdatedAt %v", found.moved, rec.UpdatedAt)
	}
	// Two different answers off one record, which is the point of the pair.
	if cqAge(found.moved) == cqAge(found.started) {
		t.Errorf("both ages read %q — the columns are measuring the same thing",
			cqAge(found.moved))
	}
}

// lineContaining is the rendered line a marker falls on, ANSI and all.
func lineContaining(out, marker string) string {
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, marker) {
			return ln
		}
	}
	return ""
}
