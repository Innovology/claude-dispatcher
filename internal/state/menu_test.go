package state

import (
	"testing"
	"time"
)

// menus lists the open dialogs as agent/tool pairs, for comparing against a
// want list without caring when each went up.
func menus(d *Dispatch) []Menu {
	var out []Menu
	for _, m := range d.Menus {
		out = append(out, Menu{Agent: m.Agent, Tool: m.Tool})
	}
	return out
}

func sameMenus(t *testing.T, d *Dispatch, want ...Menu) {
	t.Helper()
	got := menus(d)
	if len(got) != len(want) {
		t.Fatalf("open dialogs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("open dialogs = %+v, want %+v", got, want)
		}
	}
}

// A question is announced twice — by its PreToolUse and by its PermissionRequest
// — and it is one dialog. Counting it twice would leave a phantom open after
// the answer closed the real one, and the row blocked on nothing.
func TestTheSameDialogReportedTwiceIsOneDialog(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()
	if !d.OpenMenu("", QuestionTool, at) {
		t.Fatal("the first report of a dialog must record it")
	}
	if d.OpenMenu("", QuestionTool, at.Add(time.Second)) {
		t.Fatal("a second report of the same dialog must not count as a change")
	}
	sameMenus(t, d, Menu{Tool: QuestionTool})
	if !d.Menus[0].At.Equal(at) {
		t.Errorf("the dialog went up at %v, not when it was reported again", at)
	}

	// The same tool for another agent is another dialog.
	if !d.OpenMenu("worker-h", QuestionTool, at) {
		t.Fatal("a subagent's dialog for the same tool is its own")
	}
	sameMenus(t, d, Menu{Tool: QuestionTool}, Menu{Agent: "worker-h", Tool: QuestionTool})
}

// The defect this list exists for: a background agent's tools completing beside
// a question are not the human answering it. Measured, the first of 29 such
// tools put the row back to working for the hour the question stood.
func TestAnotherAgentsToolClosesNothing(t *testing.T) {
	d := &Dispatch{}
	d.OpenMenu("", QuestionTool, time.Now())
	if d.SettleMenus("worker-h", "Bash") {
		t.Fatal("another agent's tool reported closing the session's question")
	}
	if d.SettleMenus("worker-h", QuestionTool) {
		t.Fatal("another agent's tool of the same name closed the session's question")
	}
	sameMenus(t, d, Menu{Tool: QuestionTool})
}

// A dialog closes on its own tool completing — answering is the tool returning
// — and only that one: the same agent's other dialog stays open.
func TestADialogClosesOnItsOwnTool(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()
	d.OpenMenu("", QuestionTool, at)
	d.OpenMenu("", "Bash", at)
	if !d.SettleMenus("", QuestionTool) {
		t.Fatal("the question's own tool completing did not close it")
	}
	sameMenus(t, d, Menu{Tool: "Bash"})
	if !d.SettleMenus("", "Bash") {
		t.Fatal("the permission prompt's own tool completing did not close it")
	}
	if d.Menus != nil {
		t.Errorf("an emptied list is left as %#v, want nil so the record omits it", d.Menus)
	}
}

// Denying a prompt fires no hook at all; the only sign is the agent going on to
// a different tool. With no dialog of its own for that tool, everything the
// agent had open is behind it — and nobody else's is.
func TestAnAgentMovingOnClosesEveryDialogItHad(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()
	d.OpenMenu("worker-h", "Bash", at)
	d.OpenMenu("worker-h", "Write", at)
	d.OpenMenu("", QuestionTool, at)
	if !d.SettleMenus("worker-h", "Read") {
		t.Fatal("the agent moving past its prompts closed nothing")
	}
	sameMenus(t, d, Menu{Tool: QuestionTool})
}

// A dialog only a Notification reported names neither whose nor what, so any
// tool completing closes it — all an install without PermissionRequest knows —
// while a named dialog beside it keeps its own rules.
func TestADialogNobodyNamedClosesOnAnyTool(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()
	d.OpenMenu("", "", at)
	d.OpenMenu("", QuestionTool, at)
	if !d.SettleMenus("worker-h", "Bash") {
		t.Fatal("an unattributed dialog survived a tool completing")
	}
	sameMenus(t, d, Menu{Tool: QuestionTool})
}

// An agent ending takes its dialogs with it, and the session's turn ending takes
// the session's — plus the unattributed ones, since the session is the one that
// cannot say whose those were. Another agent's dialog outlives both.
func TestDroppingAnAgentsDialogsLeavesTheOthers(t *testing.T) {
	d := &Dispatch{}
	at := time.Now()
	d.OpenMenu("worker-h", "Bash", at)
	d.OpenMenu("worker-j", "Edit", at)
	d.OpenMenu("", QuestionTool, at)
	d.OpenMenu("", "", at)

	if !d.DropMenus("worker-h") {
		t.Fatal("an agent ending closed none of its dialogs")
	}
	sameMenus(t, d, Menu{Agent: "worker-j", Tool: "Edit"}, Menu{Tool: QuestionTool})

	if !d.DropMenus("") {
		t.Fatal("the turn ending closed none of the session's dialogs")
	}
	sameMenus(t, d, Menu{Agent: "worker-j", Tool: "Edit"})

	if d.DropMenus("worker-h") {
		t.Error("dropping an agent with nothing open reported a change")
	}
}

// The reason a blocked row gives depends on whether any open dialog is a
// question: "asked you a question" and "approve a permission" send the human in
// for different things.
func TestMenuAsksOnlyWhileAQuestionIsOpen(t *testing.T) {
	d := &Dispatch{}
	if d.MenuAsks() {
		t.Fatal("no dialog is not a question")
	}
	d.OpenMenu("worker-h", "Bash", time.Now())
	if d.MenuAsks() {
		t.Fatal("a permission prompt read as a question")
	}
	d.OpenMenu("", QuestionTool, time.Now())
	if !d.MenuAsks() {
		t.Fatal("an open question did not read as one")
	}
	d.SettleMenus("", QuestionTool)
	if d.MenuAsks() {
		t.Error("an answered question still reads as asked")
	}
}

// The list rides the record through every hook, which are separate processes:
// a dialog that did not survive the round trip would be closed by the next
// event to save the record, whatever the human had done.
func TestOpenDialogsSurviveTheRoundTrip(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	at := time.Now().Truncate(time.Second)
	d := &Dispatch{ID: "q", Status: StatusBlocked}
	d.OpenMenu("", QuestionTool, at)
	d.OpenMenu("worker-h", "Bash", at)
	if err := Save(d); err != nil {
		t.Fatal(err)
	}
	got := LoadAll()
	if len(got) != 1 {
		t.Fatalf("%d records", len(got))
	}
	sameMenus(t, got[0], Menu{Tool: QuestionTool}, Menu{Agent: "worker-h", Tool: "Bash"})
	if !got[0].Menus[0].At.Equal(at) {
		t.Errorf("the dialog's time came back as %v, want %v", got[0].Menus[0].At, at)
	}
}
