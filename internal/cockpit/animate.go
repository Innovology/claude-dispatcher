package cockpit

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Timing constants, taken from the mock's setInterval/setTimeout values.
const (
	shipInterval  = 80 * time.Millisecond
	landLinger    = 2800 * time.Millisecond
	undoLinger    = 9 * time.Second
	cqFlashLinger = 850 * time.Millisecond
)

func shipTick() tea.Cmd {
	return tea.Tick(shipInterval, func(time.Time) tea.Msg { return shipTickMsg{} })
}

// startShip begins the merge→live animation for x and returns the first tick.
func (m model) startShip(x dispatch) (model, tea.Cmd) {
	if m.shipFx != nil {
		return m, nil
	}
	m.shipFx = &shipFxState{feature: x.feature, repo: x.repo, frame: 0}
	m.notice = "merging " + x.feature + "…"
	return m, shipTick()
}

// advanceShip steps the merge animation one frame; on the last frame it marks
// the feature shipped, flags it as just-landed and schedules the land clear.
func (m model) advanceShip() (model, tea.Cmd) {
	f := m.shipFx
	if f == nil {
		return m, nil
	}
	f.frame++
	if f.frame > 13 {
		feature := f.feature
		m.shipFx = nil
		m.justLanded = feature
		m.notice = "✓ " + feature + " is live · 6m 14s from approve to production"
		return m, tea.Tick(landLinger, func(time.Time) tea.Msg { return landClearMsg{} })
	}
	return m, shipTick()
}
