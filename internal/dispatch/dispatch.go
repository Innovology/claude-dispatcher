// Package dispatch launches a dispatcher: a feature branch materialised in a
// dedicated git worktree, plus an interactive claude session inside a
// dedicated tmux session.
package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a feature name into a branch/session-safe slug.
func Slugify(feature string) string {
	s := slugRe.ReplaceAllString(strings.ToLower(feature), "-")
	return strings.Trim(s, "-")
}

// Launch materialises feature/<slug> in its own git worktree under the state
// dir, records the dispatch, and starts claude with the prompt inside tmux
// session disp-<slug>. Per-dispatch worktrees keep concurrent dispatches —
// and the human — from fighting over the repo's single checkout. The
// CLAUDE_DISPATCHER_ID environment variable is the join key that lets the
// lifecycle hook attribute events back to this record.
func Launch(r repos.Repo, feature, prompt string) (*state.Dispatch, error) {
	slug := Slugify(feature)
	if slug == "" {
		return nil, fmt.Errorf("feature name %q produces an empty slug", feature)
	}
	branch := "feature/" + slug
	worktree := filepath.Join(state.WorktreesDir(), r.Name, slug)
	if err := ensureWorktree(r.Path, worktree, branch); err != nil {
		return nil, err
	}
	baseSHA := ""
	if out, err := exec.Command("git", "-C", worktree, "rev-parse", "HEAD").Output(); err == nil {
		baseSHA = strings.TrimSpace(string(out))
	}

	d := &state.Dispatch{
		ID:           state.NewID(),
		Feature:      feature,
		Slug:         slug,
		RepoPath:     r.Path,
		RepoName:     r.Name,
		Product:      r.Product,
		Branch:       branch,
		WorktreePath: worktree,
		BaseSHA:      baseSHA,
		Prompt:       prompt,
		TmuxSession:  supervisor.UniqueName("disp-" + slug),
		Status:       state.StatusLaunching,
		CreatedAt:    time.Now(),
	}
	if err := state.Save(d); err != nil {
		return nil, err
	}

	// When claude exits we drop to a shell so the tmux session stays open for
	// inspection instead of vanishing.
	cmd := fmt.Sprintf("CLAUDE_DISPATCHER_ID=%s claude %s; exec ${SHELL:-/bin/sh}",
		d.ID, shellQuote(prompt))
	if err := supervisor.NewSession(d.TmuxSession, worktree, cmd); err != nil {
		d.Status = state.StatusExited
		d.StatusReason = "tmux launch failed"
		_ = state.Save(d)
		return nil, err
	}
	return d, nil
}

// ensureWorktree adds a worktree for the branch at path, creating the branch
// from the repo's current HEAD when it doesn't exist yet, and reusing a
// worktree left behind by an earlier dispatch of the same feature.
func ensureWorktree(repoPath, path, branch string) error {
	if out, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		if strings.TrimSpace(string(out)) == branch {
			return nil
		}
		return fmt.Errorf("%s exists but is not a worktree on %s", path, branch)
	}
	// Recover bookkeeping for worktree dirs deleted behind git's back.
	_ = exec.Command("git", "-C", repoPath, "worktree", "prune").Run()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	branchExists := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
	args := []string{"-C", repoPath, "worktree", "add", path, branch}
	if !branchExists {
		args = []string{"-C", repoPath, "worktree", "add", "-b", branch, path}
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add %s: %s", branch, strings.TrimSpace(string(out)))
	}
	return nil
}

// CleanupWorktree removes a dispatch's worktree when git deems it safe (no
// modified or untracked files); a dirty worktree is kept for inspection so
// killing a dispatcher never discards unshipped work.
//
// It reports whether the worktree is gone, not whether this call removed it:
// an absent path (never had a worktree, or already cleaned up) is "gone", so
// callers can report "kept" on false without special-casing.
func CleanupWorktree(repoPath, worktreePath string) bool {
	if worktreePath == "" {
		return true
	}
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return true
	}
	_ = exec.Command("git", "-C", repoPath, "worktree", "remove", worktreePath).Run()
	_, err := os.Stat(worktreePath)
	return os.IsNotExist(err)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
