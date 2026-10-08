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
