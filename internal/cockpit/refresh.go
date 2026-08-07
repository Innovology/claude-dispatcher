package cockpit

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/track"
)

// refreshEvery paces the periodic PR/deploy poll; fsnotify drives faster
// updates when a record changes on disk.
const refreshEvery = 15 * time.Second

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
