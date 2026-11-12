package cockpit

// dxeditor_test.go covers ctrl+g: WHAT and DONE WHEN handed to the human's
// editor and read back. The handover itself (tea.ExecProcess) is replaced, and
// the test plays the editor by writing the file it was given.

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeEditor replaces the handover and returns what it was asked to run and the
// callback that reports the editor's exit.
type fakeEditor struct {
	cmd  *exec.Cmd
	done tea.ExecCallback
}

func stubEditor(t *testing.T) *fakeEditor {
	t.Helper()
	t.Setenv("VISUAL", "true") // any editor on PATH; it is never run
	fe := &fakeEditor{}
	prev := dxExec
	dxExec = func(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
		fe.cmd, fe.done = c, fn
		return func() tea.Msg { return nil }
	}
	t.Cleanup(func() { dxExec = prev })
	return fe
}

// file is the path the editor was opened on.
func (fe *fakeEditor) file(t *testing.T) string {
	t.Helper()
	if fe.cmd == nil {
		t.Fatal("no editor was opened")
	}
	return fe.cmd.Args[len(fe.cmd.Args)-1]
}

// exit plays the editor exiting, having left content in the file, and feeds
// the result back through Update.
func (fe *fakeEditor) exit(t *testing.T, m model, content string, err error) model {
	t.Helper()
	if err := os.WriteFile(fe.file(t), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(fe.done(err))
	return mm.(model)
}

// The brief goes to the editor as it stands, and comes back whole: its lines
// kept, the newline the editor ends the file with dropped, the caret at the
// end, and the file gone.
func TestDXCtrlGEditsWhat(t *testing.T) {
	fe := stubEditor(t)
	m := dxFormModel(t)
	m.dxField = dxWhatF
	m = feed(t, m, text("retry cards"), key(tea.KeyCtrlG))

	path := fe.file(t)
	if got, _ := os.ReadFile(path); string(got) != "retry cards" {
		t.Errorf("the editor opened on %q, want WHAT as it stood", got)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the brief's file should be private: %v %v", fi.Mode(), err)
	}

	m = fe.exit(t, m, "retry declined cards\r\n\n- once\n- then stop\n", nil)
	if m.dxWhat != "retry declined cards\n\n- once\n- then stop" {
		t.Errorf("WHAT = %q", m.dxWhat)
	}
	if m.dxField != dxWhatF || !m.cqDispatch {
		t.Error("the form should come back open on WHAT")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the file should be removed once its text is in the form")
	}
	if !strings.Contains(m.notice, "from your editor") {
		t.Errorf("notice = %q", m.notice)
	}
	// Typing after it carries on at the end.
	if m = feed(t, m, text("!")); !strings.HasSuffix(m.dxWhat, "then stop!") {
		t.Errorf("typing after the editor: %q", m.dxWhat)
	}
}

// DONE WHEN is one line, so what comes back is folded onto it.
func TestDXCtrlGEditsDoneWhenOnOneLine(t *testing.T) {
	fe := stubEditor(t)
	m := dxFormModel(t)
	m.dxField = dxGoalF
	m = feed(t, m, key(tea.KeyCtrlG))
	m = fe.exit(t, m, "ci is green\nand the deploy succeeded\n", nil)
	if m.dxGoal != "ci is green and the deploy succeeded" {
		t.Errorf("DONE WHEN = %q", m.dxGoal)
	}
}

// An editor that fails — vim's :cq is the deliberate "never mind" — leaves the
// field alone, and the file is kept and named: it may be the only copy.
func TestDXCtrlGEditorFailureKeepsTheFile(t *testing.T) {
	fe := stubEditor(t)
	m := dxFormModel(t)
	m.dxField = dxWhatF
	m = feed(t, m, text("retry cards"), key(tea.KeyCtrlG))
	path := fe.file(t)
	t.Cleanup(func() { _ = os.Remove(path) })

	m = fe.exit(t, m, "a long brief nobody saved anywhere else", errors.New("exit status 1"))
	if m.dxWhat != "retry cards" {
		t.Errorf("WHAT = %q, want it unchanged", m.dxWhat)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the file should be kept: %v", err)
	}
	if !strings.Contains(m.notice, path) {
		t.Errorf("the notice should name the file: %q", m.notice)
	}
}

// ctrl+g is for the fields that run long. On TITLE it opens nothing; with no
// editor to run it says how to get one.
func TestDXCtrlGOnlyWhereItHelps(t *testing.T) {
	fe := stubEditor(t)
	m := titleModel(t, "payment retries")
	m = feed(t, m, key(tea.KeyCtrlG))
	if fe.cmd != nil {
		t.Error("ctrl+g on TITLE should open nothing")
	}
	if strings.Contains(m.dxFooterHelp(), "ctrl+g") {
		t.Errorf("the footer should not offer ctrl+g on TITLE: %q", m.dxFooterHelp())
	}
	m = feed(t, m, key(tea.KeyTab))
	if !strings.Contains(m.dxFooterHelp(), "ctrl+g editor") {
		t.Errorf("the footer should offer ctrl+g on WHAT: %q", m.dxFooterHelp())
	}

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("PATH", t.TempDir())
	m = feed(t, m, key(tea.KeyCtrlG))
	if !strings.Contains(m.notice, "set $EDITOR") {
		t.Errorf("notice = %q", m.notice)
	}
}
