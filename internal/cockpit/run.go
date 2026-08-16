package cockpit

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// binaryName is this command as it is installed on PATH — what relaunch execs
// after an in-place upgrade.
const binaryName = "claude-dispatcher"

// applyConfigEnv exports integration settings from config into the environment
// so the env-gated Linear/Azure clients pick them up. A real env var already
// set wins, so a secret can stay out of config.toml if preferred.
func applyConfigEnv(cfg *config.Config) {
	setIfEmpty := func(k, v string) {
		if v != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
	setIfEmpty("LINEAR_API_KEY", cfg.LinearAPIKey)
	setIfEmpty("AZURE_DEVOPS_ORG", cfg.AzureOrg)
	setIfEmpty("AZURE_DEVOPS_PROJECT", cfg.AzureProject)
}

// Run opens the v2 cockpit. With a config it renders live data — the dispatch
// records, git/gh signals, backlog and metrics — refreshed from an fsnotify
// watch on the state dir plus a periodic poll. Without one it falls back to the
// design's demo seed data so the cockpit still opens.
func Run() error {
	m := newModel()

	if cfg, err := config.Load(); err == nil {
		m.cfg = cfg
		applyConfigEnv(cfg)
		_ = state.EnsureDirs()
		ch := make(chan struct{}, 1)
		if watcher, err := fsnotify.NewWatcher(); err == nil {
			if watcher.Add(state.DispatchesDir()) == nil {
				m.stateCh = ch
				go forwardEvents(watcher, ch)
				defer func() { _ = watcher.Close() }()
			} else {
				_ = watcher.Close()
			}
		}
		m.loading = true
	} else {
		m.notice = "no config — showing demo data · run `claude-dispatcher init`"
	}

	// Focus reporting is what tells the cockpit the human has come back from a
	// session it switched them to rather than attached them to — the handover
	// there exits on the way out, so nothing else marks the return. Terminals
	// that do not report focus simply never send the message; see the
	// tea.FocusMsg case in model.go, which ignores it unless we handed someone
	// over in the first place.
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithReportFocus()).Run()
	if err != nil {
		return err
	}
	// The one exit that is not an exit: `U` installed a new build, and this
	// process quit so the terminal would be handed back before we exec it.
	if fm, ok := final.(model); ok && fm.relaunch {
		return relaunch()
	}
	return nil
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
