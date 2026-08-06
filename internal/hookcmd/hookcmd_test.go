package hookcmd

import (
	"os/exec"
	"strings"
	"testing"

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
