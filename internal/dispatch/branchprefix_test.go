package dispatch

import (
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/supervisor"
)

func strptr(s string) *string { return &s }

// The default is the convention and must survive a Repo nobody configured:
// every dispatch works on a feature branch, and the prefix is what says so at
// a glance.
func TestBranchKeepsItsFeaturePrefixByDefault(t *testing.T) {
	if got := branchFor(repos.Repo{Name: "player-app"}, "login-fix"); got != "feature/login-fix" {
		t.Errorf("branchFor = %q, want feature/login-fix", got)
	}
}

// An empty prefix is a real answer, not an unset one: on a machine whose
// checkouts are folders named after their branches, a prefix makes every
// dispatch a folder whose name is not its branch.
func TestAnEmptyPrefixMakesTheBranchTheFeature(t *testing.T) {
	r := repos.Repo{Name: "player-app", BranchPrefix: strptr("")}
	if got := branchFor(r, "dispatch-test-2"); got != "dispatch-test-2" {
		t.Errorf("branchFor = %q, want the bare slug", got)
	}
	// Any other prefix is carried verbatim — a trailing slash is the human's
	// to type, since "wip-" is as good a prefix as "wip/".
	if got := branchFor(repos.Repo{BranchPrefix: strptr("wip-")}, "login-fix"); got != "wip-login-fix" {
		t.Errorf("branchFor = %q, want wip-login-fix", got)
	}
}

// The pair together is the layout this was asked for: a checkout beside the
// project, named for the feature, on a branch of exactly that name.
func TestWorktreeAndBranchCanCarryTheSameName(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	project := t.TempDir()

	prevNew, prevUniq, prevAlive := newSession, uniqueName, sessionAlive
	newSession = func(supervisor.Session, string, string, string) error { return nil }
	uniqueName = func(s supervisor.Session) string { return s.Name }
	sessionAlive = func(supervisor.Session) bool { return false }
	t.Cleanup(func() { newSession, uniqueName, sessionAlive = prevNew, prevUniq, prevAlive })

	d, err := Launch(repos.Repo{
		Name: "player-app", Path: repo,
		WorktreeRoot: project, BranchPrefix: strptr(""),
	}, "dispatch test 2", "go", ModeAuto, DefaultModel, DefaultRoot, false, ownAccount)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if d.Branch != d.Slug {
		t.Errorf("branch %q and feature %q differ", d.Branch, d.Slug)
	}
	if got, want := d.WorktreePath, filepath.Join(project, d.Branch); got != want {
		t.Errorf("worktree %q, want %q — the folder should carry its branch's name", got, want)
	}
	if filepath.Base(d.WorktreePath) != d.Branch {
		t.Errorf("the folder is %q and the branch is %q", filepath.Base(d.WorktreePath), d.Branch)
	}
}
