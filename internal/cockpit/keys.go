package cockpit

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// typedText returns the literal text a key message types, and whether it types
// anything at all.
//
// Bubbletea coalesces everything readable in one poll into a single message —
// "a single KeyRunes or KeySpace event" (bubbletea/key.go) — so typing or
// pasting "count" arrives as ONE message carrying five runes, not five
// messages. Anything that rebuilds text from a key must take the whole run.
// alt+x is a chord, not text, and a pasted run keeps its runes even though
// Key.String() brackets it as "[count]".
func typedText(raw tea.KeyMsg) (string, bool) {
	if raw.Alt {
		return "", false
	}
	switch raw.Type {
	case tea.KeyRunes:
		return string(raw.Runes), len(raw.Runes) > 0
	case tea.KeySpace:
		return " ", true
	}
	return "", false
}

// typedTextFor resolves the text for a key, preferring the untouched message.
// raw is trusted only when it is the message k was derived from; the key-name
// fallback keeps the cockpit drivable by name (as the tests do), where a real
// terminal always supplies raw.
func typedTextFor(raw tea.KeyMsg, k string) (string, bool) {
	if s, ok := typedText(raw); ok && raw.String() == k {
		return s, true
	}
	switch {
	case k == "space":
		return " ", true
	case len([]rune(k)) == 1:
		return k, true
	}
	return "", false
}

// inputMsg is the message to feed a bubbles textinput: the untouched key
// message when it is the one being handled, else a reconstruction from the key
// name for name-driven callers. Passing the real message through also restores
// every editing key keyToMsg never covered — home, end, delete, ctrl+w, ctrl+u
// and paste all reach the input now.
func (m model) inputMsg(k string) tea.Msg {
	if m.key.String() == k {
		return m.key
	}
	return keyToMsg(k)
}

// applyEdit is the tiny inline editor shared by the filter, reply, palette and
// resume inputs. It returns the next string plus submit/cancel intents. raw is
// the untouched key message — see typedText for why k alone is not enough.
func applyEdit(cur, k string, raw tea.KeyMsg) (next string, submit, cancel bool) {
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
	}
	if s, ok := typedTextFor(raw, k); ok {
		return cur + s, false, false
	}
	return cur, false, false
}

// filteredCommands narrows the palette to what matches the typed query, name
// matches first. Ranking matters: hints mention other lenses by name, so
// without it typing "product" selected "usage" — whose hint happens to end
// "…by window, model and product".
func (m model) filteredCommands() []command {
	q := strings.TrimSpace(strings.ToLower(m.paletteText))
	if q == "" {
		return commands
	}
	var byName, byHint []command
	for _, c := range commands {
		switch {
		case strings.Contains(c.name, q):
			byName = append(byName, c)
		case strings.Contains(strings.ToLower(c.hint), q):
			byHint = append(byHint, c)
		}
	}
	return append(byName, byHint...)
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
	if c.name == "dispatch" || c.name == "new dispatch" {
		m.paletteOpen, m.paletteText = false, ""
		m.dispatchForm = newDispatchForm(m.cfg)
		return m, m.dispatchForm.filter.Focus()
	}
	if c.name == "upgrade" {
		m.paletteOpen, m.paletteText = false, ""
		return m.startUpgrade()
	}
	direct := map[string]string{
		"backlog": "backlog", "usage": "usage",
		"decisions": "decisions", "plugins": "decisions", "velocity": "velocity",
	}
	lens := m.lens
	if strings.HasPrefix(c.name, "product") {
		// The product panel is not a lens the palette can just name: opening it
		// has to seed the same state `enter` on the products list does, or the
		// panel comes up on whichever tab and shipped row the last visit left.
		lens = "product"
		m.rightTab, m.shipCursor = "overview", 0
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
	// Nothing may trap the process: the triage prompt swallows q, so ctrl+c has
	// to be resolved before any lens or overlay gets a say.
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if m.settings != nil {
		mm, cmd := m.updateSettings(k)
		return mm, cmd
	}
	if m.dispatchForm != nil {
		mm, cmd := m.updateDispatchForm(k)
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
	if m.resumeOpen {
		mm, cmd := m.updateProduct(k)
		return mm, cmd
	}
	// The park-reason input is text entry and owns the keyboard like the other
	// overlays — resolved before the triage lens, which would otherwise swallow
	// every letter as a fleet key.
	if m.parkOpen {
		mm, cmd := m.updatePark(k)
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
			next, _, _ := applyEdit(m.paletteText, k, m.key)
			if next != m.paletteText {
				m.paletteText, m.paletteCursor = next, 0
			}
		}
		return m, nil
	}
	// The triage lens sits below every overlay — with the palette open, `:` and
	// the letters are the palette's — and above '?' and 'u', which are text
	// while the dispatch prompt has the keyboard.
	if m.lens == "floor" {
		// Once before, so the cursor is keyed to the row that is on screen as the
		// key is pressed — the first key of a session would otherwise act on an
		// unseeded id. Once after, because an act or a skip can move the table
		// out from under it.
		m = m.fleetSync()
		mm, cmd, handled := m.updateFloorQueue(k)
		mm = mm.fleetSync()
		if handled {
			return mm, cmd
		}
		m = mm
	}
	// Naming a product is text entry, so it belongs with the other modes that
	// own the keyboard — settings, the dispatch form, the palette. Without this
	// guard the global keys resolved first: `q` killed the cockpit mid-name and
	// a digit switched lens, both discarding what had been typed, and the footer
	// promised only "esc cancels".
	if m.clNaming {
		mm, cmd := m.updateProducts(k)
		return mm, cmd
	}

	if k == "?" {
		m.helpOpen = true
		return m, nil
	}
	// ctrl+z, not `u`: `U` upgrades the machine in place, and undo — the key you
	// hit fastest, without looking — must not be one slipped shift away from it.
	// The chord also frees `u` for the assignment editor's unassign, which the
	// pending-undo check here used to steal whenever something was undoable.
	if k == "ctrl+z" && (m.cqUndo != nil || m.undo != "") {
		if cu := m.cqUndo; cu != nil {
			// Puts the row back at the front. The act's command already ran, so
			// this un-hides an ask; it does not un-kill a session.
			m.cqOrder = append([]string{cu.id}, m.cqOrder...)
			delete(m.cqSuppressed, cu.id)
			m.cqUndo, m.undo = nil, ""
			m.cqCleared = maxi(0, m.cqCleared-1)
			m.notice = "undone · " + cu.label
			return m, nil
		}
		lbl := m.undo
		m.undo = ""
		m.notice = "undone · " + lbl
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
	if k == "+" {
		m.dispatchForm = newDispatchForm(m.cfg)
		return m, m.dispatchForm.filter.Focus()
	}
	// `U` is the upgrade key and nothing else's: bound globally rather than
	// per-lens because the version it acts on is in the footer, which every lens
	// carries — so any lens that wanted a capital U of its own would never see
	// it (the assignment editor's "start over" was exactly that, and is a chord
	// now).
	if k == "U" {
		return m.startUpgrade()
	}
	if k == "q" {
		return m, tea.Quit
	}
	if isLensDigit(k) {
		m.lens = lensOrder[k[0]-'1']
		m.notice = ""
		// Leaving the lens leaves its modes: coming back to a half-filled
		// dispatch form, or to the products lens still holding an assignment
		// editor the human walked away from, is a state they did not ask for.
		// dxReset closes the form and clears all four fields.
		m = m.dxReset()
		m.clOpen, m.clNaming, m.clNewName = false, false, ""
		m.clMarked = map[string]bool{}
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
	default:
		mm = m
	}
	return mm, cmd
}
