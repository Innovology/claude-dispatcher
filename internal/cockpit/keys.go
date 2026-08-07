package cockpit

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// applyEdit is the tiny inline editor shared by the filter, reply, palette and
// resume inputs. It returns the next string plus submit/cancel intents.
func applyEdit(cur, k string) (next string, submit, cancel bool) {
	switch k {
	case "esc":
		return cur, false, true
	case "enter":
		return cur, true, false
	case "backspace":
		r := []rune(cur)
		if len(r) > 0 {
			r = r[:len(r)-1]
		}
		return string(r), false, false
	case "space", " ":
		return cur + " ", false, false
	default:
		if len([]rune(k)) == 1 {
			return cur + k, false, false
		}
		return cur, false, false
	}
}

func (m model) filteredCommands() []command {
	q := strings.TrimSpace(strings.ToLower(m.paletteText))
	if q == "" {
		return commands
	}
	var out []command
	for _, c := range commands {
		if strings.Contains(c.name, q) || strings.Contains(strings.ToLower(c.hint), q) {
			out = append(out, c)
		}
	}
	return out
}

func (m model) runCommand() (model, tea.Cmd) {
	cmds := m.filteredCommands()
	if len(cmds) == 0 {
		m.paletteOpen = false
		m.paletteText = ""
		return m, nil
	}
	idx := m.paletteCursor
	if idx >= len(cmds) {
		idx = len(cmds) - 1
	}
	c := cmds[idx]
	if c.name == "settings" || c.name == "roots" {
		m.paletteOpen, m.paletteText = false, ""
		m.settings = newSettings(m.cfg)
		return m, nil
	}
	direct := map[string]string{
		"backlog": "backlog", "usage": "usage", "dispatch": "queue",
		"decisions": "decisions", "plugins": "decisions", "velocity": "velocity",
	}
	lens := m.lens
	if strings.HasPrefix(c.name, "product") {
		lens = "product"
	} else if d, ok := direct[c.name]; ok {
		lens = d
	}
	m.paletteOpen = false
	m.paletteText = ""
	m.lens = lens
	m.notice = ":" + c.name
	return m, nil
}

// handleKey is the global router. It resolves overlays and mode-switch keys,
// then delegates to the active lens's updater.
func (m model) handleKey(k string) (tea.Model, tea.Cmd) {
	if m.settings != nil {
		mm, cmd := m.updateSettings(k)
		return mm, cmd
	}
	if m.confirm != nil {
		switch k {
		case "y", "enter":
			mm, cmd := m.doConfirm()
			return mm, cmd
		case "n", "esc":
			m.confirm = nil
			m.notice = "cancelled"
		}
		return m, nil
	}
	if m.helpOpen {
		if k == "esc" || k == "?" || k == "q" {
			m.helpOpen = false
		}
		return m, nil
	}
	if m.diffOpen {
		if k == "esc" || k == "D" || k == "q" {
			m.diffOpen = false
		}
		return m, nil
	}
	if m.filterOpen {
		next, submit, cancel := applyEdit(m.filter, k)
		if cancel {
			m.filterOpen, m.filter, m.cursor = false, "", 0
			return m, nil
		}
		if submit {
			m.filterOpen = false
			return m, nil
		}
		if next != m.filter {
			m.filter, m.cursor = next, 0
		}
		return m, nil
	}
	if k == "?" {
		m.helpOpen = true
		return m, nil
	}
	if k == "u" && m.undo != "" {
		lbl := m.undo
		m.undo = ""
		m.notice = "undone · " + lbl
		return m, nil
	}
	if m.resumeOpen {
		mm, cmd := m.updateProduct(k)
		return mm, cmd
	}
	if m.paletteOpen {
		switch k {
		case "esc":
			m.paletteOpen, m.paletteText = false, ""
		case "down":
			if m.paletteCursor < len(m.filteredCommands())-1 {
				m.paletteCursor++
			}
		case "up":
			if m.paletteCursor > 0 {
				m.paletteCursor--
			}
		case "enter":
			mm, cmd := m.runCommand()
			return mm, cmd
		default:
			next, _, _ := applyEdit(m.paletteText, k)
			if next != m.paletteText {
				m.paletteText, m.paletteCursor = next, 0
			}
		}
		return m, nil
	}
	if m.replyFocused {
		next, submit, cancel := applyEdit(m.replyText, k)
		if cancel {
			m.replyFocused = false
			return m, nil
		}
		if submit {
			feat := m.floorSelectedFeature()
			text := m.replyText
			m.replyText, m.replyFocused = "", false
			return m, replyCmd(feat, text)
		}
		m.replyText = next
		return m, nil
	}
	if k == ":" {
		m.paletteOpen, m.paletteText, m.paletteCursor = true, "", 0
		return m, nil
	}
	if k == "," {
		m.settings = newSettings(m.cfg)
		return m, nil
	}
	if k == "tab" {
		if m.narrowPane == "list" {
			m.narrowPane = "detail"
		} else {
			m.narrowPane = "list"
		}
		return m, nil
	}
	if k == "q" || k == "ctrl+c" {
		return m, tea.Quit
	}
	if len(k) == 1 && k[0] >= '1' && k[0] <= '8' {
		m.lens = lensOrder[k[0]-'1']
		m.notice = ""
		return m, nil
	}

	var (
		mm  model
		cmd tea.Cmd
	)
	switch m.lens {
	case "decisions":
		mm, cmd = m.updateDecisions(k)
	case "backlog":
		mm, cmd = m.updateBacklog(k)
	case "products":
		mm, cmd = m.updateProducts(k)
	case "product":
		mm, cmd = m.updateProduct(k)
	case "floor":
		mm, cmd = m.updateFloor(k)
	default:
		mm = m
	}
	return mm, cmd
}
