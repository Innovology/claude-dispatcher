package hookcmd

import (
	"testing"

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
