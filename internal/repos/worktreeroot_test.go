package repos

import (
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/config"
)

// Reported from a machine that keeps one folder per branch beside a bare repo:
// a dispatch's checkout was the only one that lived somewhere else, so git
// listed a worktree of player-app whose folder was not in player-app. With
// `worktrees = "project"` it is cut where the others are.
func TestWorktreeRootJoinsTheProjectWhenAsked(t *testing.T) {
	beside := &config.Config{Worktrees: "project"}
	bare := filepath.Join("/src", "player-app", ".bare")
	root, prefix := worktreeLayoutFor(beside, bare, nil)
	if want := filepath.Join("/src", "player-app"); root != want || prefix != "" {
		t.Errorf("a bare-layout project: worktreeLayoutFor = %q, %q, want %q and no prefix", root, prefix, want)
	}
	// The word is a mode, not a path, and it is matched the way a human types
	// it rather than the way the file happens to be written.
	if root, _ := worktreeLayoutFor(&config.Config{Worktrees: " Project "}, bare, nil); root == "" {
		t.Error("the mode should be read whatever the spacing and case")
	}
}

// Then reported from the same machine for playerpulse, an ordinary clone with
// twenty worktrees beside it named `playerpulse-<what>`: its dispatches were
// still cut in the state directory, the one checkout of it that was not with
// the others. Those worktrees are the convention, so it is joined — named the
// way they are, beside them — while one of Claude Code's own inside the clone
// counts for nothing.
func TestWorktreeRootJoinsTheClonesSiblings(t *testing.T) {
	org := filepath.Join("/src", "org")
	clone := filepath.Join(org, "playerpulse")
	wts := []Worktree{
		{Path: clone, Main: true},
		{Path: filepath.Join(org, "playerpulse-byga-scoping")},
		{Path: filepath.Join(org, "playerpulse-o6")},
		{Path: filepath.Join(clone, ".claude", "worktrees", "agent-a3806c3a")},
	}
	root, prefix := worktreeLayoutFor(&config.Config{Worktrees: "project"}, filepath.Join(clone, ".git"), wts)
	if root != org || prefix != "playerpulse-" {
		t.Errorf("worktreeLayoutFor = %q, %q, want %q, %q", root, prefix, org, "playerpulse-")
	}
}

// The separator is the one the human's worktrees use, the commonest where they
// disagree, and a folder that merely starts with the clone's name — or is a
// sibling that is not one of its worktrees — is not one of them.
func TestWorktreeRootNamesSiblingsTheWayTheyAreNamed(t *testing.T) {
	org := filepath.Join("/src", "org")
	clone := filepath.Join(org, "app")
	cfg := &config.Config{Worktrees: "project"}
	for name, tc := range map[string]struct {
		siblings []string
		want     string
	}{
		"underscored":        {[]string{"app_login"}, "app_"},
		"the commonest":      {[]string{"app.x", "app_login", "app_billing"}, "app_"},
		"only a longer name": {[]string{"apple", "app-"}, ""},
	} {
		wts := []Worktree{{Path: clone, Main: true}}
		for _, s := range tc.siblings {
			wts = append(wts, Worktree{Path: filepath.Join(org, s)})
		}
		root, prefix := worktreeLayoutFor(cfg, filepath.Join(clone, ".git"), wts)
		if prefix != tc.want || (tc.want == "") != (root == "") {
			t.Errorf("%s: worktreeLayoutFor = %q, %q, want prefix %q", name, root, prefix, tc.want)
		}
	}
}

// The default is unchanged, and so is every repo that has no place to join:
// "" means the state directory, which is where dispatch checkouts have always
// been cut.
func TestWorktreeRootStaysInTheStateDirByDefault(t *testing.T) {
	bare := filepath.Join("/src", "player-app", ".bare")
	clone := filepath.Join("/src", "steve")
	siblings := []Worktree{{Path: clone, Main: true}, {Path: clone + "-login"}}

	for name, tc := range map[string]struct {
		cfg    *config.Config
		gitDir string
		wts    []Worktree
	}{
		"no config at all":           {nil, bare, nil},
		"nothing asked for":          {&config.Config{}, bare, nil},
		"another mode":               {&config.Config{Worktrees: "state"}, bare, nil},
		"siblings, not asked":        {&config.Config{}, filepath.Join(clone, ".git"), siblings},
		"a plain clone":              {&config.Config{Worktrees: "project"}, filepath.Join(clone, ".git"), siblings[:1]},
		"worktrees elsewhere":        {&config.Config{Worktrees: "project"}, filepath.Join(clone, ".git"), []Worktree{{Path: clone, Main: true}, {Path: "/tmp/steve-login"}}},
		"metadata we could not read": {&config.Config{Worktrees: "project"}, "", siblings},
	} {
		if root, prefix := worktreeLayoutFor(tc.cfg, tc.gitDir, tc.wts); root != "" || prefix != "" {
			t.Errorf("%s: worktreeLayoutFor = %q, %q, want the state directory", name, root, prefix)
		}
	}
}
