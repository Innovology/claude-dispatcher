package cockpit

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// `: tidy` removes the worktrees finished dispatchers left on disk — see
// dispatch.Tidy for what it will and will not take. It is two background
// steps around the confirm bar: reading what a tidy would do costs a few git
// calls per worktree (fourteen seconds across a hundred on the reporting
// machine), and doing it deletes node_modules trees measured in gigabytes, so
// neither may hold the screen.

// planTidy and doTidy are seams: tests swap them rather than stand up repos.
var (
	planTidy = func() []dispatchpkg.TidyItem {
		ds := state.LoadAll()
		// A record still claiming "working" over a session that is gone would
		// keep its folder; the ghost sweep every load runs settles it first.
		reconcileSessions(ds)
		return dispatchpkg.PlanTidy(ds)
	}
	doTidy = dispatchpkg.Tidy
)

type tidyPlannedMsg struct{ items []dispatchpkg.TidyItem }

func tidyPlanCmd() tea.Cmd {
	return func() tea.Msg { return tidyPlannedMsg{items: planTidy()} }
}

func tidyRunCmd(items []dispatchpkg.TidyItem) tea.Cmd {
	return func() tea.Msg {
		done := doTidy(items)
		c := tidyCount(done)
		notice := fmt.Sprintf("tidied · removed %d %s", c.remove, plural(c.remove, "worktree", "worktrees"))
		if c.strip > 0 {
			notice += fmt.Sprintf(" · node_modules gone from %d kept for uncommitted work", c.strip)
		}
		if c.failed > 0 {
			notice += fmt.Sprintf(" · %d could not be removed (claude-dispatcher tidy says why)", c.failed)
		}
		return actionMsg{notice: notice}
	}
}

// startTidy is the palette's `tidy`: read the plan, then put it on the confirm
// bar. A second press while the first is still reading is the same request.
func (m model) startTidy() (model, tea.Cmd) {
	if m.tidyReading {
		m.notice = "still reading the worktrees…"
		return m, nil
	}
	m.tidyReading = true
	m.notice = "reading the worktrees finished dispatchers left behind…"
	return m, tidyPlanCmd()
}

func (m model) onTidyPlanned(msg tidyPlannedMsg) (model, tea.Cmd) {
	m.tidyReading = false
	c := tidyCount(msg.items)
	if c.remove+c.strip == 0 {
		m.notice = "nothing to tidy"
		if c.keep > 0 {
			m.notice += fmt.Sprintf(" · %d %s kept (claude-dispatcher tidy says why)", c.keep, plural(c.keep, "worktree", "worktrees"))
		}
		return m, nil
	}
	var parts []string
	if c.remove > 0 {
		parts = append(parts, fmt.Sprintf("remove %d finished %s with their node_modules", c.remove, plural(c.remove, "worktree", "worktrees")))
	}
	if c.strip > 0 {
		parts = append(parts, fmt.Sprintf("strip node_modules from %d with uncommitted work", c.strip))
	}
	if c.keep > 0 {
		parts = append(parts, fmt.Sprintf("%d kept", c.keep))
	}
	m.confirm = &confirmState{kind: "tidy", label: "tidy: " + strings.Join(parts, " · "), tidy: msg.items}
	return m, nil
}

type tidyCounts struct{ remove, strip, keep, failed int }

func tidyCount(items []dispatchpkg.TidyItem) tidyCounts {
	var c tidyCounts
	for _, it := range items {
		switch {
		case it.Err != "" && it.Action == dispatchpkg.TidyKeep:
			c.failed++
		case it.Action == dispatchpkg.TidyRemove:
			c.remove++
		case it.Action == dispatchpkg.TidyStrip:
			c.strip++
		default:
			c.keep++
		}
	}
	return c
}
