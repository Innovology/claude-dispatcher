package cockpit

// adopt.go is the cockpit's side of adoption (internal/dispatch/adopt.go):
// taking a session this cockpit did not start and making it a dispatcher.
//
// It is the migration path for a human who already works this way. Their
// long-running orchestrator sessions hold the context that makes them fast,
// and moving onto this tool used to mean leaving that behind — the sessions
// tab could say where a session was and never what it wanted. Adopting one
// changes nothing about the session and everything about what the cockpit can
// see: the hooks already fire, and from the moment there is a record to match
// them to, the row waits, quotes and answers like any other.
//
// `space` marks sessions in the tab, `A` adopts. One session asks for a name,
// pre-filled with the branch it is on; a marked set is adopted on those
// defaults, because naming six sessions one at a time is not a migration.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/supervisor"
)

// adoptOpen starts an adoption: the marked sessions, or the one under the
// cursor. A single one gets the naming input; several are taken on their
// default names.
func (m model) adoptOpen(items []ownSession) (model, tea.Cmd) {
	picked := m.adoptPicked(items)
	switch len(picked) {
	case 0:
		m.notice = "no session to adopt"
		return m, nil
	case 1:
		// The session is copied when the input opens: the list behind it is
		// rebuilt on every load, and a name typed over five seconds must not
		// land on whatever row has taken that index since.
		t := picked[0]
		m.adoptOpenInput, m.adoptAt = true, &t
		// Pre-filled with the name the work already has, so the common case is
		// one keypress and the human is still the one who named it.
		m.adoptText = dispatchpkg.AdoptName(picked[0].path)
		return m, nil
	default:
		// The marks have been acted on. Kept, they would outlive the sessions
		// they named and pick the next A's targets instead of the cursor.
		m.sessionMarked = map[string]bool{}
		return m, adoptCmd(picked, "")
	}
}

// adoptPicked is what `A` acts on: every marked session in this product, or
// the row under the cursor when nothing is marked.
func (m model) adoptPicked(items []ownSession) []ownSession {
	var out []ownSession
	for _, s := range items {
		if m.sessionMarked[s.socket+"/"+s.name] {
			out = append(out, s)
		}
	}
	if len(out) > 0 {
		return out
	}
	if len(items) == 0 {
		return nil
	}
	return []ownSession{items[clampCursor(m.sessionCursor, len(items))]}
}

// adoptKey drives the naming input. Enter adopts, esc abandons, and an empty
// name is the default rather than a refusal.
func (m model) adoptKey(k string) (model, tea.Cmd) {
	next, submit, cancel := applyEdit(m.adoptText, k, m.key)
	if cancel {
		m.adoptOpenInput, m.adoptText, m.adoptAt = false, "", nil
		return m, nil
	}
	if submit {
		t := m.adoptAt
		name := strings.Join(strings.Fields(m.adoptText), " ")
		m.adoptOpenInput, m.adoptText, m.adoptAt = false, "", nil
		if t == nil {
			return m, nil
		}
		m.sessionMarked = map[string]bool{}
		return m, adoptCmd([]ownSession{*t}, name)
	}
	m.adoptText = next
	return m, nil
}

// adoptCmd adopts each session and reports what happened, naming every
// refusal: a migration that silently skipped two of six sessions would be
// worse than one that adopted none.
func adoptCmd(sessions []ownSession, name string) tea.Cmd {
	return func() tea.Msg {
		byName := map[string]repos.Repo{}
		for _, r := range lastDiscovered {
			byName[r.Name] = r
		}
		var took []string
		var refused []string
		for _, s := range sessions {
			d, err := dispatchpkg.Adopt(dispatchpkg.Adoption{
				Session: supervisor.Session{Name: s.name, Socket: s.socket},
				Dir:     s.path,
				Repo:    byName[s.repo],
				Feature: name,
			}, time.Now())
			if err != nil {
				refused = append(refused, s.name+": "+firstLine(err.Error()))
				continue
			}
			took = append(took, d.Feature)
		}
		switch {
		case len(took) == 0:
			return actionMsg{notice: "adopted nothing · " + strings.Join(refused, " · ")}
		case len(refused) > 0:
			return actionMsg{notice: "adopted " + strings.Join(took, ", ") +
				" · refused " + strings.Join(refused, " · ")}
		case len(took) == 1:
			return actionMsg{notice: "adopted \"" + took[0] + "\" · it reports from its next hook"}
		default:
			return actionMsg{notice: "adopted " + itoa(len(took)) + " sessions · " + strings.Join(took, ", ")}
		}
	}
}

// viewAdopt is the naming input, an overlay the way park's is. It shipped as a
// bar nothing drew: `A` opened an input that took every key that followed and
// showed none of them, so the press read as doing nothing — and the next
// letters typed went into a name nobody could see.
func (m model) viewAdopt(w, h int) string {
	t := m.adoptAt
	if t == nil {
		return ""
	}
	cw := w - 2*pad
	if cw < 10 {
		cw = w
	}
	lead := t.name
	if t.repo != "" {
		lead += " in " + t.repo
	}
	var out []string
	out = append(out, fg(cDim, "adopt this session"))
	out = append(out, line(lead, cw, cWhite, ""))
	out = append(out, fg(cFaint, truncate(t.path, cw)))
	explain := "it joins the fleet as a dispatcher and reports from its next hook · " +
		"nothing else changes: no branch, no worktree, and the session keeps running either way"
	for _, ln := range productWrap(explain, cw) {
		out = append(out, fg(cFaint, ln))
	}
	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	out = append(out, fg(cAmber, "name ")+fg(cWhite, m.adoptText+"▏"))
	out = append(out, fg(cFaint, "enter adopts it · esc cancels"))
	return clampLines(gutter(strings.Join(out, "\n"), pad), h)
}
