package hookcmd

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

func TestApplyTransitions(t *testing.T) {
	cases := []struct {
		from    state.Status
		event   string
		want    state.Status
		changed bool
	}{
		{state.StatusLaunching, "SessionStart", state.StatusWorking, true},
		{state.StatusNeedsInput, "UserPromptSubmit", state.StatusWorking, true},
		{state.StatusWorking, "Stop", state.StatusNeedsInput, true},
		{state.StatusWorking, "Notification:idle_prompt", state.StatusNeedsInput, true},
		{state.StatusWorking, "Notification:permission_prompt", state.StatusBlocked, true},
		{state.StatusBlocked, "PostToolUse", state.StatusWorking, true},
		{state.StatusWorking, "PostToolUse", state.StatusWorking, false},
		{state.StatusWorking, "SessionEnd", state.StatusExited, true},
		{state.StatusDone, "Stop", state.StatusDone, false},
		{state.StatusDone, "SessionEnd", state.StatusDone, false},
		{state.StatusDone, "Notification:idle_prompt", state.StatusDone, false},
		{state.StatusDone, "PostToolUse", state.StatusDone, false},
		{state.StatusDone, "SessionStart", state.StatusDone, false},
		{state.StatusWorking, "SomeUnknownEvent", state.StatusWorking, false},
	}
	for _, c := range cases {
		d := &state.Dispatch{Status: c.from}
		changed := apply(d, c.event, hookInput{})
		if changed != c.changed || d.Status != c.want {
			t.Errorf("%s + %s: got (%s, changed=%v), want (%s, changed=%v)",
				c.from, c.event, d.Status, changed, c.want, c.changed)
		}
	}
}

// track flips a record to done the moment its PR merges, which routinely
// happens while the session is still running. When the session then asks for a
// permission approval, or the human sends it another prompt, the record has to
// come back — otherwise a live dispatcher sits at a prompt nobody can see,
// because triage only shows blocked/needs/review/working rows.
func TestApplyReopensDone(t *testing.T) {
	d := &state.Dispatch{Status: state.StatusDone}
	if !apply(d, "Notification:permission_prompt", hookInput{}) {
		t.Fatal("a permission prompt must reopen a done dispatch")
	}
	if d.Status != state.StatusBlocked {
		t.Fatalf("want blocked, got %s", d.Status)
	}
	// And once reopened it behaves like any other blocked dispatch: the tool
	// completing means the approval was given.
	if !apply(d, "PostToolUse", hookInput{}) || d.Status != state.StatusWorking {
		t.Fatalf("want working after PostToolUse, got %s", d.Status)
	}

	d = &state.Dispatch{Status: state.StatusDone}
	if !apply(d, "UserPromptSubmit", hookInput{}) || d.Status != state.StatusWorking {
		t.Fatalf("a new prompt must reopen a done dispatch, got %s", d.Status)
	}
}

// A Stop with in-flight background tasks means the session is paused waiting
// to be woken, not waiting on the human — and the later idle_prompt (which
// carries no task info) must not undo that verdict.
func TestApplyBackgroundTaskWait(t *testing.T) {
	tasks := []json.RawMessage{json.RawMessage(`{"task_id":"t1"}`), json.RawMessage(`{"task_id":"t2"}`)}
	d := &state.Dispatch{Status: state.StatusWorking}

	if !apply(d, "Stop", hookInput{BackgroundTasks: tasks}) {
		t.Fatal("Stop with pending tasks should report a change")
	}
	if d.Status != state.StatusWorking || !d.WaitingOnTasks {
		t.Fatalf("want working+waiting, got %s waiting=%v", d.Status, d.WaitingOnTasks)
	}
	if d.StatusReason != "waiting on 2 background tasks" {
		t.Errorf("unexpected reason %q", d.StatusReason)
	}

	if apply(d, "Notification:idle_prompt", hookInput{}) {
		t.Error("idle_prompt must not downgrade a task-waiting dispatch")
	}
	if d.Status != state.StatusWorking {
		t.Errorf("status downgraded to %s", d.Status)
	}

	// Background work done, session woke, ran, and stopped for real.
	if !apply(d, "Stop", hookInput{}) {
		t.Fatal("plain Stop should report a change")
	}
	if d.Status != state.StatusNeedsInput || d.WaitingOnTasks {
		t.Fatalf("want needs-input+cleared, got %s waiting=%v", d.Status, d.WaitingOnTasks)
	}
}

func TestApplyUserPromptClearsTaskWait(t *testing.T) {
	d := &state.Dispatch{Status: state.StatusWorking, WaitingOnTasks: true}
	apply(d, "UserPromptSubmit", hookInput{})
	if d.WaitingOnTasks {
		t.Error("UserPromptSubmit should clear WaitingOnTasks")
	}
	d = &state.Dispatch{Status: state.StatusLaunching, WaitingOnTasks: true}
	apply(d, "SessionStart", hookInput{})
	if d.WaitingOnTasks {
		t.Error("SessionStart should clear WaitingOnTasks")
	}
}

func TestApplySessionStartBindsSession(t *testing.T) {
	d := &state.Dispatch{Status: state.StatusLaunching}
	apply(d, "SessionStart", hookInput{SessionID: "s1", TranscriptPath: "/t.jsonl"})
	if d.SessionID != "s1" || d.TranscriptPath != "/t.jsonl" {
		t.Errorf("session not bound: %+v", d)
	}
}

func TestResolve(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	bySession := &state.Dispatch{ID: "id1", SessionID: "sess1", Status: state.StatusWorking}
	launching := &state.Dispatch{ID: "id2", RepoPath: "/repo/two", Status: state.StatusLaunching}
	for _, d := range []*state.Dispatch{bySession, launching} {
		if err := state.Save(d); err != nil {
			t.Fatal(err)
		}
	}

	if d := resolve("id2", "Stop", hookInput{}); d == nil || d.ID != "id2" {
		t.Error("env dispatcher id should win")
	}
	if d := resolve("", "Stop", hookInput{SessionID: "sess1"}); d == nil || d.ID != "id1" {
		t.Error("session id should match")
	}
	if d := resolve("", "SessionStart", hookInput{Cwd: "/repo/two"}); d == nil || d.ID != "id2" {
		t.Error("SessionStart should bind launching dispatch by cwd")
	}
	worktreed := &state.Dispatch{ID: "id3", RepoPath: "/repo/three",
		WorktreePath: "/wt/three", Status: state.StatusLaunching}
	if err := state.Save(worktreed); err != nil {
		t.Fatal(err)
	}
	if d := resolve("", "SessionStart", hookInput{Cwd: "/wt/three"}); d == nil || d.ID != "id3" {
		t.Error("SessionStart should bind launching dispatch by worktree cwd")
	}
	if d := resolve("", "SessionStart", hookInput{Cwd: "/repo/three"}); d != nil {
		t.Error("a worktree-backed dispatch must not bind by repo cwd")
	}
	if d := resolve("", "Stop", hookInput{Cwd: "/repo/two"}); d != nil {
		t.Error("cwd binding must be SessionStart-only")
	}
	if d := resolve("", "Stop", hookInput{SessionID: "unknown"}); d != nil {
		t.Error("foreign session should not resolve")
	}
}

func TestRefreshCommits(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "main")
	git("commit", "--allow-empty", "-m", "base")
	base := git("rev-parse", "HEAD")
	git("checkout", "-b", "feature/f")
	git("commit", "--allow-empty", "-m", "one")
	git("commit", "--allow-empty", "-m", "two")

	d := &state.Dispatch{RepoPath: repo, Branch: "feature/f", BaseSHA: base}
	if !refreshCommits(d) || len(d.Commits) != 2 {
		t.Fatalf("expected 2 attributed commits, got %d", len(d.Commits))
	}
	if refreshCommits(d) {
		t.Error("unchanged commits should report no change")
	}
	if d := (&state.Dispatch{RepoPath: repo, Branch: "feature/f"}); refreshCommits(d) {
		t.Error("missing BaseSHA must be a no-op")
	}
}

func TestSamePath(t *testing.T) {
	if !samePath("/a/b/../b", "/a/b") {
		t.Error("cleaned paths should match")
	}
	if samePath("/a", "/b") {
		t.Error("different paths should not match")
	}
}

// Parking is the human saying "I cannot answer that right now". The one event
// that dissolves it is a prompt reaching the session — someone just answered —
// and nothing else the session does may empty the shelf: a parked dispatcher
// is allowed to sit at its prompt, go idle, and even die with the machine.
func TestApplyOnlyAPromptClearsThePark(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	parked := func(st state.Status) *state.Dispatch {
		return &state.Dispatch{Status: st, ParkedReason: "waiting on legal", ParkedAt: &at}
	}

	d := parked(state.StatusNeedsInput)
	if !apply(d, "UserPromptSubmit", hookInput{}) || d.Parked() {
		t.Errorf("a submitted prompt must clear the park, got parked=%v", d.Parked())
	}

	for _, ev := range []string{"Stop", "Notification:idle_prompt", "SessionEnd", "SessionStart"} {
		d := parked(state.StatusWorking)
		apply(d, ev, hookInput{})
		if !d.Parked() {
			t.Errorf("%s cleared the park — only the human, a kill or a prompt may", ev)
		}
	}
}

// The fan-out is an annotation, never a status: subagent events record which
// agents the session spun out and change nothing about whether it is working
// or waiting — a SubagentStart arriving while blocked must not un-block it.
func TestApplySubagentEventsNeverTouchStatus(t *testing.T) {
	d := &state.Dispatch{Status: state.StatusBlocked, StatusReason: "waiting on a permission approval"}
	if !apply(d, "SubagentStart", hookInput{AgentID: "a1", AgentType: "Explore"}) {
		t.Fatal("a named start must record")
	}
	if d.Status != state.StatusBlocked || d.StatusReason != "waiting on a permission approval" {
		t.Fatalf("subagent events must leave status alone, got %s (%s)", d.Status, d.StatusReason)
	}
	if !apply(d, "SubagentStop", hookInput{AgentID: "a1", AgentType: "Explore"}) {
		t.Fatal("a stop for a known id must record")
	}
	if d.Status != state.StatusBlocked {
		t.Fatalf("subagent events must leave status alone, got %s", d.Status)
	}
	if d.SubagentsLive() != 0 || d.SubagentsDone() != 1 {
		t.Fatalf("live=%d done=%d, want 0 and 1", d.SubagentsLive(), d.SubagentsDone())
	}
	// A payload naming no agent records nothing.
	if apply(d, "SubagentStart", hookInput{}) {
		t.Fatal("an unnameable subagent cannot be tracked")
	}
}

// A Stop with nothing in flight ends the turn, and with it the fan-out: an
// entry still live at that point is a SubagentStop that never arrived, and
// must not claim a running fan-out forever. With background tasks in the
// payload the sweep must NOT run — a background agent outlives the turn.
func TestApplyStopSweepsTheFanOut(t *testing.T) {
	d := &state.Dispatch{Status: state.StatusWorking}
	apply(d, "SubagentStart", hookInput{AgentID: "a1", AgentType: "Explore"})
	apply(d, "SubagentStart", hookInput{AgentID: "a2", AgentType: "Plan"})

	tasks := []json.RawMessage{json.RawMessage(`{"task_id":"t1"}`)}
	apply(d, "Stop", hookInput{BackgroundTasks: tasks})
	if d.SubagentsLive() != 2 {
		t.Fatalf("background tasks in flight — live subagents must survive the Stop, got %d", d.SubagentsLive())
	}

	apply(d, "Stop", hookInput{})
	if d.SubagentsLive() != 0 || d.SubagentsDone() != 2 {
		t.Fatalf("live=%d done=%d after a bare Stop, want 0 and 2", d.SubagentsLive(), d.SubagentsDone())
	}
}

// Each turn tells its own fan-out story: a new prompt drops the finished
// entries, while a background agent still running carries over. A new session
// starts with no fan-out at all.
func TestApplyPromptAndSessionStartResetTheFanOut(t *testing.T) {
	d := &state.Dispatch{Status: state.StatusNeedsInput}
	apply(d, "SubagentStart", hookInput{AgentID: "a1", AgentType: "Explore"})
	apply(d, "SubagentStart", hookInput{AgentID: "a2", AgentType: "Plan"})
	apply(d, "SubagentStop", hookInput{AgentID: "a1", AgentType: "Explore"})

	apply(d, "UserPromptSubmit", hookInput{})
	if d.SubagentsLive() != 1 || d.SubagentsDone() != 0 {
		t.Fatalf("live=%d done=%d after a prompt, want the live one kept and the done one dropped",
			d.SubagentsLive(), d.SubagentsDone())
	}

	apply(d, "SessionStart", hookInput{SessionID: "s1"})
	if len(d.Subagents) != 0 {
		t.Fatalf("no subagent survives the session that spun it out, got %+v", d.Subagents)
	}
}

// Done means live: like every event that is not proof the human is being
// waited on, subagent events on a done record are dropped.
func TestApplyDoneRecordIgnoresSubagentEvents(t *testing.T) {
	d := &state.Dispatch{Status: state.StatusDone}
	if apply(d, "SubagentStart", hookInput{AgentID: "a1", AgentType: "Explore"}) {
		t.Fatal("a done record takes no subagent events")
	}
	if len(d.Subagents) != 0 {
		t.Fatalf("recorded %d entries on a done record", len(d.Subagents))
	}
}
