package cockpit

// reply.go answers a waiting dispatcher from the triage table: r on its row,
// one line, enter — typed into the session as if at its prompt.
//
// It exists because attaching was the only way to answer, and most answers are
// one line. Of 949 follow-up prompts in the transcripts, the commonest were
// "continue", "merge it", "ship it", "yes" and "try again": every one of them
// a jump into the session, a word, and a jump back out, for a question the row
// now quotes (cqAsk). The row shows the ask; this answers it where it is read.
//
// It is deliberately not offered on a blocked row. A permission prompt is a
// menu, not a text box — typed characters pick its options — so a reply there
// would approve or refuse something by accident. ⏎ attach is still the answer
// to a permission prompt.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/fleetcmd"
	"claude-dispatcher/internal/supervisor"
)

// replyTarget is what the reply input is about, captured when it opens for the
// same reason parkTarget is: the table rebuilds underneath, and a target read
// back from the cursor could answer a different dispatcher than the one whose
// question the human was reading.
type replyTarget struct{ id, feature, ask string }

// replyable reports whether r is a row whose session is waiting at a text
// prompt: a queue row that is not a permission prompt.
func replyable(r fleetRow) bool {
	return r.kind == "queue" && r.ask != "permission"
}

// updateReply drives the reply input while it owns the keyboard. Enter with
// nothing typed sends nothing — an empty line would be a turn spent on no
// instruction — and says so.
func (m model) updateReply(k string) (model, tea.Cmd) {
	next, submit, cancel := applyEdit(m.replyText, k, m.key)
	if cancel {
		m.replyOpen, m.replyText, m.replyAt = false, "", nil
		return m, nil
	}
	if submit {
		t := m.replyAt
		text := strings.TrimSpace(m.replyText)
		if text == "" {
			m.notice = "type the answer — enter sends it, esc cancels"
			return m, nil
		}
		m.replyOpen, m.replyText, m.replyAt = false, "", nil
		if t == nil {
			return m, nil
		}
		m.notice = "replying to \"" + t.feature + "\"…"
		return m, replyCmd(t.id, text)
	}
	m.replyText = next
	return m, nil
}

// viewReply renders the reply input: whose question, the question itself in
// the session's words, and the line being typed.
func (m model) viewReply(w, h int) string {
	t := m.replyAt
	if t == nil {
		return ""
	}
	cw := w - 2*pad
	if cw < 10 {
		cw = w
	}
	var out []string
	out = append(out, fg(cDim, "reply to this dispatcher"))
	out = append(out, line(t.feature, cw, cWhite, ""))
	if t.ask != "" {
		for _, ln := range productWrap(t.ask, cw) {
			out = append(out, fg(cFaint, ln))
		}
	}
	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	out = append(out, fg(cAmber, "› ")+fg(cWhite, m.replyText+"▏"))
	out = append(out, fg(cFaint, "enter types it into the session · esc cancels"))
	return clampLines(gutter(strings.Join(out, "\n"), pad), h)
}

// replyCmd types text into the dispatcher's live session, as if at its prompt.
//
// Addressed by id, not feature: a feature name can belong to several records.
// It refuses where claude has provably exited — the text would land in the
// shell the session drops to — and reports a send that failed, which the
// version before this did not: it said "replied" over a tmux that had typed
// nothing (see supervisor.SendKeys).
func replyCmd(id, text string) tea.Cmd {
	return func() tea.Msg {
		rec := recordByID(id)
		if rec == nil || !supervisor.HasSession(rec.TmuxSession) {
			return actionMsg{notice: "no live session to reply to"}
		}
		if idle, known := supervisor.SessionIdle(rec.TmuxSession); idle && known {
			return actionMsg{notice: "claude has exited in \"" + rec.Feature + "\" — ⏎ to resume it"}
		}
		if err := supervisor.SendKeys(rec.TmuxSession, text); err != nil {
			return actionMsg{notice: "reply to \"" + rec.Feature + "\" failed: " + firstLine(err.Error())}
		}
		// On the record at once, so a steward polling for open waits cannot
		// answer this one too in the moment before the session's hook lands.
		fleetcmd.MarkAnswered(rec.ID, text)
		return actionMsg{notice: "replied to \"" + rec.Feature + "\" · " + text}
	}
}
