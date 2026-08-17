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

// ageEvery paces the clock tick — the one message whose whole job is to make
// the screen say a later time than it did a second ago.
//
// Nothing on a Bubble Tea screen ages by itself. View is called in response to
// a message and at no other moment, so an age computed inside it is only ever
// recomputed when something else happens to send one — and between keystrokes
// the only thing that regularly does is the poll above, once a minute. The
// triage table counts its ages in seconds (cqAge), so a row that had just
// reported "4s" sat on "4s" until the next poll and then jumped most of a
// minute. The reading was never wrong when it was drawn; it was drawn once and
// then left there, which is worse, because a seconds column that is not moving
// is the display a human trusts most.
//
// One second, because that is the resolution the column prints. The tick is as
// cheap as its rate demands: it reads no file, makes no request, reconciles
// nothing and rebuilds no snapshot — the handler re-arms it and returns the
// model untouched, and the renderer's next frame re-reads the clock. Where
// refreshTickMsg is a poll and pays for one, this is a redraw and pays for a
// redraw.
const ageEvery = time.Second

type (
	// snapshotMsg carries a freshly built snapshot to the UI goroutine.
	snapshotMsg snapshot
	// stateChangedMsg fires when the dispatch state dir changes on disk.
	stateChangedMsg struct{}
	// refreshTickMsg is the periodic poll.
	refreshTickMsg struct{}
	// ageTickMsg is the clock tick: a redraw and nothing else.
	ageTickMsg struct{}
	// trackedMsg fires after a track.Refresh pass (PR/deploy reconciliation).
	trackedMsg struct{}
	// bootProgressMsg carries one opening-screen step transition to the UI.
	bootProgressMsg bootUpdate
	// bootTickMsg paces the opening screen's spinner and colour sweep.
	bootTickMsg struct{}
	// bootDoneMsg retires the opening screen and hands over to the cockpit.
	bootDoneMsg struct{}
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
//     SessionEnd out stops being reported as working. loadSnapshot sweeps too,
//     on every load; this one is here for the order below it, not because the
//     sweep is this path's alone — it was, and a fleet of dispatchers whose
//     sessions had died could not raise it, because the only way in was the
//     jump-in they could no longer make;
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

// bootLoadCmd is the first load, narrating itself to the opening screen. The
// snapshot it produces is identical to loadSnapshotCmd's.
//
// Sends are non-blocking over a buffered channel, so a screen that has been
// skipped — or a UI busy elsewhere — can never hold the load up waiting to be
// watched. A dropped update costs one line's figure, never a step.
func bootLoadCmd(cfg *config.Config, ch chan bootUpdate) tea.Cmd {
	return func() tea.Msg {
		report := func(u bootUpdate) {
			select {
			case ch <- u:
			default:
			}
		}
		s := loadSnapshotReporting(cfg, report)
		// Every send happens inside the call above, on this goroutine, so the
		// channel is safe to close here — and closing it is what releases the
		// waitBoot that would otherwise sit on it for the life of the process.
		close(ch)
		return snapshotMsg(s)
	}
}

// waitBoot blocks until the loader reports its next step. A closed channel ends
// the subscription: the load is over and nothing more will be reported.
func waitBoot(ch chan bootUpdate) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		u, ok := <-ch
		if !ok {
			return nil
		}
		return bootProgressMsg(u)
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

func ageTick() tea.Cmd {
	return tea.Tick(ageEvery, func(time.Time) tea.Msg { return ageTickMsg{} })
}
