package cockpit

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/track"
)

// refreshEvery paces the periodic PR/deploy poll; fsnotify drives immediate
// updates when a record changes on disk, so this only has to catch changes
// made on the forge. It was 15s, which — before the gh layer was cached —
// started a fresh portfolio-wide fetch roughly four times a minute, faster
// than one could finish.
const refreshEvery = 60 * time.Second

type (
	// snapshotMsg carries a freshly built snapshot to the UI goroutine.
	snapshotMsg snapshot
	// stateChangedMsg fires when the dispatch state dir changes on disk.
	stateChangedMsg struct{}
	// refreshTickMsg is the periodic poll.
	refreshTickMsg struct{}
	// trackedMsg fires after a track.Refresh pass (PR/deploy reconciliation).
	trackedMsg struct{}
)

// loadSnapshotCmd rebuilds the whole snapshot off the UI goroutine.
func loadSnapshotCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg { return snapshotMsg(loadSnapshot(cfg)) }
}

// recheckCmd is the reload for a cockpit that has not been looking.
//
// loadSnapshotCmd is a poll's reload: it re-reads the dispatch records, but it
// serves every forge signal out of gh's memo cache and takes each record's
// status at its word. That is right when the cockpit has been on screen the
// whole time, because nothing has happened that it did not already see land.
//
// It is wrong the moment the human comes back from jumping into a session. They
// have just spent minutes inside it driving it by hand — approving a permission,
// answering a question, pushing, opening or merging a PR, or ending the session
// outright — so the cached PR and check state predates everything they did, and
// the status on disk is only as fresh as the last hook that got to fire. The
// screen would come back up showing what was true before they left, which is
// precisely the reading they are least able to catch: it looks like an answer.
//
// So this one assumes the world moved, and re-establishes each layer in the
// order the next one depends on:
//
//  1. drop the forge cache, so checks, reviews and PR state are read again
//     rather than replayed from before the jump (shipCmd invalidates for the
//     same reason after a merge — a hand at the wheel is no less of a change);
//  2. re-check session liveness, so a session that died without getting a
//     SessionEnd out stops being reported as working;
//  3. reconcile PRs and deploys against that, and in that order — track.midWork
//     reads status, so a record the sweep just retired can still be flipped to
//     done by its merge, where the reverse order would hold it open;
//  4. rebuild the snapshot from the result.
//
// One command rather than a chain of messages, because the sequence is the
// point: a snapshot built before the sweep landed would show the stale status
// this exists to clear.
func recheckCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		gh.InvalidateCache()
		dispatchpkg.ReconcileSessions(state.LoadAll())
		track.Refresh(state.LoadAll(), cfg)
		return snapshotMsg(loadSnapshot(cfg))
	}
}

// trackRefreshCmd reconciles PR and deploy state, persisting any change; the
// fsnotify watcher then reloads the records like any other status change.
func trackRefreshCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		track.Refresh(state.LoadAll(), cfg)
		return trackedMsg{}
	}
}

// waitState blocks until the state directory changes, then signals a reload.
func waitState(ch chan struct{}) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		<-ch
		return stateChangedMsg{}
	}
}

func refreshTick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return refreshTickMsg{} })
}
