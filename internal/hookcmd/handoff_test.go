package hookcmd

import (
	"testing"

	"claude-dispatcher/internal/state"
)

// handedOff is a record the way `handoff` leaves the dispatcher it is run from:
// its run over, its session still going.
func handedOff() *state.Dispatch {
	return &state.Dispatch{Status: state.StatusExited, StatusReason: state.ReasonHandedOff + `"consume it" in app`}
}

// A handoff is run from inside the session it ends, so the events that follow
// it are that session finishing the turn it ran in: the tool returning, the
// turn ending, claude idling at its prompt, the session closing. Read as they
// would be on any other record, the first Stop put a finished run back on the
// table as waiting on the human — the stop the handoff is meant to spare them.
func TestAHandedOffRunIgnoresItsOwnSessionWindingDown(t *testing.T) {
	for _, event := range []string{"PostToolUse", "Stop", "Notification:idle_prompt", "SessionEnd", "SessionStart", "SubagentStop", "StopFailure"} {
		d := handedOff()
		apply(d, event, hookInput{LastAssistantMessage: "Handed off. Want me to do anything else?"})
		if d.Status != state.StatusExited || !d.HandedOff() {
			t.Errorf("%s moved a handed-off record to %s (%q)", event, d.Status, d.StatusReason)
		}
	}
}

// The same events that reopen a done record reopen one that handed off: the
// human typing into it, or it putting up a dialog, is a new turn with somebody
// waiting on it, and that belongs on the table whatever the run said before.
func TestAHandedOffRunReopensOnANewTurnOrADialog(t *testing.T) {
	cases := map[string]state.Status{
		"UserPromptSubmit":               state.StatusWorking,
		"PermissionRequest":              state.StatusBlocked,
		"PreToolUse:AskUserQuestion":     state.StatusBlocked,
		"Notification:permission_prompt": state.StatusBlocked,
	}
	for event, want := range cases {
		d := handedOff()
		if !apply(d, event, hookInput{}) {
			t.Errorf("%s changed nothing on a handed-off record", event)
		}
		if d.Status != want || d.HandedOff() {
			t.Errorf("%s: got %s (%q), want %s and no longer handed off", event, d.Status, d.StatusReason, want)
		}
	}
}

// Only the reason the handoff wrote holds a record. Once something else has
// ended it — the session closing after the human took it back up — the lineage
// stays and the record is an ordinary exited one again.
func TestOnlyTheHandoffsOwnReasonHolds(t *testing.T) {
	d := &state.Dispatch{Status: state.StatusExited, StatusReason: "session ended", HandedOffTo: []string{"abc"}}
	if d.HandedOff() {
		t.Fatal("an exited record read as handed off for its lineage alone")
	}
	if !apply(d, "Stop", hookInput{}) || d.Status != state.StatusNeedsInput {
		t.Errorf("an ordinary exited record ignored a Stop: %s", d.Status)
	}
}

// The fan-out is bookkeeping and keeps applying under the hold, as it does on a
// done record: a subagent still running when the handoff went out does stop.
func TestAHandedOffRunStillSettlesItsFanOut(t *testing.T) {
	d := handedOff()
	d.SubagentStarted("a1", "Explore", d.CreatedAt)
	if !apply(d, "SubagentStop", hookInput{AgentID: "a1", AgentType: "Explore"}) {
		t.Fatal("a subagent stopping was not recorded on a handed-off record")
	}
	if d.SubagentsLive() != 0 {
		t.Errorf("%d subagents still live", d.SubagentsLive())
	}
}
