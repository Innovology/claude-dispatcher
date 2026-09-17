package cockpit

// dismiss_test.go covers the two halves of "a dispatch that ends is a thing
// that happened": the triage table holding a finished dispatcher until the
// human takes it off, and history ordering itself by when each dispatcher was
// last worked rather than by when we last wrote its file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

func ptrTime(t time.Time) *time.Time { return &t }

// loadRecord reads one record back off disk by id.
func loadRecord(t *testing.T, id string) *state.Dispatch {
	t.Helper()
	for _, d := range state.LoadAll() {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("no record %q on disk", id)
	return nil
}

// touchTranscript writes a transcript file with a given mtime and returns its
// path — the "last worked" clock every finished row is ordered by.
func touchTranscript(t *testing.T, name string, mt time.Time) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

// ---- the record ---------------------------------------------------------------

// Stop stamps the instant a dispatcher stopped, once. Everything that writes a
// finished record afterwards — the tracker catching a merge, a reconcile, a PR
// field coming back different — must leave that stamp alone, or "when did this
// finish" becomes "when did we last touch the file", which is the question
// UpdatedAt already answers badly.
func TestStopStampsTheTransitionOnce(t *testing.T) {
	first := time.Now().Add(-3 * time.Hour)
	d := &state.Dispatch{ID: "r1", Status: state.StatusWorking}

	d.Stop(state.StatusExited, "session ended", first)
	if d.FinishedAt == nil || !d.FinishedAt.Equal(first) {
		t.Fatalf("FinishedAt = %v, want the moment it stopped", d.FinishedAt)
	}
	// A second ending is not an ending.
	d.Stop(state.StatusDone, "deployed — live", time.Now())
	if !d.FinishedAt.Equal(first) {
		t.Errorf("FinishedAt moved to %v — a finished record finished once", d.FinishedAt)
	}
	if d.Status != state.StatusDone || d.StatusReason != "deployed — live" {
		t.Errorf("Stop did not apply the new status: %v %q", d.Status, d.StatusReason)
	}
}

// Held is the whole rule for what the triage table keeps: it ended under this
// build, and nobody has said they saw it.
func TestHeldNeedsAStampAndNoDismissal(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		d    state.Dispatch
		want bool
	}{
		{"still working", state.Dispatch{Status: state.StatusWorking,
			FinishedAt: ptrTime(now)}, false},
		{"finished under this build", state.Dispatch{Status: state.StatusExited,
			FinishedAt: ptrTime(now)}, true},
		{"finished and dismissed", state.Dispatch{Status: state.StatusDone,
			FinishedAt: ptrTime(now), DismissedAt: ptrTime(now)}, false},
		// The no-migration rule: a record that ended before this build existed
		// carries no stamp, so it was never held and goes straight to history.
		// Without it, shipping this would put a year of finished dispatchers on
		// the fleet at once.
		{"finished before this build", state.Dispatch{Status: state.StatusDone}, false},
	}
	for _, c := range cases {
		if got := c.d.Held(); got != c.want {
			t.Errorf("%s: Held() = %v, want %v", c.name, got, c.want)
		}
	}
}

// A record that is not finished cannot carry an ending or a reading of one.
// This is what clears both when a dispatcher is resumed: Save is the one place
// every path goes through.
func TestSaveClearsTheEndingOnAReopenedRecord(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	now := time.Now()
	d := &state.Dispatch{ID: "r2", Feature: "reopened", Status: state.StatusExited}
	d.Stop(state.StatusExited, "session ended", now)
	d.Dismiss(now)
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}

	d.Status = state.StatusWorking // what resume does
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}
	if d.FinishedAt != nil || d.DismissedAt != nil {
		t.Fatalf("a working record still carries an ending: %v / %v", d.FinishedAt, d.DismissedAt)
	}
	got := loadRecord(t, "r2")
	if got.FinishedAt != nil || got.DismissedAt != nil {
		t.Fatalf("the stale ending survived on disk: %+v", got)
	}
}

// ---- the table ----------------------------------------------------------------

// The defect: a dispatcher that finished moved itself from the triage table
// into history on the next poll. The human watching the one screen they watch
// saw a row vanish and a count go down, and was told nothing at all about the
// dispatch that had just ended.
func TestFinishedDispatcherHoldsItsRowUntilDismissed(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	now := time.Now()

	fleet = []fleetRow{
		{id: "id-run", kind: "run", rank: fleetRank("run", "normal"), feature: "running",
			moved: now.Add(-time.Minute), started: now.Add(-time.Hour)},
		{id: "id-held", kind: "done", rank: fleetDoneRank, feature: "just ended",
			signal: "merged", moved: now.Add(-2 * time.Minute), started: now.Add(-time.Hour),
			acts: []cqAct{
				{k: "⏎", d: "resume", ok: "resuming \"just ended\"…", keep: true},
				{k: "x", d: "dismiss", ok: "dismissed \"just ended\"", keep: true},
			}},
		{id: "id-past", kind: "past", rank: fleetPastRank, feature: "long gone",
			moved: now.Add(-40 * time.Hour), started: now.Add(-48 * time.Hour)},
	}

	m := newModel()
	m.width, m.height = 130, 40

	if got := fleetFeatures(m); got != "running,just ended" {
		t.Fatalf("fleet = %q — the finished dispatcher must stay on the table and history must not", got)
	}
	// Below everything still going, above history, under its own divider.
	if fleetDoneRank <= fleetRank("run", "normal") || fleetDoneRank >= fleetParkedRank {
		t.Errorf("fleetDoneRank %d is not between running and the shelf", fleetDoneRank)
	}
	if fleetGlyph(fleetDoneRank) != "✓" {
		t.Errorf("a finished row's glyph = %q", fleetGlyph(fleetDoneRank))
	}
	// It is not running, and must not be counted as running clean.
	wants, parked, finished, clean := fleetCount(m.fleetRows())
	if finished != 1 || clean != 1 || wants != 0 || parked != 0 {
		t.Errorf("fleetCount = %d,%d,%d,%d — a finished row is not running clean",
			wants, parked, finished, clean)
	}
	view := m.View()
	if !strings.Contains(view, "just ended") {
		t.Error("the finished dispatcher is not on the screen it finished on")
	}
	if !strings.Contains(view, "finished") {
		t.Error("nothing on screen names the group the finished row is in")
	}
	// And the key that takes it off says so, rather than offering a kill for a
	// session that has already ended.
	m = m.fleetTo(1)
	r, _ := m.fleetSel()
	if r.id != "id-held" {
		t.Fatalf("cursor landed on %q", r.id)
	}
	if !strings.Contains(m.cqFooterHelp(), "dismiss") {
		t.Errorf("footer on a finished row = %q", m.cqFooterHelp())
	}
}

// x on a finished row dismisses the ending; it must not reach killCmd, which
// would go looking for a session to take down and a worktree to clean up.
func TestDismissIsNotAKill(t *testing.T) {
	rec := &state.Dispatch{ID: "r3", Feature: "ended", Status: state.StatusExited,
		FinishedAt: ptrTime(time.Now())}
	acts := cqActs(rec, "done")
	var x cqAct
	for _, a := range acts {
		if a.k == "x" {
			x = a
		}
	}
	if x.d != "dismiss" {
		t.Fatalf("x on a finished row = %q, want dismiss", x.d)
	}
	// keep: the row goes when the record comes back saying it was dismissed,
	// not when the flash ends — the dismissal is written to disk.
	if !x.keep {
		t.Error("dismiss must keep the row until the record says otherwise")
	}
	// Resume is still the first act: dismissing is not losing.
	if acts[0].d != "resume" {
		t.Errorf("first act on a finished row = %q", acts[0].d)
	}
}

// The state dir is the only place a dismissal can live. Kept in the model it
// would come back on the next load — and come back for the whole fleet the
// moment the cockpit was restarted.
func TestDismissWritesTheRecord(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	saved := captureVars()
	defer restoreVars(saved)

	now := time.Now()
	rec := &state.Dispatch{ID: "r4", Feature: "ended", Status: state.StatusExited}
	rec.Stop(state.StatusExited, "session ended", now)
	if err := state.Save(rec); err != nil {
		t.Fatal(err)
	}
	liveByID = map[string]*state.Dispatch{"r4": rec}

	msg := dismissCmd("r4")()
	if a, ok := msg.(actionMsg); !ok || !strings.Contains(a.notice, "dismissed") {
		t.Fatalf("dismissCmd said %#v", msg)
	}
	got := loadRecord(t, "r4")
	if got.DismissedAt == nil {
		t.Fatalf("the dismissal did not reach the record: %+v", got)
	}
	if got.Held() {
		t.Error("a dismissed record is still held")
	}
}

// ---- the ordering -------------------------------------------------------------

// History was ordered by UpdatedAt, which Save stamps on every write. A
// finished record goes on being written for the rest of its life by things that
// are not the dispatcher doing anything, so the list read as "what we last
// touched": measured on a real store, five dispatchers that ended on five
// different days shared one UpdatedAt to the second because one sweep had
// written them all in a loop, and a dispatcher whose transcript had not moved
// in seven days sat at the top.
func TestFinishedRowsAreOrderedByWhenTheyWereLastWorked(t *testing.T) {
	now := time.Now()
	// The reconcile stamp: written this morning, last worked a week ago.
	swept := &state.Dispatch{
		ID: "swept", Status: state.StatusExited, UpdatedAt: now.Add(-time.Minute),
		TranscriptPath: touchTranscript(t, "swept.jsonl", now.Add(-7*24*time.Hour)),
	}
	// Worked an hour ago; its record has not been written since.
	recent := &state.Dispatch{
		ID: "recent", Status: state.StatusExited, UpdatedAt: now.Add(-24 * time.Hour),
		TranscriptPath: touchTranscript(t, "recent.jsonl", now.Add(-time.Hour)),
	}
	if a, b := fleetActed(swept), fleetActed(recent); !b.After(a) {
		t.Fatalf("fleetActed ordered the sweep above the work: %v vs %v", a, b)
	}
	rows := []fleetRow{
		{id: "swept", kind: "past", rank: fleetPastRank, moved: fleetActed(swept)},
		{id: "recent", kind: "past", rank: fleetPastRank, moved: fleetActed(recent)},
	}
	fleetSort(rows)
	if rows[0].id != "recent" {
		t.Errorf("history leads with %q — the one you last worked belongs at the top", rows[0].id)
	}
}

// With no transcript to read there is nothing else to go on, so the record's
// own clock stands rather than the row sorting as if it were from 1970.
func TestLastWorkedFallsBackToTheRecord(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	d := &state.Dispatch{ID: "no-transcript", Status: state.StatusExited, UpdatedAt: now}
	if got := fleetActed(d); !got.Equal(now) {
		t.Errorf("fleetActed = %v, want the record's own clock %v", got, now)
	}
}

// The live table asks a different question and keeps its answer: a working
// dispatcher's hook save is real liveness, and fleetMoved still takes the
// freshest of the two.
func TestRunningRowsStillTakeTheFreshestOfBoth(t *testing.T) {
	now := time.Now()
	d := &state.Dispatch{
		ID: "live", Status: state.StatusWorking, UpdatedAt: now,
		TranscriptPath: touchTranscript(t, "live.jsonl", now.Add(-time.Hour)),
	}
	if got := fleetMoved(d); got.Before(now) {
		t.Errorf("fleetMoved = %v, want the hook's own save %v", got, now)
	}
}

// ---- the divider ---------------------------------------------------------------

// Two groups sit below the live table now, each under its own rule. The
// dividers are display lines the cursor cannot land on, so the selection has to
// map across however many of them precede it — get that wrong and j/k highlight
// the wrong row.
func TestBothGroupsGetTheirOwnDivider(t *testing.T) {
	now := time.Now()
	rows := []fleetRow{
		{id: "a", kind: "queue", feature: "asking", moved: now},
		{id: "b", kind: "done", feature: "ended", moved: now},
		{id: "c", kind: "parked", feature: "shelved", moved: now},
	}
	body := fleetBody(160, fleetColumns(160, 10), rows, 2, 6, "")
	joined := strings.Join(body, "\n")
	if !strings.Contains(joined, "finished") || !strings.Contains(joined, "parked") {
		t.Fatalf("both dividers should be drawn:\n%s", joined)
	}
	// Five lines of content: three rows and two dividers, in order.
	want := []string{"asking", "finished", "ended", "parked", "shelved"}
	at := -1
	for _, w := range want {
		i := strings.Index(joined, w)
		if i <= at {
			t.Fatalf("%q is out of order in:\n%s", w, joined)
		}
		at = i
	}
}

// ---- the collector --------------------------------------------------------------

// The end-to-end path: collectFleet reads Held off the record, so a dispatcher
// that ended under this build is collected as a held row and the one that
// ended before it existed is still history.
func TestCollectFleetHoldsWhatEndedUnderThisBuild(t *testing.T) {
	env := buildEnvScenario(t)
	saved := captureVars()
	defer restoreVars(saved)

	// rec-exited ended under this build; rec-done predates it and carries no
	// stamp, which is what keeps a year of history off the fleet.
	for _, d := range state.LoadAll() {
		if d.ID == "rec-exited" {
			d.FinishedAt = ptrTime(time.Now().Add(-5 * time.Minute))
			if err := state.Save(d); err != nil {
				t.Fatal(err)
			}
		}
	}

	snap := loadSnapshot(env.cfg)
	byID := map[string]fleetRow{}
	for _, r := range snap.fleet {
		byID[r.id] = r
	}
	if got := byID["rec-exited"]; got.kind != "done" || got.rank != fleetDoneRank {
		t.Errorf("rec-exited = kind %q rank %d, want a held row", got.kind, got.rank)
	}
	if got := byID["rec-done"]; got.kind != "past" {
		t.Errorf("rec-done = kind %q, want past — it ended before this build existed", got.kind)
	}
}
