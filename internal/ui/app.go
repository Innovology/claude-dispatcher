// Package ui is the cockpit: a stateless Bubble Tea viewer over the state
// directory. It launches dispatchers, watches their records via fsnotify, and
// hands the terminal over to tmux to jump into a session.
package ui

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/ship"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/tmux"
)

type mode int

const (
	modeList mode = iota
	modeForm
)

const (
	refreshEvery = 15 * time.Second
	shipEvery    = 3 * time.Minute
)

type model struct {
	width, height int
	cfg           *config.Config
	repos         []repos.Repo
	dispatches    []*state.Dispatch
	selectedID    string
	cursor        int
	mode          mode
	form          *form
	ship          ship.Stats
	stateCh       chan struct{}
	notice        string
}

type (
	dispatchesMsg   []*state.Dispatch
	shipMsg         ship.Stats
	stateChangedMsg struct{}
	tickMsg         struct{}
	shipTickMsg     struct{}
	launchedMsg     struct {
		d   *state.Dispatch
		err error
	}
	attachReturnedMsg struct{ err error }
)

func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return errors.New("no config found — run `claude-dispatcher init` first")
	}
	if !tmux.Available() {
		return errors.New("tmux not found on PATH — it is required")
	}
	if err := state.EnsureDirs(); err != nil {
		return err
	}

	stateCh := make(chan struct{}, 1)
	watcher, err := fsnotify.NewWatcher()
	if err == nil {
		if err := watcher.Add(state.DispatchesDir()); err == nil {
			go forwardEvents(watcher, stateCh)
			defer watcher.Close()
		}
	}

	m := model{
		cfg:     cfg,
		repos:   repos.Discover(cfg),
		stateCh: stateCh,
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// forwardEvents coalesces fsnotify chatter into at most one pending signal.
func forwardEvents(w *fsnotify.Watcher, ch chan struct{}) {
	for {
		select {
		case _, ok := <-w.Events:
			if !ok {
				return
			}
			select {
			case ch <- struct{}{}:
			default:
			}
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		loadDispatches,
		collectShip(m.repos),
		waitState(m.stateCh),
		tick(),
		shipTick(),
	)
}

// loadDispatches reads all records, reconciling any whose tmux session has
// died without a SessionEnd hook firing.
func loadDispatches() tea.Msg {
	ds := state.LoadAll()
	for _, d := range ds {
		switch d.Status {
		case state.StatusDone, state.StatusExited:
			continue
		}
		if !tmux.HasSession(d.TmuxSession) {
			d.Status = state.StatusExited
			d.StatusReason = "tmux session gone"
			state.Save(d)
		}
	}
	return dispatchesMsg(state.LoadAll())
}

func collectShip(rs []repos.Repo) tea.Cmd {
	return func() tea.Msg { return shipMsg(ship.Collect(rs)) }
}

func waitState(ch chan struct{}) tea.Cmd {
	return func() tea.Msg { <-ch; return stateChangedMsg{} }
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func shipTick() tea.Cmd {
	return tea.Tick(shipEvery, func(time.Time) tea.Msg { return shipTickMsg{} })
}

func launch(r repos.Repo, feature, prompt string) tea.Cmd {
	return func() tea.Msg {
		d, err := dispatch.Launch(r, feature, prompt)
		return launchedMsg{d: d, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.form != nil {
			m.form.resize(m.width, m.height)
		}
		return m, nil

	case stateChangedMsg:
		return m, tea.Batch(loadDispatches, waitState(m.stateCh))

	case tickMsg:
		return m, tea.Batch(loadDispatches, tick())

	case shipTickMsg:
		return m, tea.Batch(collectShip(m.repos), shipTick())

	case dispatchesMsg:
		m.dispatches = msg
		m.cursor = m.findSelected()
		return m, nil

	case shipMsg:
		m.ship = ship.Stats(msg)
		return m, nil

	case launchedMsg:
		m.mode = modeList
		m.form = nil
		if msg.err != nil {
			m.notice = "launch failed: " + msg.err.Error()
			return m, loadDispatches
		}
		m.selectedID = msg.d.ID
		m.notice = fmt.Sprintf("dispatched %q to %s", msg.d.Feature, msg.d.RepoName)
		return m, loadDispatches

	case attachReturnedMsg:
		m.notice = ""
		if msg.err != nil {
			m.notice = "attach failed: " + msg.err.Error()
		}
		return m, loadDispatches

	case tea.KeyMsg:
		if m.mode == modeForm {
			return m.updateForm(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.dispatches)-1 {
			m.cursor++
		}
	case "n":
		f := newForm(m.repos)
		f.resize(m.width, m.height)
		m.form = f
		m.mode = modeForm
	case "r":
		return m, tea.Batch(loadDispatches, collectShip(m.repos))
	case "enter", "a":
		if d := m.selected(); d != nil {
			if !tmux.HasSession(d.TmuxSession) {
				m.notice = "no live tmux session for this dispatcher"
				return m, nil
			}
			return m, tea.ExecProcess(tmux.AttachCmd(d.TmuxSession), func(err error) tea.Msg {
				return attachReturnedMsg{err: err}
			})
		}
	case "d":
		if d := m.selected(); d != nil {
			d.Status = state.StatusDone
			d.StatusReason = "marked shipped"
			state.Save(d)
			m.notice = fmt.Sprintf("%q marked shipped", d.Feature)
			return m, loadDispatches
		}
	case "x":
		if d := m.selected(); d != nil {
			tmux.KillSession(d.TmuxSession)
			if d.Status != state.StatusDone {
				d.Status = state.StatusExited
				d.StatusReason = "killed from cockpit"
			}
			state.Save(d)
			m.notice = fmt.Sprintf("killed %q", d.Feature)
			return m, loadDispatches
		}
	}
	if d := m.selected(); d != nil {
		m.selectedID = d.ID
	}
	return m, nil
}

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	done, cmd := m.form.update(msg)
	switch done {
	case formCancelled:
		m.mode = modeList
		m.form = nil
		return m, nil
	case formSubmitted:
		m.notice = "launching…"
		return m, launch(m.form.repo, m.form.featureName(), m.form.promptText())
	}
	return m, cmd
}

func (m model) selected() *state.Dispatch {
	if m.cursor >= 0 && m.cursor < len(m.dispatches) {
		return m.dispatches[m.cursor]
	}
	return nil
}

// findSelected keeps the cursor on the same dispatch across re-sorts.
func (m model) findSelected() int {
	for i, d := range m.dispatches {
		if d.ID == m.selectedID {
			return i
		}
	}
	if m.cursor >= len(m.dispatches) {
		return max(0, len(m.dispatches)-1)
	}
	return m.cursor
}
