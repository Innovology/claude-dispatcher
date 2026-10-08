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
	if got, want := worktreeRootFor(beside, bare), filepath.Join("/src", "player-app"); got != want {
		t.Errorf("a bare-layout project: worktreeRootFor = %q, want %q", got, want)
	}
	// The word is a mode, not a path, and it is matched the way a human types
	// it rather than the way the file happens to be written.
	if got := worktreeRootFor(&config.Config{Worktrees: " Project "}, bare); got == "" {
		t.Error("the mode should be read whatever the spacing and case")
	}
}

// The default is unchanged, and so is every repo that has no project folder to
// join: "" means the state directory, which is where dispatch checkouts have
// always been cut.
func TestWorktreeRootStaysInTheStateDirByDefault(t *testing.T) {
	bare := filepath.Join("/src", "player-app", ".bare")
	clone := filepath.Join("/src", "steve", ".git")

	for name, tc := range map[string]struct {
		cfg    *config.Config
		gitDir string
	}{
		"no config at all":     {nil, bare},
		"nothing asked for":    {&config.Config{}, bare},
		"another mode":         {&config.Config{Worktrees: "state"}, bare},
		"a plain clone":        {&config.Config{Worktrees: "project"}, clone},
		"metadata we could no": {&config.Config{Worktrees: "project"}, ""},
	} {
		if got := worktreeRootFor(tc.cfg, tc.gitDir); got != "" {
			t.Errorf("%s: worktreeRootFor = %q, want the state directory", name, got)
		}
	}
}
