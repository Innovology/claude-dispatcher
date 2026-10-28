package hookcmd

import (
	"encoding/json"
	"strings"
	"testing"

	"claude-dispatcher/internal/ask"
	"claude-dispatcher/internal/state"
)

// The report: a dispatcher sat on a four-option menu with the human's name on
// it while the table said "0 want you · 1 running clean". Measured in the
// event log for that session, the only hooks arriving while the menu was up
// were its background agent's SubagentStops, every 32 seconds — liveness, read
// as progress. No Stop (the turn had not ended), no idle notification (the
// prompt was not idle), no permission prompt (this is not one).
func TestAQuestionIsAWait(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &state.Dispatch{ID: "a", Feature: "robustness", Status: state.StatusWorking,
		StatusReason: "processing your prompt"}

	if !apply(d, "PreToolUse:AskUserQuestion", hookInput{}) {
		t.Fatal("a question changed nothing on the record")
	}
	if d.Status != state.StatusBlocked {
		t.Errorf("status = %q, want blocked — a menu cannot be answered by typing a line", d.Status)
	}
	if d.StatusReason == "" {
		t.Error("the row says nothing about what it is waiting for")
	}

	// Answering is a tool completing, which is how a permission prompt clears
	// too: the tool returns once the human has picked.
	if !apply(d, "PostToolUse", hookInput{}) {
		t.Fatal("answering the question left the record blocked")
	}
	if d.Status != state.StatusWorking {
		t.Errorf("status after the answer = %q, want working", d.Status)
	}
}

// A subagent finishing is not an answer. This is the exact false negative
// from the report: those events kept arriving under the menu, and anything
// that treated them as progress would clear a wait nobody had answered.
func TestSubagentTrafficDoesNotClearAQuestion(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &state.Dispatch{ID: "a", Feature: "robustness", Status: state.StatusWorking}
	apply(d, "PreToolUse:AskUserQuestion", hookInput{})

	apply(d, "SubagentStart", hookInput{AgentID: "x", AgentType: "Explore"})
	apply(d, "SubagentStop", hookInput{AgentID: "x"})

	if d.Status != state.StatusBlocked {
		t.Errorf("status = %q, want blocked — the question is still on the screen", d.Status)
	}
}

// "Done means live" is terminal for everything but proof the session is still
// going, and a question is that proof. track flips a record to done the moment
// its PR merges, and a dispatcher told to merge its own PR and keep working
// routinely merges mid-run — so a question can and does arrive on a done
// record. Without this the record would freeze at done, and since the triage
// table admits no done rows the menu would be invisible: the reported defect
// again, in the one state where marking it blocked is not enough.
func TestAQuestionReopensADoneRecord(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &state.Dispatch{ID: "a", Feature: "robustness", Status: state.StatusDone}

	if !apply(d, "PreToolUse:AskUserQuestion", hookInput{}) {
		t.Fatal("a question on a done record changed nothing")
	}
	if d.Status != state.StatusBlocked {
		t.Errorf("status = %q, want blocked — a shipped dispatcher can still stop to ask", d.Status)
	}

	// The asymmetry this closes: the permission prompt was always let through,
	// so a done record that asked permission came back and one that asked a
	// question did not.
	if !reopensDone("PreToolUse:AskUserQuestion") || !reopensDone("Notification:permission_prompt") {
		t.Error("both menus must outrank done; neither is what a shipped feature looks like")
	}
	// And the events that are still terminal.
	for _, e := range []string{"Stop", "SessionEnd", "Notification:idle_prompt"} {
		if reopensDone(e) {
			t.Errorf("%s must not downgrade done", e)
		}
	}
}

// Reported from a live dispatcher an hour after the question-tool defect, and
// it is the same defect from the other side. A session ended its turn with a
// prose question — "One thing needs your confirmation … OK to keep that?" —
// and sat at an empty prompt while the table read "2 in flight · 0 want you ·
// 2 running clean" and the row's signal said "fan-out · 1 live · merged,
// deploying".
//
// Measured on the record (d51bc11a5c5d): status "working", reason "waiting on
// 2 background tasks", waiting_on_tasks true. Its Stop carried background
// tasks — an adversarial review agent was still running — and that branch was
// taken on the task count alone, so the ask in the message was never
// consulted.
//
// It is the tool's own doing. The auto contract this project composes at
// launch says in one breath to wait on something slow with a background task
// rather than ending the turn, and to end the message on the one question that
// is the human's. A session obeying both lands exactly here, which is why the
// rule that background work is no human wait is right on its own and wrong the
// moment the message asks something.
func TestAnAskOutranksBackgroundWork(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	const said = "I pushed the fix and the PR is merged.\n\n" +
		"One thing needs your confirmation: I left the legacy shim in place. OK to keep that?"
	tasks := []json.RawMessage{json.RawMessage(`{"task_id":"t1"}`), json.RawMessage(`{"task_id":"t2"}`)}
	d := &state.Dispatch{ID: "a", Feature: "accounts", Status: state.StatusWorking}

	if !apply(d, "Stop", hookInput{LastAssistantMessage: said, BackgroundTasks: tasks}) {
		t.Fatal("a turn ending on a question changed nothing on the record")
	}
	if d.Status != state.StatusNeedsInput {
		t.Errorf("status = %q, want needs-input — the message asked the human something", d.Status)
	}
	// Both facts are true. The background work keeps its flag because the idle
	// prompt that trails a stop still defers to it; it just no longer decides.
	if !d.WaitingOnTasks {
		t.Error("WaitingOnTasks cleared — the background agents really are still running")
	}
	if !strings.Contains(d.StatusReason, "background") {
		t.Errorf("reason %q drops the background work the row is also waiting on", d.StatusReason)
	}
	if !strings.HasPrefix(d.StatusReason, state.ReasonTurnComplete) {
		t.Errorf("reason %q does not lead with the ask; cqKind reads that prefix", d.StatusReason)
	}
	// The status and the SIGNAL must not be able to disagree: cqAsk is this
	// same ask.Of over this same field, so a wait claimed here is a question
	// the row can quote back.
	if ask.Of(d.Said) == "" {
		t.Error("the record does not carry the ask the cockpit would quote")
	}

	// The idle prompt a minute later is the same wait, and the task flag is
	// what makes it defer rather than relabel the row.
	if apply(d, "Notification:idle_prompt", hookInput{}) {
		t.Error("the trailing idle_prompt rewrote a wait that was already reported")
	}
	if d.Status != state.StatusNeedsInput {
		t.Errorf("status after idle_prompt = %q, want needs-input", d.Status)
	}
}

// The control, and the rule this does not break: background work on its own is
// still no reason to want the human. A report that asks nothing stays working,
// which is the case TestApplyBackgroundTaskWait covers in full.
func TestBackgroundWorkWithNoAskStillWorks(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	tasks := []json.RawMessage{json.RawMessage(`{"task_id":"t1"}`)}
	d := &state.Dispatch{ID: "a", Feature: "accounts", Status: state.StatusWorking}

	apply(d, "Stop", hookInput{
		LastAssistantMessage: "Benchmarks are running in the background. I will report when they land.",
		BackgroundTasks:      tasks,
	})
	if d.Status != state.StatusWorking {
		t.Errorf("status = %q, want working — nothing in that message is a question", d.Status)
	}
}

// The second report from the same dispatcher, measured in its event log: the
// question went up at 21:10:26, and the background agent working beside it
// ("Worker H") ran 29 tools in the next five minutes. Every one of them is a
// PostToolUse on this session, and the first put the row back to working for
// the hour the question stood. Measured on claude 2.1.296, a subagent's hooks
// carry its agent_id, so its tools are told apart from the answer.
func TestASubagentsToolsDoNotAnswerTheSessionsQuestion(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &state.Dispatch{ID: "a", Feature: "robustness", Status: state.StatusWorking}
	h := hookInput{AgentID: "a649d79c2ad0a21b8", AgentType: "general-purpose"}

	apply(d, "PreToolUse:AskUserQuestion", hookInput{ToolName: state.QuestionTool})
	apply(d, "PermissionRequest", hookInput{ToolName: state.QuestionTool})
	for _, tool := range []string{"Bash", "Read", "Bash", "Edit"} {
		h.ToolName = tool
		apply(d, "PostToolUse", h)
	}
	if d.Status != state.StatusBlocked || d.StatusReason != state.ReasonQuestion {
		t.Fatalf("after the worker's tools: %s, %q — want blocked on the question", d.Status, d.StatusReason)
	}

	apply(d, "PostToolUse", hookInput{ToolName: state.QuestionTool})
	if d.Status != state.StatusWorking || len(d.Menus) != 0 {
		t.Errorf("after the answer: %s with %d dialogs open, want working with none", d.Status, len(d.Menus))
	}
}

// Every question in the reporting log was followed about six seconds later by
// a permission_prompt notification. It is the same dialog, and taking it at its
// word filed every question under "approve a permission".
func TestTheNotificationAfterAQuestionLeavesItAQuestion(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &state.Dispatch{ID: "a", Status: state.StatusWorking}
	apply(d, "PreToolUse:AskUserQuestion", hookInput{ToolName: state.QuestionTool})
	apply(d, "Notification:permission_prompt", hookInput{})

	if d.StatusReason != state.ReasonQuestion {
		t.Errorf("reason = %q, want the question's", d.StatusReason)
	}
}

// Two dialogs can be up at once — measured, a question on the session with a
// subagent's Bash prompt queued behind it — and answering either leaves the
// other the human's.
func TestTwoDialogsAreTwoWaits(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	sub := hookInput{AgentID: "adac291934f0c3acd", ToolName: "Bash"}
	question := hookInput{ToolName: state.QuestionTool}

	for name, order := range map[string][]hookInput{
		"question first": {question, sub},
		"prompt first":   {sub, question},
	} {
		d := &state.Dispatch{ID: "a", Status: state.StatusWorking}
		apply(d, "PermissionRequest", question)
		apply(d, "PermissionRequest", sub)

		apply(d, "PostToolUse", order[0])
		if d.Status != state.StatusBlocked {
			t.Errorf("%s: one answered, the other open — status %s, want blocked", name, d.Status)
		}
		apply(d, "PostToolUse", order[1])
		if d.Status != state.StatusWorking {
			t.Errorf("%s: both answered — status %s, want working", name, d.Status)
		}
	}
}

// Denying a prompt fires no hook at all — measured: no PostToolUse, no failure
// event; the subagent went on to its next tool and ended four seconds later.
// Either of those is the evidence, or a refused dialog would hold the row
// blocked for as long as its agent kept working.
func TestARefusedDialogClosesWhenItsAgentMovesOn(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	const agent = "a861a4f75aaf11ec7"

	d := &state.Dispatch{ID: "a", Status: state.StatusWorking}
	apply(d, "PermissionRequest", hookInput{AgentID: agent, ToolName: "Bash"})
	apply(d, "PostToolUse", hookInput{AgentID: agent, ToolName: "ToolSearch"})
	if d.Status != state.StatusWorking {
		t.Errorf("its agent's next tool: status %s, want working", d.Status)
	}

	d = &state.Dispatch{ID: "a", Status: state.StatusWorking}
	apply(d, "PermissionRequest", hookInput{AgentID: agent, ToolName: "Bash"})
	apply(d, "SubagentStop", hookInput{AgentID: agent})
	if d.Status != state.StatusWorking {
		t.Errorf("its agent ended: status %s, want working", d.Status)
	}
}

// The session can end its turn — waiting on the very agent that is asking —
// while a subagent's prompt is up, and it can take a prompt too. Neither
// answers the subagent's dialog.
func TestASubagentsDialogOutlastsTheSessionsTurn(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	for _, event := range []string{"Stop", "UserPromptSubmit", "StopFailure", "Notification:idle_prompt"} {
		d := &state.Dispatch{ID: "a", Status: state.StatusWorking}
		apply(d, "PermissionRequest", hookInput{AgentID: "x", ToolName: "Bash"})
		apply(d, event, hookInput{BackgroundTasks: []json.RawMessage{json.RawMessage(`{}`)}})
		if d.Status != state.StatusBlocked {
			t.Errorf("%s under a subagent's prompt: status %s, want blocked", event, d.Status)
		}
	}
}

// A turn that ends with nothing in flight has no subagent in flight either, so
// a subagent's dialog still open then is a SubagentStop that never came — and
// it goes with the sweep that settles the subagent, instead of holding the
// row at blocked until the session ends.
func TestAStopWithNothingInFlightClosesAMissedSubagentsDialog(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &state.Dispatch{ID: "a", Status: state.StatusWorking}
	apply(d, "PermissionRequest", hookInput{AgentID: "x", ToolName: "Bash"})
	apply(d, "Stop", hookInput{})
	if d.Status == state.StatusBlocked || len(d.Menus) != 0 {
		t.Errorf("status %s with %d dialogs, want the missed one gone", d.Status, len(d.Menus))
	}
}

// An install that has not re-run init has no PermissionRequest hook, so a
// permission prompt is only the notification, which says neither whose nor
// what. That dialog is closed by any tool, as every block was before.
func TestADialogNobodyNamedClosesOnAnyTool(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &state.Dispatch{ID: "a", Status: state.StatusWorking}
	apply(d, "Notification:permission_prompt", hookInput{})
	if d.Status != state.StatusBlocked || d.StatusReason == state.ReasonQuestion {
		t.Fatalf("status %s, %q — want blocked on a permission", d.Status, d.StatusReason)
	}
	apply(d, "PostToolUse", hookInput{AgentID: "x", ToolName: "Bash"})
	if d.Status != state.StatusWorking {
		t.Errorf("status %s, want working", d.Status)
	}
}

// A subagent's dialog on a shipped record reopens it, the way the session's own
// permission prompt always has.
func TestASubagentsDialogReopensADoneRecord(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &state.Dispatch{ID: "a", Status: state.StatusDone}
	apply(d, "PermissionRequest", hookInput{AgentID: "x", ToolName: "Bash"})
	if d.Status != state.StatusBlocked {
		t.Errorf("status %s, want blocked", d.Status)
	}
}
