package cockpit

// dxeditor.go is ctrl+g on the dx form's WHAT and DONE WHEN: the field's text
// handed to the human's own editor, and read back when the editor exits —
// what Claude Code's own ctrl+g does with its prompt.
//
// Asked for because "sometimes the prompt is really long". WHAT wraps to six
// rows and then scrolls to keep its end in view, and its caret only ever
// stands at the end (dxcaret.go), so a brief longer than the box is written
// blind and can only be changed from the back. An editor has neither problem,
// and it is the one the human already writes in.
//
// The handover is the one `e` on settings makes with config.toml
// (editconfig.go): the same editor ($VISUAL, $EDITOR, vi), the terminal handed
// over with tea.ExecProcess, and the terminal's theme reports asked for again
// on the way back, because the editor had the screen.

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// dxEditedMsg is the editor having exited, on field's text in path.
type dxEditedMsg struct {
	field dxFieldID
	path  string
	err   error
}

// dxExec hands the terminal to cmd; a test replaces it to play the editor.
var dxExec = tea.ExecProcess

// dxEditable reports whether ctrl+g opens f in an editor: WHAT, the brief, and
// DONE WHEN, which can run to a paragraph of its own. The others are a few
// words or a branch name.
func dxEditable(f dxFieldID) bool {
	return f == dxWhatF || f == dxGoalF
}

// dxFieldName is how a notice names a field: its label.
func dxFieldName(f dxFieldID) string {
	if f == dxGoalF {
		return "DONE WHEN"
	}
	return "WHAT"
}

// dxOpenEditor writes f's text to a file of its own and hands the terminal to
// the editor on it. The file is the human's only copy of what they type there
// until the editor exits, so it is private (CreateTemp makes it 0600) and named
// for what it holds; it is removed once read back.
func (m model) dxOpenEditor(f dxFieldID) (model, tea.Cmd) {
	slug := "what"
	if f == dxGoalF {
		slug = "done-when"
	}
	file, err := os.CreateTemp("", "dispatch-"+slug+"-*.md")
	if err != nil {
		m.notice = "could not open " + dxFieldName(f) + " in an editor: " + err.Error()
		return m, nil
	}
	path := file.Name()
	_, werr := file.WriteString(m.dxText(f))
	if cerr := file.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		m.notice = "could not open " + dxFieldName(f) + " in an editor: " + werr.Error()
		return m, nil
	}
	cmd := editorCommand(path)
	if cmd == nil {
		_ = os.Remove(path)
		m.notice = "no editor to open " + dxFieldName(f) + " with — set $EDITOR"
		return m, nil
	}
	return m, dxExec(cmd, func(err error) tea.Msg { return dxEditedMsg{field: f, path: path, err: err} })
}

// dxFromEditor is what came back from the editor, as the field takes it. An
// editor ends a file with a newline, which is not part of the text. WHAT keeps
// the rest of its lines; DONE WHEN is one line, so its are folded.
func dxFromEditor(f dxFieldID, raw string) string {
	s := strings.TrimRight(blockText(raw), "\n")
	if f == dxWhatF {
		return s
	}
	return strings.TrimSpace(oneLine(s))
}

// onDxEdited reads the editor's file back into the field it was opened from,
// with the caret at the end.
//
// The file is removed only once its text is in the form. An editor that exits
// with an error — vim's :cq is the deliberate way to say "never mind" — leaves
// the field as it was, and so does a form that has closed in the meantime; in
// both cases the file is kept and named, because it may hold a brief the human
// spent ten minutes on and has no other copy of.
func (m model) onDxEdited(msg dxEditedMsg) (model, tea.Cmd) {
	follow := m.themeFollowTerm() // the editor had the screen; ask again
	name := dxFieldName(msg.field)
	if msg.err != nil {
		m.notice = "editor " + exitWords(msg.err) + " · " + name + " unchanged · kept " + msg.path
		return m, follow
	}
	raw, err := os.ReadFile(msg.path)
	if err != nil {
		m.notice = "could not read " + name + " back from the editor: " + err.Error()
		return m, follow
	}
	if !m.cqFormOn() {
		m.notice = "the dispatch form had closed — " + name + " is in " + msg.path
		return m, follow
	}
	_ = os.Remove(msg.path)
	text := dxFromEditor(msg.field, string(raw))
	before := m.dxText(msg.field)
	if msg.field == dxGoalF {
		m.dxGoal = text
	} else {
		m.dxWhat = text
	}
	m.dxField, m.dxCaret = msg.field, dxCaret{field: msg.field}
	if text == before {
		m.notice = name + " unchanged"
	} else {
		m.notice = name + " from your editor · " + itoa(len([]rune(text))) + " characters"
	}
	return m, follow
}

// exitWords is an editor's failure in the words that matter here: the exit
// status where there is one, else the error itself.
func exitWords(err error) string {
	if ee, ok := err.(*exec.ExitError); ok {
		return "exited " + itoa(ee.ExitCode())
	}
	return err.Error()
}
