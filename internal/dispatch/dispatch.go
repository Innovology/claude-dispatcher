// Package dispatch launches a dispatcher: a feature branch materialised in a
// dedicated git worktree, plus an interactive claude session inside a
// dedicated tmux session.
package dispatch

import (
	"context"
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
	return capSlug(strings.Trim(s, "-"))
}

// SlugWords is how many words a slug keeps. The slug names three things at
// once — the branch, the worktree directory and the tmux session — so an
// uncapped one produced `feature/several-improvements-here-use-the-claude-
// design-mcp-https-api-anthropic-com` and a 96-character session name, because
// whatever was typed as the feature name went in whole.
//
// Five words is enough to tell two features apart at a glance and short enough
// to type. The full name still lives on the record and is what every screen
// shows; only the slug is abbreviated.
const SlugWords = 5

// slugMaxLen bounds the result even when five words are each very long, so a
// path component stays comfortably inside what every filesystem accepts.
const slugMaxLen = 60

// capSlug keeps at most SlugWords words and slugMaxLen characters, never
// splitting a word unless a single word exceeds the limit on its own.
func capSlug(s string) string {
	if s == "" {
		return ""
	}
	words := strings.Split(s, "-")
	if len(words) > SlugWords {
		words = words[:SlugWords]
	}
	out := strings.Join(words, "-")
	for len(out) > slugMaxLen && len(words) > 1 {
		words = words[:len(words)-1]
		out = strings.Join(words, "-")
	}
	if len(out) > slugMaxLen {
		out = strings.Trim(out[:slugMaxLen], "-")
	}
	return out
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
	if live := liveDispatch(slug); live != nil {
		return nil, fmt.Errorf("%q is already live in %s (session %s) — kill it, or dispatch under a different feature name",
			live.Feature, live.RepoName, live.TmuxSession)
	}
	branch := "feature/" + slug
	worktree := filepath.Join(state.WorktreesDir(), r.Name, slug)
	if err := ensureWorktree(r.Path, worktree, branch); err != nil {
		return nil, err
	}
	// A fresh worktree is a folder Claude Code has never seen, and it blocks on
	// its trust prompt before reading the prompt we launched it with. Inherit
	// the repo's own trust decision so an unattended session can actually start.
	InheritTrust(r.Path, worktree)

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

	// launchCommand is OS-specific (bash on Unix, cmd.exe on Windows); it keeps
	// the session's window open after claude exits so it stays available for
	// inspection instead of vanishing.
	cmd := launchCommand(d.ID, prompt)
	if err := supervisor.NewSession(d.TmuxSession, worktree, cmd); err != nil {
		d.Status = state.StatusExited
		d.StatusReason = "tmux launch failed"
		_ = state.Save(d)
		return nil, err
	}
	return d, nil
}

// liveDispatch returns a dispatch of the same slug whose session is still
// running, or nil.
//
// The feature name is the key throughout: the worktree path is
// worktrees/<repo>/<slug>, and the cockpit maps a feature name to one record.
// So a second concurrent dispatch of a name already in flight would put two
// claude sessions in a single checkout — the exact collision per-dispatch
// worktrees exist to prevent — and shadow one record with the other in the
// cockpit. Re-dispatching a feature whose session has ended is still fine, and
// deliberately reuses the worktree left behind.
//
// The live tmux session, not the record's status, is the test: a record can be
// left stale by a crash, but a running session is ground truth.
func liveDispatch(slug string) *state.Dispatch {
	for _, d := range state.LoadAll() {
		if d.Slug == slug && d.TmuxSession != "" && sessionAlive(d.TmuxSession) {
			return d
		}
	}
	return nil
}

// sessionAlive and supervisorReady are seams: tests swap them rather than
// stand up real sessions.
var (
	sessionAlive    = supervisor.HasSession
	supervisorReady = supervisor.Available
)

// ReconcileSessions re-derives status from the one fact the lifecycle hook
// cannot report — whether the session behind a record still exists — and marks
// every stray record exited. It returns how many it changed.
//
// Status normally comes from Claude Code hooks, and they are truth right up to
// the moment a session dies without getting to say so: a SIGKILL, a
// `tmux kill-session` from another terminal, a tmux server that went down with
// the machine. No SessionEnd fires for any of those, so the record goes on
// claiming "working" or "waiting on you" forever and the triage table carries a
// dispatcher that is not there. The live session, not the record's status, is
// ground truth — the same call liveDispatch makes.
//
// Note what does NOT strand a record: claude merely exiting. launchCommand
// drops to a login shell so the session survives for inspection, and SessionEnd
// fires on the way out. A missing session means something took it away.
//
// The three statuses swept are a boundary, not caution. Each is reachable only
// through a hook fired from inside the session, so the session provably
// existed and its absence now proves it ended. A launching record has had no
// hook at all, and there is a window — between Launch saving it and NewSession
// returning — where its session does not exist yet and never did; absence
// there proves nothing, so nothing is claimed. Likewise a supervisor we cannot
// even reach is not evidence that every session is gone, which is why an
// unavailable backend sweeps nothing rather than retiring the whole fleet.
func ReconcileSessions(ds []*state.Dispatch) int {
	if !supervisorReady() {
		return 0
	}
	n := 0
	for _, d := range ds {
		if d.TmuxSession == "" {
			continue
		}
		switch d.Status {
		case state.StatusWorking, state.StatusNeedsInput, state.StatusBlocked:
		default:
			continue
		}
		if sessionAlive(d.TmuxSession) {
			continue
		}
		d.Status = state.StatusExited
		d.StatusReason = "its " + supervisor.Backend() + " session is gone"
		if state.Save(d) == nil {
			n++
		}
	}
	return n
}

// fetchTimeout bounds the pre-dispatch fetch. Refreshing the base is a
// courtesy; a slow or unreachable remote must never stall a launch.
const fetchTimeout = 20 * time.Second

// baseRef resolves the start point for a new feature branch: the repo's
// default branch, as the remote sees it.
//
// Letting git default the start point to the repo's HEAD — what `worktree add
// -b` does with no explicit base — silently cuts the branch from whatever the
// human left checked out, so a dispatch starts on top of an unrelated unmerged
// feature and its PR carries that work onto main. Naming origin/<default>
// rather than the local branch also keeps a stale local main out of the base.
func baseRef(repoPath string) string {
	// Best effort: a fresh origin/<default> beats a stale one, but offline is
	// not a launch failure. GIT_TERMINAL_PROMPT=0 stops a credential prompt
	// from holding the fetch open until the timeout expires.
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	fetch := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", "--quiet", "origin")
	fetch.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	_ = fetch.Run()

	// origin/HEAD names the remote's default branch, but it is absent from
	// --single-branch clones and from repos cloned before git recorded it, so
	// fall through the conventional names — and finally to the local checkout,
	// for a repo with no remote at all.
	if out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "--quiet",
		"refs/remotes/origin/HEAD").Output(); err == nil {
		if ref := strings.TrimSpace(string(out)); ref != "" {
			return ref
		}
	}
	for _, cand := range []string{
		"refs/remotes/origin/main", "refs/remotes/origin/master",
		"refs/heads/main", "refs/heads/master",
	} {
		if exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--quiet", cand).Run() == nil {
			return cand
		}
	}
	return "HEAD"
}

// ensureWorktree adds a worktree for the branch at path, creating the branch
// from the repo's default branch when it doesn't exist yet, and reusing a
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
	if !branchExists {
		// Created as its own step so the base is explicit. --no-track because
		// the base is normally a remote-tracking ref, and inheriting it as
		// upstream would make a later plain `git push` refuse: push.default
		// simple rejects a branch whose upstream carries a different name.
		base := baseRef(repoPath)
		if out, err := exec.Command("git", "-C", repoPath, "branch", "--no-track", branch, base).CombinedOutput(); err != nil {
			return fmt.Errorf("git branch %s from %s: %s", branch, base, strings.TrimSpace(string(out)))
		}
	}
	if out, err := exec.Command("git", "-C", repoPath, "worktree", "add", path, branch).CombinedOutput(); err != nil {
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
