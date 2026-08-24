package cockpit

// pending_test.go guards the window a dispatch used to spend invisible: from
// the key that starts it to the record that proves it started. See pending.go.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// stubLaunch swaps the form's hand-off so a submit can be driven all the way
// through without starting a real worktree or tmux session, and reports what it
// was passed.
func stubLaunch(t *testing.T) *struct{ repo, feature, prompt string } {
	t.Helper()
	got := &struct{ repo, feature, prompt string }{}
	prev := dxLaunch
	dxLaunch = func(_ *config.Config, repo, feature, prompt string, _ dispatchpkg.Mode, _ dispatchpkg.Model, _ bool) tea.Cmd {
		got.repo, got.feature, got.prompt = repo, feature, prompt
		return nil
	}
	t.Cleanup(func() { dxLaunch = prev })
	return got
}

// submitting is a model that has just dispatched "retry backoff" into a seeded
// repo, with nothing else in flight.
func submitting(t *testing.T) model {
	t.Helper()
	stubLaunch(t)
	m := newModel()
	m.width, m.height = 130, 40
	m.cfg = &config.Config{Roots: []string{seedRepoRoot(t, "alpha-api")}}
	m = press(m, "d")
	m.dxField, m.dxTitle = dxWhatF, "retry backoff"
	m.dxWhat, m.dxGoal = "retry the declined charges on a backoff", "ci is green"
	m, _ = m.dxSubmit()
	return m
}

// The whole complaint: a dispatch went in and nothing appeared until the launch
// had finished fetching, building a worktree and starting tmux. It is on the
// table on the same keystroke now.
func TestDispatchAppearsBeforeItHasLaunched(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = nil

	m := submitting(t)

	rows := m.fleetRows()
	if len(rows) != 1 {
		t.Fatalf("fleetRows() = %d rows, want the dispatch that was just asked for", len(rows))
	}
	r := rows[0]
	if r.feature != "retry backoff" || r.repo != "alpha-api" {
		t.Errorf("row = %q in %q", r.feature, r.repo)
	}
	if r.signal != startingSignal {
		t.Errorf("signal = %q, want %q", r.signal, startingSignal)
	}
	if r.ref != "feature/retry-backoff" {
		t.Errorf("ref = %q, want the branch it will appear on", r.ref)
	}
	// It is not asking for anything, so it must not be counted among the rows
	// that are — the headline above the table is read as a count of demands.
	if wants, _, _ := fleetCount(rows); wants != 0 {
		t.Errorf("a starting dispatcher wants you %d times", wants)
	}
	// And it offers no keys, because there is nothing behind them yet.
	if len(r.acts) != 0 {
		t.Errorf("acts = %v, want none until there is a session", r.acts)
	}

	// The triage lens shows the dispatch form whenever the table is empty, so
	// before this the screen went straight back to a blank form — the reading
	// the human is least able to tell apart from a failure.
	if m.cqPromptOn() {
		t.Error("the form is still up over a table that now has a row on it")
	}
	if !strings.Contains(m.viewCQ(m.width, 30), "retry backoff") {
		t.Error("the triage lens does not draw the dispatch that was just made")
	}
}

// A launch that failed keeps its row, and the row says why.
//
// This used to delete it, which is the disappearance the whole file is named
// after — in its worst form. A launch can fail before dispatch.Launch writes
// anything at all (a feature already live, a worktree its last session left on
// another branch, a supervisor that would not take the command), so there is no
// record, no worktree and nothing in history: that row was the only thing on
// screen that knew the dispatch had been asked for. Taking it away left a
// one-line notice against a table that looked exactly as it had before.
func TestFailedLaunchKeepsTheRowAndSaysWhy(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = nil

	m := submitting(t)
	if len(m.pending) != 1 {
		t.Fatalf("pending = %d before the launch reports", len(m.pending))
	}

	next, _ := m.Update(launchedMsg{feature: "retry backoff",
		notice: "launch failed: no remote", failed: true, reason: "no remote"})
	m = next.(model)

	rows := m.fleetRows()
	if len(rows) != 1 || len(m.pending) != 1 {
		t.Fatalf("the failed dispatch vanished: %d rows, %d pending", len(rows), len(m.pending))
	}
	if rows[0].signal != failedSignal {
		t.Errorf("row signal = %q, want %q", rows[0].signal, failedSignal)
	}
	if rows[0].why != "no remote" {
		t.Errorf("the row does not say why it failed: %q", rows[0].why)
	}
	if rows[0].tone != "red" {
		t.Errorf("tone = %q — a dispatch that did not happen is not a normal row", rows[0].tone)
	}
	if m.notice != "launch failed: no remote" {
		t.Errorf("notice = %q", m.notice)
	}
	// And it is on the screen, not merely in the model.
	if !strings.Contains(m.viewCQ(m.width, 30), failedSignal) {
		t.Error("the triage lens does not draw the launch that failed")
	}

	// No snapshot retires it: the thing it reports is that there is nothing for
	// a snapshot to find. Not even one that read the records long afterwards.
	m = m.prunePending(time.Now().Add(time.Hour))
	if len(m.pending) != 1 {
		t.Error("a snapshot retired the report of a dispatch it knows nothing about")
	}
}

// The human takes it off, and that is the only thing that does.
func TestDismissingAFailedLaunchIsTheOnlyWayItGoes(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = nil

	m := submitting(t)
	next, _ := m.Update(launchedMsg{feature: "retry backoff",
		notice: "launch failed: no remote", failed: true, reason: "no remote"})
	m = next.(model)

	row, ok := m.fleetSel()
	if !ok {
		t.Fatal("nothing under the cursor")
	}
	if len(row.acts) != 1 || row.acts[0].k != "x" {
		t.Fatalf("acts = %#v, want the one act a row with no record behind it can honour", row.acts)
	}
	// Through the act loop, not by calling dropPending: the point is that the
	// key the row advertises reaches it.
	m, _ = m.cqRun(row, row.acts[0])
	if len(m.pending) != 0 || len(m.fleetRows()) != 0 {
		t.Errorf("x did not dismiss the row: %d rows, %d pending", len(m.fleetRows()), len(m.pending))
	}
}

// A launch that succeeded keeps its row until the table can speak for itself.
// Dropping it on the success message would blank the row again for as long as
// the next snapshot takes, which is the same disappearance in a smaller window.
func TestSuccessfulLaunchKeepsTheRowUntilTheRecordLands(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = nil

	m := submitting(t)
	next, _ := m.Update(launchedMsg{feature: "retry backoff", notice: "dispatched \"retry backoff\" → alpha-api"})
	m = next.(model)
	if len(m.fleetRows()) != 1 {
		t.Fatalf("the row vanished on a successful launch: %d rows", len(m.fleetRows()))
	}
	m.fleetSelID = pendingID("retry backoff")

	// The record lands: the table now carries a real row for the same feature.
	fleet = []fleetRow{{id: "rec-1", kind: "run", rank: 2, feature: "retry backoff", repo: "alpha-api", signal: startingSignal}}
	m = m.prunePending(time.Now()).fleetSync()

	rows := m.fleetRows()
	if len(rows) != 1 || rows[0].id != "rec-1" {
		t.Fatalf("want exactly the record's row, got %d rows: %+v", len(rows), rows)
	}
	if len(m.pending) != 0 {
		t.Errorf("the placeholder outlived the record it stood in for")
	}
	// The human's selection was on this dispatcher, not on this row object.
	if m.fleetSelID != "rec-1" {
		t.Errorf("fleetSelID = %q, want the row that replaced the placeholder", m.fleetSelID)
	}
}

// The other end of the same wait: the record exists, no hook has fired for it,
// and cqShipDetail has nothing to say about a dispatcher with no PR. A blank
// SIGNAL there reads as "no news" on the row being watched hardest.
func TestLaunchingRecordSaysItIsStarting(t *testing.T) {
	rec := &state.Dispatch{
		ID: "rec-1", Feature: "retry backoff", RepoName: "alpha-api",
		Branch: "feature/retry-backoff", Status: state.StatusLaunching,
		UpdatedAt: time.Now(),
	}
	r, _ := fleetRunRow(&collectCtx{}, &snapshot{}, map[string]dispatch{}, map[string]int{}, rec)
	if r.signal != startingSignal {
		t.Errorf("signal = %q, want %q", r.signal, startingSignal)
	}

	// And it stops saying it the moment the session reports in.
	rec.Status = state.StatusWorking
	r, _ = fleetRunRow(&collectCtx{}, &snapshot{}, map[string]dispatch{}, map[string]int{}, rec)
	if r.signal == startingSignal {
		t.Error("a working dispatcher is still claiming to be starting")
	}
}

// A note that has been asked for but not reported on survives a snapshot: the
// launch is still running and nothing has happened to make it untrue.
func TestUnreportedPendingSurvivesASnapshot(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = nil

	m := submitting(t)
	m = m.prunePending(time.Now())
	if len(m.pending) != 1 {
		t.Error("a launch still in flight had its row pruned")
	}

	// Once it has reported success, a snapshot that read the records after that
	// is the hand-over: whatever the table says about this feature — including
	// nothing — is the record's answer and outranks ours.
	m = m.settlePending("retry backoff")
	m = m.prunePending(time.Now())
	if len(m.pending) != 0 {
		t.Error("a launched dispatch is still being described by its placeholder")
	}
}

// The disappearance itself, in the one gap the placeholder left open.
//
// A snapshot load takes seconds — measured at 4.5s warm and 61s cold on a real
// portfolio — and one is very often already out when a dispatch lands, having
// read the dispatch records before the record existed. It returns after the
// launch reports success, carrying a fleet with no such dispatcher in it. The
// placeholder used to retire on any snapshot at all, so the row went with it:
// the dispatch the human had just made was gone from the screen, and with
// nothing else in flight the lens fell back to the blank dispatch form.
func TestAStaleSnapshotCannotTakeTheDispatchAway(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = nil

	m := submitting(t)
	before := time.Now() // a load that read the records before the launch landed
	next, _ := m.Update(launchedMsg{feature: "retry backoff", notice: "dispatched"})
	m = next.(model)

	// It lands after the launch reported, and knows nothing about it.
	m = m.prunePending(before).fleetSync()
	if len(m.pending) != 1 {
		t.Fatal("a snapshot that predates the record retired its placeholder")
	}
	rows := m.fleetRows()
	if len(rows) != 1 || rows[0].feature != "retry backoff" {
		t.Fatalf("the dispatch disappeared: %d rows", len(rows))
	}
	if m.cqPromptOn() {
		t.Error("the lens fell back to the dispatch form over a dispatch that is still starting")
	}

	// And the first load that did read the disk after the record hands over.
	fleet = []fleetRow{{id: "rec-1", kind: "run", rank: 2, feature: "retry backoff", repo: "alpha-api"}}
	m = m.prunePending(time.Now()).fleetSync()
	if len(m.pending) != 0 {
		t.Error("the placeholder outlived the snapshot that could see the record")
	}
}

// The same thing again, through Update, with the snapshot arriving the way a
// real one does: the whole path from the key to the screen.
func TestTheDispatchSurvivesAStaleSnapshotLanding(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = nil

	m := submitting(t)
	// A load that read the records before any of this — it is out, and will
	// return knowing nothing about the dispatch about to be made.
	stale := snapshot{seq: 1, dataMode: "live", fleet: []fleetRow{}, recordsAt: time.Now()}

	next, _ := m.Update(launchedMsg{feature: "retry backoff", notice: "dispatched"})
	next, _ = next.(model).Update(snapshotMsg(stale))
	m = next.(model)

	rows := m.fleetRows()
	if len(rows) != 1 || rows[0].feature != "retry backoff" {
		t.Fatalf("the dispatch vanished when a stale snapshot landed: %d rows", len(rows))
	}

	// The load that reads the records after the launch is the hand-over, and
	// this one carries the record's own row.
	fresh := snapshot{
		seq: 2, dataMode: "live", recordsAt: time.Now(),
		fleet: []fleetRow{{id: "rec-1", kind: "run", rank: 2, feature: "retry backoff", repo: "alpha-api"}},
	}
	next, _ = m.Update(snapshotMsg(fresh))
	m = next.(model)
	if len(m.pending) != 0 {
		t.Error("the placeholder outlived the snapshot that could see the record")
	}
	if rows := m.fleetRows(); len(rows) != 1 || rows[0].id != "rec-1" {
		t.Errorf("want the record's own row, got %+v", rows)
	}
}

// Two dispatches under one name are one dispatch: Launch refuses the second, so
// two rows would be two claims about one thing.
func TestPendingIsKeyedByFeature(t *testing.T) {
	m := newModel()
	m = m.markPending(pendingDispatch{feature: "one", repo: "a"})
	m = m.markPending(pendingDispatch{feature: "two", repo: "a"})
	m = m.markPending(pendingDispatch{feature: "one", repo: "b"})
	if len(m.pending) != 2 {
		t.Fatalf("pending = %d, want one per feature name", len(m.pending))
	}
	if m.pending[1].feature != "one" || m.pending[1].repo != "b" {
		t.Errorf("the second ask under a live name did not replace the first: %+v", m.pending)
	}
}
