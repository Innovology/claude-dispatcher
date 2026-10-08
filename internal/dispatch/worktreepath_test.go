package dispatch

import (
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// A repo that names a project folder gets its dispatch checkout beside the
// others, under the feature's own name and with no repo component: the project
// folder is the repo, so `<project>/<repo>/<slug>` would be a directory named
// after the repository inside the repository.
func TestWorktreeGoesBesideTheProjectWhenTheRepoSaysSo(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	r := repos.Repo{Name: "player-app", WorktreeRoot: "/src/player-app"}
	if got, want := worktreePath(r, "dispatch-test-2"), "/src/player-app/dispatch-test-2"; got != want {
		t.Errorf("worktreePath = %q, want %q", got, want)
	}
}

// Everything else is where it has always been.
func TestWorktreeStaysUnderTheStateDirOtherwise(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	r := repos.Repo{Name: "steve"}
	want := filepath.Join(state.WorktreesDir(), "steve", "login-fix")
	if got := worktreePath(r, "login-fix"); got != want {
		t.Errorf("worktreePath = %q, want %q", got, want)
	}
}

// End to end through Launch: the record carries the path the session actually
// runs in, which is what resume, kill and every screen read afterwards.
func TestLaunchRecordsTheProjectSideWorktree(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	// A project folder of its own, so the dispatch checkout is cut beside the
	// repo rather than inside it.
	project := t.TempDir()

	prevNew, prevUniq, prevAlive := newSession, uniqueName, sessionAlive
	newSession = func(supervisor.Session, string, string, string) error { return nil }
	uniqueName = func(s supervisor.Session) string { return s.Name }
	sessionAlive = func(supervisor.Session) bool { return false }
	t.Cleanup(func() { newSession, uniqueName, sessionAlive = prevNew, prevUniq, prevAlive })

	d, err := Launch(repos.Repo{Name: "player-app", Path: repo, WorktreeRoot: project},
		"dispatch test 2", "go", ModeAuto, DefaultModel, DefaultRoot, false)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	want := filepath.Join(project, d.Slug)
	if d.WorktreePath != want {
		t.Errorf("record's worktree = %q, want %q", d.WorktreePath, want)
	}
	if strings.HasPrefix(d.WorktreePath, state.WorktreesDir()) {
		t.Errorf("the checkout is still under the state directory: %q", d.WorktreePath)
	}
}
