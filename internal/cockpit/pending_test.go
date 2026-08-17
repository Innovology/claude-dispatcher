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
	if wants, _, _, _ := fleetCount(rows); wants != 0 {
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

// A launch that failed leaves no record and never will, so the row goes with
// the notice that says so — a screen still promising "starting session" under
// "launch failed" would be contradicting itself.
func TestFailedLaunchTakesTheRowBack(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = nil

	m := submitting(t)
	if len(m.pending) != 1 {
		t.Fatalf("pending = %d before the launch reports", len(m.pending))
	}

	next, _ := m.Update(launchedMsg{feature: "retry backoff", notice: "launch failed: no remote", failed: true})
	m = next.(model)
	if len(m.fleetRows()) != 0 || len(m.pending) != 0 {
		t.Errorf("the row survived a failed launch: %d rows, %d pending", len(m.fleetRows()), len(m.pending))
	}
	if m.notice != "launch failed: no remote" {
		t.Errorf("notice = %q", m.notice)
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
	fleet = []fleetRow{{id: "rec-1", kind: "run", rank: 3, feature: "retry backoff", repo: "alpha-api", signal: startingSignal}}
	m = m.prunePending().fleetSync()

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
	m = m.prunePending()
	if len(m.pending) != 1 {
		t.Error("a launch still in flight had its row pruned")
	}

	// Once it has reported success, the next snapshot is the hand-over: the
	// records were re-read in it, so whatever the table says about this feature
	// — including nothing — is the record's answer and outranks ours.
	m = m.settlePending("retry backoff").prunePending()
	if len(m.pending) != 0 {
		t.Error("a launched dispatch is still being described by its placeholder")
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
