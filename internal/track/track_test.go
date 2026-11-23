package track

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
)

// The regression this guards: a dispatcher that merges its own PR mid-run was
// flipped to done while its session was still going, and hookcmd would not let
// anything the session said afterwards downgrade that — so a live session
// stuck at a permission prompt disappeared from the cockpit, which then showed
// the empty-fleet dispatch form.
func TestMidWork(t *testing.T) {
	cases := []struct {
		status state.Status
		want   bool
	}{
		{state.StatusWorking, true},
		{state.StatusBlocked, true},
		{state.StatusLaunching, true},
		{state.StatusNeedsInput, false}, // turn over — "done means live" applies
		{state.StatusExited, false},
		{state.StatusDone, false},
	}
	for _, c := range cases {
		if got := midWork(&state.Dispatch{Status: c.status}); got != c.want {
			t.Errorf("midWork(%s) = %v, want %v", c.status, got, c.want)
		}
	}
}

// A conversation that went into a branch with a merge already behind it is not
// handed that merge, nor called live for it; a conversation's own merge, and a
// dispatch's, are taken as they always were.
func TestAMergeBeforeAConversationIsNotItsWork(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  \"pr list --head umberto/calendar\"*) echo '[{\"number\":7,\"state\":\"MERGED\",\"url\":\"u\",\"mergedAt\":\"2026-01-02T00:00:00Z\"}]' ;;\n" +
		"  *) echo '[]' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	gh.InvalidateCache()
	gh.ResetThrottle()
	t.Cleanup(gh.InvalidateCache)

	after := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	talk := &state.Dispatch{ID: "c1", Feature: "talk", Branchless: true, Branch: "umberto/calendar",
		RepoPath: dir, Status: state.StatusNeedsInput, CreatedAt: after}
	early := &state.Dispatch{ID: "c2", Feature: "early talk", Branchless: true, Branch: "umberto/calendar",
		RepoPath: dir, Status: state.StatusNeedsInput, CreatedAt: before}
	Refresh([]*state.Dispatch{talk, early}, &config.Config{})
	if talk.PRNumber != 0 || talk.Status == state.StatusDone {
		t.Errorf("a conversation begun after the merge took PR #%d and is %s", talk.PRNumber, talk.Status)
	}
	if early.PRNumber != 7 || early.Status != state.StatusDone {
		t.Errorf("a conversation begun before its branch merged has PR #%d and is %s; want #7, done", early.PRNumber, early.Status)
	}
}
