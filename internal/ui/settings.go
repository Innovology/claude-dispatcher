package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/repos"
)

// settings edits the repo scan roots. It opens on first run (no config yet)
// and any time via `s`; saving rewrites config.toml and re-runs discovery.
type settingsResult int

const (
	settingsActive settingsResult = iota
	settingsCancelled
	settingsSaved
)

type settings struct {
	roots    []string
	cursor   int
	adding   bool
	input    textinput.Model
	firstRun bool
	errMsg   string
	// counts maps a root (as typed) to how many repos discovery finds under
	// it; -1 while a recount is in flight.
	counts map[string]int
}

type rootCountsMsg map[string]int

func newSettings(roots []string, firstRun bool) *settings {
	input := textinput.New()
	input.Placeholder = "~/path/to/your/repos"
	input.CharLimit = 200
	s := &settings{
		roots:    append([]string(nil), roots...),
		firstRun: firstRun,
		input:    input,
		counts:   map[string]int{},
	}
	for _, r := range s.roots {
		s.counts[r] = -1
	}
	return s
}

func (s *settings) resize(width, _ int) {
	s.input.Width = min(width-8, 100)
}

// countRoots discovers each root independently so the settings view can show
// per-root repo counts.
func countRoots(roots []string) tea.Cmd {
	rs := append([]string(nil), roots...)
	return func() tea.Msg {
		counts := map[string]int{}
		for _, r := range rs {
			counts[r] = len(repos.Discover(&config.Config{Roots: []string{r}}))
		}
		return rootCountsMsg(counts)
	}
}

func (s *settings) update(msg tea.KeyMsg) (settingsResult, tea.Cmd) {
	s.errMsg = ""
	if s.adding {
		return s.updateAdding(msg)
	}
	switch msg.String() {
	case "esc", "q":
		return settingsCancelled, nil
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.roots)-1 {
			s.cursor++
		}
	case "a", "n":
		s.adding = true
		return settingsActive, s.input.Focus()
	case "x", "d", "backspace":
		if len(s.roots) > 0 {
			s.roots = append(s.roots[:s.cursor], s.roots[s.cursor+1:]...)
			s.cursor = min(s.cursor, max(0, len(s.roots)-1))
		}
	case "enter":
		if len(s.roots) == 0 {
			s.errMsg = "at least one root is required — press a to add one"
			return settingsActive, nil
		}
		return settingsSaved, nil
	}
	return settingsActive, nil
}

func (s *settings) updateAdding(msg tea.KeyMsg) (settingsResult, tea.Cmd) {
	switch msg.String() {
	case "esc":
		s.adding = false
		s.input.Reset()
		s.input.Blur()
		return settingsActive, nil
	case "enter":
		root := strings.TrimSpace(s.input.Value())
		if root == "" {
			s.errMsg = "enter a directory path"
			return settingsActive, nil
		}
		if info, err := os.Stat(config.ExpandHome(root)); err != nil || !info.IsDir() {
			s.errMsg = "not a directory: " + root
			return settingsActive, nil
		}
		for _, r := range s.roots {
			if r == root {
				s.errMsg = "already listed: " + root
				return settingsActive, nil
			}
		}
		s.roots = append(s.roots, root)
		s.counts[root] = -1
		s.cursor = len(s.roots) - 1
		s.adding = false
		s.input.Reset()
		s.input.Blur()
		return settingsActive, countRoots(s.roots)
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return settingsActive, cmd
}
