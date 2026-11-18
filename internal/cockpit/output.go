package cockpit

// output.go is `l` on a row whose session would not start: everything the
// failed start printed, in the human's editor, at the line its error opens.
//
// The row's lead is the cause in one line (internal/cause), and that is
// usually enough to say what went wrong. It is not always enough to fix it: a
// nix shell that would not build is fixed from the builder's log, the URL it
// tried and the `nix log` command nix names, and none of that fits a line.
// Before this the only way to that text was the event log, where it is one
// JSON string; on a terminal wide enough, a failed resume also spilled it over
// the footer, which is how it was seen at all.

import (
	"errors"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/cause"
)

// openStartOutput hands the terminal to the editor on r's start output.
func (m model) openStartOutput(r fleetRow) (model, tea.Cmd) {
	rec := recordByID(r.id)
	if rec == nil || rec.StartOutput == "" {
		m.notice = "nothing was kept of what \"" + r.feature + "\" printed"
		return m, nil
	}
	cmd, err := startOutputCommand(rec.StartOutput)
	switch {
	case errors.Is(err, errNoEditor):
		m.notice = "no editor to open " + rec.StartOutput + " with — set $EDITOR"
		return m, nil
	case err != nil:
		m.notice = "what \"" + r.feature + "\" printed is gone — " + err.Error()
		return m, nil
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return actionMsg{notice: "editor: " + err.Error()}
		}
		return actionMsg{}
	})
}

var errNoEditor = errors.New("no editor")

// startOutputCommand is the editor on the output at path, at the line its
// error opens rather than at the top of what can be hundreds of lines of
// downloads. The file is read here only to find that line.
func startOutputCommand(path string) (*exec.Cmd, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cmd := decEditorCommand(path, cause.LineOf(string(b)))
	if cmd == nil {
		return nil, errNoEditor
	}
	return cmd, nil
}
