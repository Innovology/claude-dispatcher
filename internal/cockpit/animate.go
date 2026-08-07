package cockpit

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Timing constants, taken from the mock's setInterval/setTimeout values.
const (
	followInterval = 900 * time.Millisecond
	shipInterval   = 80 * time.Millisecond
	landLinger     = 2800 * time.Millisecond
	undoLinger     = 9 * time.Second
)

// burst is the merge-animation glyph sequence played over the shipping row.
var burst = []string{"◦", "✦", "✳", "✷", "✺", "✷", "✳", "✦", "◦", "", "", "", "", ""}

func followTick() tea.Cmd {
	return tea.Tick(followInterval, func(time.Time) tea.Msg { return followTickMsg{} })
}

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
		m.shipped[feature] = true
		if m.cursor > 0 {
			m.cursor--
		}
		m.notice = "✓ " + feature + " is live · 6m 14s from approve to production"
		return m, tea.Tick(landLinger, func(time.Time) tea.Msg { return landClearMsg{} })
	}
	return m, shipTick()
}

// startFollow toggles live-tail following; returns a tick when turning on.
func (m model) startFollow() (model, tea.Cmd) {
	m.follow = !m.follow
	m.tailN = 3
	if m.follow {
		return m, followTick()
	}
	return m, nil
}
