// Package dispatch launches a dispatcher: a feature branch in the target
// repo plus an interactive claude session inside a dedicated tmux session.
package dispatch

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/tmux"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a feature name into a branch/session-safe slug.
func Slugify(feature string) string {
	s := slugRe.ReplaceAllString(strings.ToLower(feature), "-")
	return strings.Trim(s, "-")
}

// Launch creates (or checks out) feature/<slug> in the repo, records the
// dispatch, and starts claude with the prompt inside tmux session
// disp-<slug>. The CLAUDE_DISPATCHER_ID environment variable is the join key
// that lets the lifecycle hook attribute events back to this record.
func Launch(r repos.Repo, feature, prompt string) (*state.Dispatch, error) {
	slug := Slugify(feature)
	if slug == "" {
		return nil, fmt.Errorf("feature name %q produces an empty slug", feature)
	}
	branch := "feature/" + slug
	if err := ensureBranch(r.Path, branch); err != nil {
		return nil, err
	}

	d := &state.Dispatch{
		ID:          state.NewID(),
		Feature:     feature,
		Slug:        slug,
		RepoPath:    r.Path,
		RepoName:    r.Name,
		Product:     r.Product,
		Branch:      branch,
		Prompt:      prompt,
		TmuxSession: tmux.UniqueName("disp-" + slug),
		Status:      state.StatusLaunching,
		CreatedAt:   time.Now(),
	}
	if err := state.Save(d); err != nil {
		return nil, err
	}

	// When claude exits we drop to a shell so the tmux session stays open for
	// inspection instead of vanishing.
	cmd := fmt.Sprintf("CLAUDE_DISPATCHER_ID=%s claude %s; exec ${SHELL:-/bin/sh}",
		d.ID, shellQuote(prompt))
	if err := tmux.NewSession(d.TmuxSession, r.Path, cmd); err != nil {
		d.Status = state.StatusExited
		d.StatusReason = "tmux launch failed"
		state.Save(d)
		return nil, err
	}
	return d, nil
}

func ensureBranch(repoPath, branch string) error {
	exists := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
	var out []byte
	var err error
	if exists {
		out, err = exec.Command("git", "-C", repoPath, "checkout", branch).CombinedOutput()
	} else {
		out, err = exec.Command("git", "-C", repoPath, "checkout", "-b", branch).CombinedOutput()
	}
	if err != nil {
		return fmt.Errorf("git checkout %s: %s", branch, strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
