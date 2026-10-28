//go:build !windows

package dispatch

// root_test.go is the dead-branch bug and the root branch that answers it.
//
// The bug came in as "the dispatcher attempts to fork a dead branch", and the
// event log had the launch that did it:
//
//	DispatchFailed  soundbooth-website-nextjs  "validate json-ld"
//	git branch feature/validate-json-ld from origin/dev:
//	fatal: not a valid object name: 'origin/dev'
//
// That repo's refs/remotes/origin/HEAD still names `dev`, a branch archived and
// deleted when it moved to trunk-based development; `ls-remote` says the
// remote's HEAD is `main`. origin/HEAD is written once, at clone time, and no
// fetch has ever updated it — so the tests below are about not trusting it: the
// remote is asked, and anything local is resolved before it is used.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs a git command in dir and fails the test if it does not succeed.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %s", args, dir, out)
	}
	return strings.TrimSpace(string(out))
}

// clonedRepo builds a clone of a bare remote whose default branch is
// defaultBranch — the shape every repo in the fleet has, and the only shape in
// which origin/HEAD, ls-remote and a stale local ref can disagree.
func clonedRepo(t *testing.T, defaultBranch string) (clone, remote string) {
	t.Helper()
	dir := t.TempDir()
	remote = filepath.Join(dir, "remote.git")
	gitIn(t, dir, "init", "--quiet", "--bare", remote)
	gitIn(t, remote, "symbolic-ref", "HEAD", "refs/heads/"+defaultBranch)

	up := filepath.Join(dir, "up")
	gitIn(t, dir, "init", "--quiet", "-b", defaultBranch, up)
	gitIn(t, up, "commit", "--allow-empty", "-m", "on "+defaultBranch)
	gitIn(t, up, "remote", "add", "origin", remote)
	gitIn(t, up, "push", "--quiet", "origin", defaultBranch)

	clone = filepath.Join(dir, "clone")
	gitIn(t, dir, "clone", "--quiet", remote, clone)
	return clone, remote
}

// The reported launch, exactly: origin/HEAD names a branch that is not there.
//
// It used to be handed to `git branch` unread, and git refused the whole
// dispatch — no worktree, no session, nothing to look at but a footer notice.
// A ref that does not resolve is not a base; it is a fact about a stale cache,
// and the resolution has to carry on past it.
func TestBaseRefIgnoresDanglingOriginHEAD(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", "refs/heads/main")
	// The clone-time cache, still naming the branch this repo used to develop
	// on. Nothing local resolves it: the branch was deleted on the remote and
	// pruned here.
	gitIn(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/dev")

	got, err := baseRef(repo, DefaultRoot)
	if err != nil {
		t.Fatalf("baseRef refused a repo whose origin/HEAD is stale: %v", err)
	}
	if got != "refs/remotes/origin/main" {
		t.Errorf("baseRef = %q, want refs/remotes/origin/main — a dangling origin/HEAD must not be the base", got)
	}
	// And the dispatch it feeds actually starts.
	wt := filepath.Join(t.TempDir(), "feat")
	from, err := ensureWorktree(repo, wt, "feature/feat", DefaultRoot)
	if err != nil {
		t.Fatalf("the launch that reported this bug still fails: %v", err)
	}
	if from != "origin/main" {
		t.Errorf("cut from %q, want origin/main", from)
	}
}

// The silent half of the same defect: origin/HEAD names a branch that is still
// in this clone but retired on the remote. Nothing errors — the dispatcher just
// spends its whole session on top of a dead branch, and its PR carries the diff
// against a branch nobody merges into any more.
//
// The remote is the only thing that knows, so the remote is asked.
func TestBaseRefAsksTheRemoteNotTheLocalCache(t *testing.T) {
	repo, remote := clonedRepo(t, "develop")
	if got := gitIn(t, repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); got != "origin/develop" {
		t.Fatalf("test setup: origin/HEAD = %q, want origin/develop", got)
	}
	// The remote moves to trunk-based development. develop stays in this clone
	// — a fetch does not delete it, and it does not update origin/HEAD either.
	gitIn(t, remote, "branch", "trunk", "develop")
	gitIn(t, remote, "symbolic-ref", "HEAD", "refs/heads/trunk")

	got, err := baseRef(repo, DefaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != "refs/remotes/origin/trunk" {
		t.Errorf("baseRef = %q, want refs/remotes/origin/trunk — origin/HEAD is a clone-time cache, not an answer", got)
	}
	// The local cache is untouched: this reads the remote, it does not rewrite
	// the human's repo behind their back.
	if got := gitIn(t, repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); got != "origin/develop" {
		t.Errorf("origin/HEAD was rewritten to %q; resolving a base must not edit the repo", got)
	}
}

// A named root is used, and it is the remote's copy of it: a local branch is as
// stale as the last time the human pulled, and a dispatch starting behind
// origin opens a PR carrying commits it did not write.
func TestBaseRefNamedRootPrefersOrigin(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "branch", "release/24")
	gitIn(t, repo, "update-ref", "refs/remotes/origin/release/24", "refs/heads/release/24")

	got, err := baseRef(repo, Root("release/24"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "refs/remotes/origin/release/24" {
		t.Errorf("baseRef = %q, want the remote's copy of release/24", got)
	}
	// A branch only this clone has is still a base — that is a normal way to
	// stack work — it is just the second choice.
	gitIn(t, repo, "branch", "spike/local-only")
	got, err = baseRef(repo, Root("spike/local-only"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "refs/heads/spike/local-only" {
		t.Errorf("baseRef = %q, want refs/heads/spike/local-only", got)
	}
}

// A root that is not a branch is refused by name, and refused rather than
// quietly swapped for the default: "start it from release/24" and "start it
// from wherever" are different instructions, and a dispatch that took the
// second when it was given the first is the dead-branch bug with the blame
// moved.
func TestBaseRefNamedRootThatIsNotABranchIsRefused(t *testing.T) {
	repo := initRepo(t)
	_, err := baseRef(repo, Root("realese/24"))
	if err == nil {
		t.Fatal("a root that is not a branch was accepted")
	}
	if !strings.Contains(err.Error(), "realese/24") {
		t.Errorf("the refusal does not name the branch asked for: %v", err)
	}
}

// An empty repository has no base at all, and says so here rather than letting
// `git branch` fail from inside the launch with a message about HEAD.
func TestBaseRefEmptyRepoIsRefusedInOurWords(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "--quiet", "-b", "main")
	if _, err := baseRef(repo, DefaultRoot); err == nil {
		t.Fatal("an empty repository was offered as a base")
	}
}

// The named root is what the branch is actually cut from — asserted on the
// commits the new branch carries, not on the ref name that was passed along.
func TestEnsureWorktreeCutsFromTheNamedRoot(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "checkout", "--quiet", "-b", "release/24")
	gitIn(t, repo, "commit", "--allow-empty", "-m", "only on release/24")
	gitIn(t, repo, "checkout", "--quiet", "main")

	wt := filepath.Join(t.TempDir(), "feat")
	from, err := ensureWorktree(repo, wt, "feature/hotfix", Root("release/24"))
	if err != nil {
		t.Fatal(err)
	}
	if from != "release/24" {
		t.Errorf("recorded as cut from %q, want release/24", from)
	}
	if got := gitIn(t, wt, "log", "--format=%s", "-1"); got != "only on release/24" {
		t.Errorf("the feature branch sits on %q, not on release/24", got)
	}
}

// Re-dispatching a feature reuses the branch it left behind — that is how a
// dispatcher is sent back to work it already did — so a root named for a branch
// that already exists cannot be honoured. It is refused rather than dropped:
// otherwise the human picks a base, is agreed with, and gets another one.
func TestEnsureWorktreeRefusesANamedRootForAnExistingBranch(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "branch", "release/24")
	wt := ourWorktreePath(t, "feat")
	if _, err := ensureWorktree(repo, wt, "feature/feat", DefaultRoot); err != nil {
		t.Fatal(err)
	}
	// The worktree is there and on the branch; naming a root now is a request
	// that cannot be carried out.
	if _, err := ensureWorktree(repo, wt, "feature/feat", Root("release/24")); err == nil {
		t.Error("a root named for an existing branch was accepted and silently ignored")
	}
	// The default still reuses it, because the default asks for nothing.
	if _, err := ensureWorktree(repo, wt, "feature/feat", DefaultRoot); err != nil {
		t.Errorf("re-dispatch on the default was refused: %v", err)
	}
	// And so does a branch that exists with no worktree on it.
	wt2 := filepath.Join(t.TempDir(), "other")
	if _, err := ensureWorktree(repo, wt2, "release/24", Root("main")); err == nil {
		t.Error("a root named for an existing branch with no worktree was accepted")
	}
}

// A branch cut from an existing one records nothing, because nothing was cut:
// Root is what this dispatch started, not what it happens to be sitting on.
func TestEnsureWorktreeRecordsNoRootWhenItCutNothing(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "branch", "feature/feat")
	wt := filepath.Join(t.TempDir(), "feat")
	from, err := ensureWorktree(repo, wt, "feature/feat", DefaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	if from != "" {
		t.Errorf("cut from %q, want nothing — the branch already existed", from)
	}
}

// The offer a form makes: every branch the repo has, newest first, one row per
// name, and origin/HEAD — which is not a branch, and on the repo that reported
// this bug does not even resolve — nowhere in it.
func TestRootBranches(t *testing.T) {
	repo := initRepo(t)
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", "refs/heads/main")
	gitIn(t, repo, "branch", "spike/local-only")
	gitIn(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	// A branch on origin that this clone has no local copy of.
	gitIn(t, repo, "update-ref", "refs/remotes/origin/release/24", "refs/heads/main")

	got := RootBranches(repo)
	where := map[string]string{}
	for _, b := range got {
		if _, dup := where[b.Name]; dup {
			t.Errorf("%s is offered twice", b.Name)
		}
		where[b.Name] = b.Where
		if b.When.IsZero() {
			t.Errorf("%s has no commit date to order by", b.Name)
		}
	}
	for name, want := range map[string]string{
		"main":             "origin", // both copies exist; origin is the one it would be cut from
		"release/24":       "origin",
		"spike/local-only": "local",
	} {
		if where[name] != want {
			t.Errorf("%s offered as %q, want %q", name, where[name], want)
		}
	}
	if _, ok := where["HEAD"]; ok {
		t.Error("origin/HEAD is offered as a branch; it is a cache, and it may not resolve at all")
	}
}

// A repo can name a root branch of its own ([root_branches]), and a dispatch
// that names none on the form is cut from it rather than from origin's HEAD.
// Reported on playerpulse: work merges into `dev`, origin's HEAD says `main`,
// and a dispatch with ROOT left blank was cut from main — which on that repo
// also lacked the flake, so it never started.
func TestARepoRootBranchStandsInForTheDefault(t *testing.T) {
	clone, _ := clonedRepo(t, "main")
	gitIn(t, clone, "branch", "dev")
	gitIn(t, clone, "push", "--quiet", "origin", "dev")
	gitIn(t, clone, "fetch", "--quiet", "origin")

	got, err := baseRefFrom(clone, DefaultRoot, "dev")
	if err != nil || got != "refs/remotes/origin/dev" {
		t.Fatalf("baseRefFrom(default, repo root dev) = %q, %v; want origin's dev", got, err)
	}
	// A ROOT typed on the form still wins, for that dispatch.
	if got, _ := baseRefFrom(clone, Root("main"), "dev"); got != "refs/remotes/origin/main" {
		t.Errorf("a named root lost to the repo's: %q", got)
	}
	// A configured branch that has gone is refused by name, not swapped for
	// main without a word.
	if _, err := baseRefFrom(clone, DefaultRoot, "develop"); err == nil || !strings.Contains(err.Error(), "root_branches") {
		t.Errorf("a [root_branches] entry for a missing branch: err = %v", err)
	}
	// And the record says where it was actually cut from.
	wt := ourWorktreePath(t, "feat")
	if from, err := ensureWorktreeFrom(clone, wt, "feature/feat", DefaultRoot, "dev"); err != nil || from != "origin/dev" {
		t.Fatalf("cut from %q, %v; want origin/dev", from, err)
	}
	// Re-dispatching the same feature reuses its branch, root branch or not: the
	// repo's root only ever chooses where a NEW branch is cut.
	if _, err := ensureWorktreeFrom(clone, wt, "feature/feat", DefaultRoot, "dev"); err != nil {
		t.Errorf("re-dispatch with a repo root branch was refused: %v", err)
	}
}

// The line beside ROOT on both forms says where this dispatch will actually be
// cut: origin's default when nothing is named, the named branch when one is,
// and — with nothing on the form — the repo's own root branch, because "as
// origin sees it" would describe a launch that will not happen.
func TestTheRootHintSaysWhereTheBranchWillBeCut(t *testing.T) {
	for _, tc := range []struct {
		root     Root
		repoRoot string
		want     string
	}{
		{DefaultRoot, "", "the repo's default branch, as origin sees it"},
		{Root(" dev "), "", "cut from dev"},
		{DefaultRoot, " dev ", "dev — this repo's root branch (config)"},
		{Root("main"), "dev", "cut from main"},
		{"", "  ", "the repo's default branch, as origin sees it"},
	} {
		if got := tc.root.HintFor(tc.repoRoot); got != tc.want {
			t.Errorf("Root(%q).HintFor(%q) = %q, want %q", tc.root, tc.repoRoot, got, tc.want)
		}
	}
}

// With the remote out of reach the clone's own origin/HEAD is the next answer,
// once it is proved to point at a commit. Offline is an ordinary state for a
// laptop, and a dispatch made on a train should still start where the repo's
// work goes rather than on whatever happens to be checked out.
func TestAnUnreachableRemoteFallsBackToAnOriginHEADThatResolves(t *testing.T) {
	clone, remote := clonedRepo(t, "trunk")
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}
	if got := remoteDefaultBranch(clone); got != "" {
		t.Fatalf("a remote that is gone named %q as its default", got)
	}
	got, err := baseRef(clone, DefaultRoot)
	if err != nil {
		t.Fatalf("baseRef refused an offline clone: %v", err)
	}
	if got != "refs/remotes/origin/trunk" {
		t.Errorf("baseRef = %q, want the clone's origin/HEAD, origin/trunk", got)
	}
}

// A remote whose HEAD is detached names no default branch. That is an answer
// of nothing, not an error, and the conventional names take over.
func TestARemoteWithADetachedHeadNamesNoDefault(t *testing.T) {
	clone, remote := clonedRepo(t, "main")
	gitIn(t, remote, "update-ref", "--no-deref", "HEAD", gitIn(t, remote, "rev-parse", "main"))
	if got := remoteDefaultBranch(clone); got != "" {
		t.Errorf("a detached remote HEAD read as branch %q", got)
	}
	if got, err := baseRef(clone, DefaultRoot); err != nil || got != "refs/remotes/origin/main" {
		t.Errorf("baseRef = %q, %v; want origin/main by its conventional name", got, err)
	}
}

// A repo with no remote and no main or master still has a base: the commit
// that is checked out. It is the last resort, and the record calls it HEAD.
func TestARepoWithNoTrunkStartsFromWhatIsCheckedOut(t *testing.T) {
	repo := t.TempDir()
	gitIn(t, repo, "init", "--quiet", "-b", "trunk")
	gitIn(t, repo, "commit", "--allow-empty", "-m", "first")
	got, err := baseRef(repo, DefaultRoot)
	if err != nil || got != "HEAD" {
		t.Fatalf("baseRef = %q, %v; want HEAD", got, err)
	}
	if shortRef(got) != "HEAD" {
		t.Errorf("shortRef(HEAD) = %q", shortRef(got))
	}
}

// The forms list nothing for a folder that is not a repository, rather than an
// error to show beside an empty list.
func TestRootBranchesOfANonRepoIsEmpty(t *testing.T) {
	if got := RootBranches(t.TempDir()); got != nil {
		t.Errorf("RootBranches(not a repo) = %v", got)
	}
}
