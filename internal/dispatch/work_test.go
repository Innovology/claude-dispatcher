//go:build !windows

package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
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

// aliveAll makes every session look alive and busy, so every unfinished record
// is a live dispatcher.
func aliveAll(t *testing.T) {
	t.Helper()
	prevAlive, prevIdle := sessionAlive, sessionIdle
	sessionAlive = func(supervisor.Session) bool { return true }
	sessionIdle = func(supervisor.Session) (bool, bool) { return false, true }
	t.Cleanup(func() { sessionAlive, sessionIdle = prevAlive, prevIdle })
}

// A conversation never goes into a checkout a live dispatcher is working in,
// by the work's name or by the branch's, and a dispatch never goes into one a
// conversation took: two sessions in one checkout is what a worktree per
// dispatch exists to prevent, and kill on the conversation removed the other
// dispatcher's worktree from under it.
func TestALiveCheckoutIsNeverShared(t *testing.T) {
	d, all := conversation(t)
	app := all[1]
	live, err := Launch(app, "pricing fix", "go", ModeAuto, DefaultModel, DefaultRoot, false, ownAccount)
	if err != nil {
		t.Fatal(err)
	}
	aliveAll(t)
	for _, name := range []string{"pricing fix", live.Branch} {
		if _, err := TakeBranch(d, app, name, DefaultRoot); err == nil || !strings.Contains(err.Error(), `"pricing fix"`) {
			t.Errorf("TakeBranch(%q) into a live dispatcher's checkout: %v", name, err)
		}
	}
	if d.Branch != "" || d.Checkout != "" {
		t.Errorf("a refused take changed the record: branch %q checkout %q", d.Branch, d.Checkout)
	}

	// The other way round: the conversation has the work, and a dispatch of
	// the same name is refused, though their slugs differ.
	path, err := TakeBranch(d, app, "spike", DefaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Launch(app, "spike", "go", ModeAuto, DefaultModel, DefaultRoot, false, ownAccount); err == nil ||
		!strings.Contains(err.Error(), `"talk"`) {
		t.Errorf("a dispatch went into the checkout a live conversation took (%s): %v", path, err)
	}
}

// A checkout that was already there when the conversation took it — one a
// finished dispatch of the same name left behind — is entered and not cut, so
// killing the conversation leaves it; and resuming that finished dispatch while
// the conversation works in it is refused.
func TestACheckoutAlreadyThereIsEnteredNotCut(t *testing.T) {
	d, all := conversation(t)
	app := all[1]
	old, err := Launch(app, "old work", "go", ModeAuto, DefaultModel, DefaultRoot, false, ownAccount)
	if err != nil {
		t.Fatal(err)
	}
	old.Stop(state.StatusExited, "session ended", old.CreatedAt)
	if err := state.Save(old); err != nil {
		t.Fatal(err)
	}
	path, err := TakeBranch(d, app, "old work", DefaultRoot)
	if err != nil {
		t.Fatalf("TakeBranch into a finished dispatch's worktree: %v", err)
	}
	if !samePath(path, old.WorktreePath) || d.CheckoutCut || d.RemovableCheckout() != "" {
		t.Errorf("entered %q (want %q), cut %v, removable %q — this call cut nothing", path, old.WorktreePath, d.CheckoutCut, d.RemovableCheckout())
	}
	aliveAll(t)
	if _, _, err := Resume(old, ""); err == nil || !strings.Contains(err.Error(), "live in the same worktree") {
		t.Errorf("resumed a dispatch into the checkout a live conversation works in: %v", err)
	}
}

// A conversation does not take the branch its repo's work merges into, even
// with the canonical checkout on another branch: not main, not the repo's own
// root branch, not the default origin names, nor the one origin/HEAD still
// remembers.
func TestAConversationDoesNotTakeTheTrunk(t *testing.T) {
	d, all := conversation(t)
	app, pulse := all[1], all[0]
	gitIn(t, app.Path, "checkout", "-q", "-b", "elsewhere")
	if _, err := TakeBranch(d, app, "main", DefaultRoot); err == nil || !strings.Contains(err.Error(), "merges") {
		t.Errorf("took main with the canonical checkout elsewhere: %v", err)
	}
	gitIn(t, pulse.Path, "branch", "dev") // playerpulse's [root_branches] entry
	if _, err := TakeBranch(d, pulse, "dev", DefaultRoot); err == nil || !strings.Contains(err.Error(), "merges") {
		t.Errorf("took the repo's own root branch: %v", err)
	}

	clone, _ := clonedRepo(t, "trunk")
	r := repos.Repo{Name: "svc", Path: clone}
	gitIn(t, clone, "checkout", "-q", "-b", "elsewhere")
	if _, err := TakeBranch(d, r, "trunk", DefaultRoot); err == nil || !strings.Contains(err.Error(), "--root trunk") {
		t.Errorf("took the branch origin names its default: %v", err)
	}
	// origin moved its default; the clone's cache still names the old one.
	gitIn(t, clone, "branch", "old-trunk")
	gitIn(t, clone, "push", "-q", "origin", "old-trunk")
	gitIn(t, clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/old-trunk")
	if _, err := TakeBranch(d, r, "old-trunk", DefaultRoot); err == nil || !strings.Contains(err.Error(), "merges") {
		t.Errorf("took the branch origin/HEAD remembers: %v", err)
	}
	if d.Branch != "" {
		t.Errorf("a refused take left the record on %q", d.Branch)
	}
	if _, err := TakeBranch(d, r, "trunk work", DefaultRoot); err != nil {
		t.Errorf("new work in the same repo: %v", err)
	}
}

// Whether a record may take a branch is read from the store under the lock,
// not from the copy the caller holds: a conversation that has finished is
// resumed first, and one a second `worktree` already moved onto a branch does
// not take another.
func TestTakingABranchReadsTheRecordAsStored(t *testing.T) {
	d, all := conversation(t)
	app := all[1]
	stale := *d
	if _, err := TakeBranch(d, app, "first", DefaultRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := TakeBranch(&stale, app, "second", DefaultRoot); err == nil || !strings.Contains(err.Error(), "already working") {
		t.Errorf("a stale copy with no branch took a second one: %v", err)
	}

	done, all := conversation(t)
	done.Stop(state.StatusExited, "session ended", done.CreatedAt)
	if err := state.Save(done); err != nil {
		t.Fatal(err)
	}
	live := *done
	live.Status = state.StatusWorking // what a caller holding an old copy believes
	if _, err := TakeBranch(&live, all[1], "late", DefaultRoot); err == nil || !strings.Contains(err.Error(), "finished") {
		t.Errorf("a finished conversation took a branch: %v", err)
	}
	if _, err := os.Stat(worktreePath(all[1], "late")); err == nil {
		t.Error("a refused take left a worktree behind")
	}
}

// A branch this clone knows only by its remote-tracking ref is entered when
// origin still has it, and refused by name when origin retired it: a fetch
// never deletes the ref, so it alone would have the conversation working on a
// branch that is gone.
func TestABranchOriginRetiredIsNotEntered(t *testing.T) {
	d, _ := conversation(t)
	clone, remote := clonedRepo(t, "main")
	r := repos.Repo{Name: "svc", Path: clone}
	for _, b := range []string{"spike/old", "spike/live"} {
		gitIn(t, remote, "branch", b, "main")
	}
	gitIn(t, clone, "fetch", "-q", "origin")
	gitIn(t, remote, "branch", "-D", "spike/old")

	if _, err := TakeBranch(d, r, "spike/old", DefaultRoot); err == nil || !strings.Contains(err.Error(), "no longer has spike/old") {
		t.Errorf("entered a branch origin retired: %v", err)
	}
	path, err := TakeBranch(d, r, "spike/live", DefaultRoot)
	if err != nil {
		t.Fatalf("a branch origin still has: %v", err)
	}
	if worktreeBranch(t, path) != "spike/live" || !d.CheckoutCut {
		t.Errorf("checkout %q on %q, cut %v", path, worktreeBranch(t, path), d.CheckoutCut)
	}
}

// Killed and reopened, a conversation that cut a checkout gets it back, on the
// branch it took, as any dispatch's worktree is rebuilt; one it went into
// without cutting is not put back in somebody else's folder.
func TestResumingAConversationRebuildsTheCheckoutItCut(t *testing.T) {
	d, all := conversation(t)
	app := all[1]
	path, err := TakeBranch(d, app, "spike", DefaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, path, "commit", "--allow-empty", "-q", "-m", "work")
	head := gitIn(t, path, "rev-parse", "HEAD")
	d.Stop(state.StatusExited, "killed", d.CreatedAt)
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}
	if !CleanupWorktree(app.Path, path) {
		t.Fatal("the clean worktree was not removed")
	}
	if _, _, err := Resume(d, ""); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if worktreeBranch(t, path) != d.Branch || gitIn(t, path, "rev-parse", "HEAD") != head {
		t.Errorf("rebuilt %s on %q at another commit; want %s with its work", path, worktreeBranch(t, path), d.Branch)
	}

	entered := &state.Dispatch{Branchless: true, Branch: "theirs", RepoPath: app.Path, Checkout: filepath.Join(t.TempDir(), "gone")}
	if err := rebuildTaken(entered); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entered.Checkout); err == nil {
		t.Error("a checkout the conversation went into was put back")
	}
}

// A checkout that cannot be rebuilt is said, not resumed past.
func TestACheckoutThatCannotBeRebuiltIsSaid(t *testing.T) {
	d := &state.Dispatch{Branchless: true, CheckoutCut: true, Branch: "feature/x",
		RepoPath: t.TempDir(), Checkout: filepath.Join(t.TempDir(), "gone")}
	if err := rebuildTaken(d); err == nil || !strings.Contains(err.Error(), "rebuild") {
		t.Errorf("rebuilding in a folder that is no repo: %v", err)
	}
}
