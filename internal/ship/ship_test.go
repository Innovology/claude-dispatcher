package ship

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

func gitRepo(t *testing.T, commits int) (path string, shas []string) {
	t.Helper()
	path = t.TempDir()
	git := func(args ...string) string {
		full := append([]string{"-C", path, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "main")
	for i := 0; i < commits; i++ {
		git("commit", "--allow-empty", "-m", "c")
		shas = append(shas, git("rev-parse", "HEAD"))
	}
	return path, shas
}

func TestCollectAttributesByProvenance(t *testing.T) {
	active, shas := gitRepo(t, 3)
	idle, _ := gitRepo(t, 0) // init'd but no commits

	ds := []*state.Dispatch{{ID: "d1", Commits: shas[:2]}}
	got := Collect([]repos.Repo{
		{Name: "active", Path: active},
		{Name: "idle", Path: idle},
	}, ds)

	if got.Commits != 3 {
		t.Errorf("Commits = %d, want 3", got.Commits)
	}
	if got.Dispatched != 2 {
		t.Errorf("Dispatched = %d, want 2", got.Dispatched)
	}
	if got.DispatchedPct() != 66 {
		t.Errorf("DispatchedPct = %d, want 66", got.DispatchedPct())
	}
	if got.ReposActive != 1 || got.ReposTotal != 2 {
		t.Errorf("repos: active=%d total=%d, want 1/2", got.ReposActive, got.ReposTotal)
	}
}

func TestCollectCountsFeaturesLiveToday(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-48 * time.Hour)
	ds := []*state.Dispatch{
		{ID: "a", DeployedAt: &now},
		{ID: "b", DeployedAt: &yesterday},
		{ID: "c"},
	}
	got := Collect(nil, ds)
	if got.FeaturesLive != 1 {
		t.Errorf("FeaturesLive = %d, want 1", got.FeaturesLive)
	}
}

func TestDispatchedPctZeroCommits(t *testing.T) {
	if (Stats{}).DispatchedPct() != 0 {
		t.Error("zero commits must not divide by zero")
	}
}
