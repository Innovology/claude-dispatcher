//go:build !windows

package fleetcmd

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// gitRepo is a repository with one commit on main.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "base"}} {
		full := append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	return dir
}

// portfolio stands in for discovery: two repos in pp and one in no product.
func portfolio(t *testing.T) []repos.Repo {
	t.Helper()
	all := []repos.Repo{
		{Name: "app", Path: gitRepo(t), Product: "pp"},
		{Name: "api", Path: gitRepo(t), Product: "pp"},
		{Name: "other", Path: gitRepo(t)},
	}
	prev := discover
	discover = func() ([]repos.Repo, *config.Config, error) {
		return all, &config.Config{Accounts: map[string]string{"work": t.TempDir()}}, nil
	}
	t.Cleanup(func() { discover = prev })
	return all
}

// From inside a conversation, `worktree` takes a branch in one of its repos
// and says where to go: the session finds its own record by the variable its
// launch line set, and the record is a worker's from then on.
func TestWorktreeTakesABranchFromInsideTheConversation(t *testing.T) {
	all := portfolio(t)
	t.Setenv("HOME", t.TempDir())
	withStore(t, &state.Dispatch{ID: "c1", Feature: "talk", Product: "pp", Branchless: true, Status: state.StatusNeedsInput})
	t.Setenv("CLAUDE_DISPATCHER_ID", "c1")

	var out bytes.Buffer
	if err := runWorktree([]string{"--repo", "api", "fix", "the", "sync"}, &out); err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if !strings.Contains(out.String(), "working on feature/fix-the-sync in ") || !strings.Contains(out.String(), "open the PR") {
		t.Errorf("output:\n%s", out.String())
	}
	d, _ := find("c1")
	if d.Branch != "feature/fix-the-sync" || d.RepoName != "api" || d.RepoPath != all[1].Path || !d.CheckoutCut {
		t.Errorf("record = %+v", d)
	}

	// A repo it was not dispatched over is not one it may work in.
	withStore(t, &state.Dispatch{ID: "c2", Feature: "talk2", Product: "pp", Branchless: true, Status: state.StatusNeedsInput})
	if err := runWorktree([]string{"--id", "c2", "--repo", "other", "x"}, &out); err == nil || !strings.Contains(err.Error(), "api, app") {
		t.Errorf("a repo outside the product: %v", err)
	}
	// With several repos and none named, it asks which.
	if err := runWorktree([]string{"--id", "c2", "x"}, &out); err == nil || !strings.Contains(err.Error(), "--repo") {
		t.Errorf("no repo named: %v", err)
	}
	t.Setenv("CLAUDE_DISPATCHER_ID", "")
	if err := runWorktree([]string{"--repo", "app", "x"}, &out); err == nil || !strings.Contains(err.Error(), "--id") {
		t.Errorf("run from no session: %v", err)
	}
}
