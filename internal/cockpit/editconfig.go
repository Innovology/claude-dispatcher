package cockpit

// editconfig.go hands config.toml to the human's own editor and reads it back.
//
// The settings screen edits six fields, and config.toml holds a great deal
// more — products, checkouts, sockets, session environments, Linear tokens,
// keys, gates — most of it written by the editors elsewhere in the cockpit and
// some of it by nothing at all. Asked for as "a way to open config.toml in the
// default editor from within the dispatcher": `e` on settings, or the palette's
// "config", hands the terminal over the way ⏎ hands it to tmux, and coming back
// re-reads the file and applies everything the startup applied from it.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/keymap"
	"claude-dispatcher/internal/supervisor"
)

// configEditedMsg is the editor having exited.
type configEditedMsg struct{ err error }

// editorCommand is the human's editor on path: $VISUAL, then $EDITOR — the two
// every terminal tool asks — and vi where neither is set, because vi is the one
// editor POSIX promises. A value with arguments ("code --wait") is split on
// whitespace, the way git reads core.editor. nil when nothing can be run.
func editorCommand(path string) *exec.Cmd {
	for _, v := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi"} {
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		if _, err := exec.LookPath(f[0]); err != nil {
			continue
		}
		return exec.Command(f[0], append(f[1:], path)...)
	}
	return nil
}

// editConfig hands the terminal to the editor on config.toml. A config that
// does not exist yet is written first, so the editor opens on the commented
// template rather than an empty file.
func (m model) editConfig() (model, tea.Cmd) {
	if _, _, err := config.WriteDefault(); err != nil {
		m.notice = "config.toml: " + err.Error()
		return m, nil
	}
	cmd := editorCommand(config.Path())
	if cmd == nil {
		m.notice = "no editor to open config.toml with — set $EDITOR"
		return m, nil
	}
	m.settings = nil
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return configEditedMsg{err: err} })
}

// onConfigEdited reads the file back and applies it: everything Run applied
// from it at startup, then a reload, because roots, products, checkouts and
// sockets are what a load is built from.
//
// A file that no longer parses keeps the config already running. The cockpit
// saving its own settings rewrites the whole file from the config it holds,
// so carrying on with a half-read one would quietly throw away whatever the
// human typed that it could not read.
func (m model) onConfigEdited(msg configEditedMsg) (model, tea.Cmd) {
	follow := m.themeFollowTerm() // the editor had the screen; ask again
	if msg.err != nil {
		m.notice = "editor: " + msg.err.Error()
		return m, follow
	}
	cfg, err := config.Load()
	if err != nil {
		m.notice = "config.toml did not load — still running on the config from before: " + err.Error()
		return m, follow
	}
	m.cfg = cfg
	applyConfigEnv(cfg)
	supervisor.KnowShell(cfg.Shell)
	notice := "config.toml reloaded"
	if km, err := keymap.New(cfg.Keys); err != nil {
		notice += fmt.Sprintf(" · keys: %v — keeping the bindings in use", err)
	} else {
		m.keys = km
	}
	var cmds []tea.Cmd
	if mode, _ := themeModeOf(cfg.Theme); mode != m.themeMode {
		m.themeMode = mode
		m.applyTheme()
		if mode == themeSystem {
			var c tea.Cmd
			m, c = m.followSystem()
			cmds = append(cmds, c)
		}
	}
	m.notice = notice
	mm, load := m.requestLoad(loadPlain)
	return mm, tea.Batch(append(cmds, follow, load)...)
}
