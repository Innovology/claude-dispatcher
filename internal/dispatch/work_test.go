//go:build !windows

package dispatch

import (
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// conversation launches a branchless dispatch over pp starting on player-app,
// and returns it with the portfolio it spans.
func conversation(t *testing.T) (*state.Dispatch, []repos.Repo) {
	t.Helper()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	all := portfolio(t)
	stubStart(t)
	s, _ := ScopeOf(all, "", "player-app")
	d, err := LaunchBranchless(s, "talk", "?", ModeAuto, DefaultModel, false, ownAccount)
	if err != nil {
		t.Fatal(err)
	}
	return d, all
}

// The conversation arrives at work and takes a branch for it: cut the way a
// dispatch's is, where the repo's layout puts one, and the record becomes a
// worker's — its branch, repo and base set so the tracker and provenance find
// it — while its folder, where the transcript is keyed, stays put.
func TestAConversationTakesABranchForNewWork(t *testing.T) {
	d, all := conversation(t)
	home := d.WorktreePath
	pulse := all[0] // playerpulse, in pp, whose work merges into dev
	gitIn(t, pulse.Path, "branch", "dev")

	path, err := TakeBranch(d, pulse, "fix the sync", DefaultRoot)
	if err != nil {
		t.Fatalf("TakeBranch: %v", err)
	}
	if want := filepath.Join(state.WorktreesDir(), "playerpulse", "fix-the-sync"); path != want {
		t.Errorf("checkout at %q, want %q", path, want)
	}
	if got := worktreeBranch(t, path); got != "feature/fix-the-sync" {
		t.Errorf("checkout is on %q, want feature/fix-the-sync", got)
	}
	if d.Branch != "feature/fix-the-sync" || d.RepoName != "playerpulse" || d.RepoPath != pulse.Path ||
		d.Root != "dev" || d.BaseSHA != gitIn(t, path, "rev-parse", "HEAD") {
		t.Errorf("record = branch %q repo %q at %q root %q base %q", d.Branch, d.RepoName, d.RepoPath, d.Root, d.BaseSHA)
	}
	if d.WorktreePath != home || d.Conversation() || d.RemovableCheckout() != path {
		t.Errorf("folder %q (want %q), conversation %v, removable %q", d.WorktreePath, home, d.Conversation(), d.RemovableCheckout())
	}
	// It is on disk, not just in memory: the next load sees the worker.
	for _, rec := range state.LoadAll() {
		if rec.ID == d.ID && rec.Branch != d.Branch {
			t.Errorf("saved branch %q, want %q", rec.Branch, d.Branch)
		}
	}
	// One branch per record: more work is sent out, not taken.
	if _, err := TakeBranch(d, pulse, "and another", DefaultRoot); err == nil || !strings.Contains(err.Error(), "dispatch") {
		t.Errorf("a second branch was not refused, or the refusal did not say what to do: %v", err)
	}
}

// Going into a branch that already exists is going into it: the worktree it
// already has, entered and never removed — somebody's PR branch, checked out
// beside the repo — or, with none, a worktree cut for it under its own name.
func TestAConversationGoesIntoABranchThatExists(t *testing.T) {
	d, all := conversation(t)
	app := all[1]
	theirs := filepath.Join(t.TempDir(), "umberto-calendar")
	gitIn(t, app.Path, "worktree", "add", "-q", "-b", "umberto/calendar", theirs)

	path, err := TakeBranch(d, app, "umberto/calendar", DefaultRoot)
	if err != nil {
		t.Fatalf("TakeBranch: %v", err)
	}
	if !samePath(path, theirs) || d.CheckoutCut || d.RemovableCheckout() != "" {
		t.Errorf("entered %q (want %q), cut %v, removable %q — it is not ours to remove", path, theirs, d.CheckoutCut, d.RemovableCheckout())
	}
	if d.Branch != "umberto/calendar" || d.Root != "" {
		t.Errorf("branch %q root %q; want their branch, cut from nothing", d.Branch, d.Root)
	}

	d2, all2 := conversation(t)
	gitIn(t, all2[1].Path, "branch", "spike/x")
	path, err = TakeBranch(d2, all2[1], "spike/x", DefaultRoot)
	if err != nil {
		t.Fatalf("TakeBranch on a branch with no worktree: %v", err)
	}
	if worktreeBranch(t, path) != "spike/x" || !d2.CheckoutCut || d2.Branch != "spike/x" {
		t.Errorf("checkout %q on %q, cut %v; want spike/x in a worktree of ours", path, worktreeBranch(t, path), d2.CheckoutCut)
	}
}

// What it will not do: work in the checkout every conversation reads, take a
// branch for a dispatch that already has one, or quietly drop a root for a
// branch that already exists.
func TestTakingABranchRefusesWhatItShould(t *testing.T) {
	d, all := conversation(t)
	app := all[1]
	if _, err := TakeBranch(d, app, "main", DefaultRoot); err == nil || !strings.Contains(err.Error(), "--root main") {
		t.Errorf("entering the canonical checkout's branch: %v", err)
	}
	gitIn(t, app.Path, "branch", "spike/y")
	if _, err := TakeBranch(d, app, "spike/y", Root("main")); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a root for an existing branch: %v", err)
	}
	if _, err := TakeBranch(&state.Dispatch{Feature: "f", Branch: "feature/f"}, app, "x", DefaultRoot); err == nil {
		t.Error("a branch dispatch was given a second branch")
	}
	if _, err := TakeBranch(d, app, "!!!", DefaultRoot); err == nil {
		t.Error("a name with no slug was taken")
	}
}

// The disk reclaim finds the worktree a finished conversation cut, as it finds
// any finished dispatch's, and never lists the conversation's own folder.
func TestTidyReclaimsTheWorktreeAConversationCut(t *testing.T) {
	d, all := conversation(t)
	path, err := TakeBranch(d, all[1], "spike", DefaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	d.Stop(state.StatusExited, "session ended", d.CreatedAt)
	d.TmuxSession = ""
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range PlanTidy(state.LoadAll()) {
		switch {
		case samePath(it.Path, path):
			found = it.Action == TidyRemove
		case samePath(it.Path, d.WorktreePath):
			t.Errorf("tidy listed the conversation's own folder: %+v", it)
		}
	}
	if !found {
		t.Error("tidy did not plan to remove the clean worktree the conversation cut")
	}
}
