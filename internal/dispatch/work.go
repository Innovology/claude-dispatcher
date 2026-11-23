package dispatch

// work.go is how a conversation turns into work: the point of a branchless
// dispatch is to start talking about a product and then, when the talk arrives
// somewhere, become either a dispatcher on a branch — this same session, its context intact,
// on a branch of its own — or an orchestrator, sending the pieces out as
// dispatchers of their own. A session cannot move its own launch directory,
// and claude's EnterWorktree only enters a worktree of the repository it was
// launched in, which a conversation's folder is not. So both moves are verbs
// of this binary, run from the session's own shell (fleetcmd's `worktree` and
// `dispatch`), and both leave the cockpit knowing what happened.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// TakeBranch moves a branchless dispatcher onto a branch in r and returns the
// checkout to work in. name is either a branch that already exists — entered
// as it is, in the worktree it already has, or in one cut for it — or the name
// of new work, cut as Launch cuts a feature: the repo's prefix and the name's
// slug, from root, where the repo's layout puts a dispatch's worktree.
//
// From then on the record is branch work. Branch, repo and base are set, so the
// tracker finds its pull request, provenance counts its commits from here, and
// done means live again. WorktreePath is left alone, because the transcript is
// keyed by where the session started and Resume reopens it there.
//
// The canonical checkout is never entered: it is the one every conversation
// reads and the human works in, so a branch already checked out there is
// refused with the way round it — cut a branch of its own from that one. Nor is
// a checkout or branch a live dispatcher has (liveIn), whichever name it is
// asked for by. CheckoutCut is set only where this call ran git worktree add,
// because it is what lets kill and tidy remove the folder.
func TakeBranch(d *state.Dispatch, r repos.Repo, name string, root Root) (string, error) {
	if d == nil {
		return "", fmt.Errorf("no dispatch record")
	}
	if err := func() error {
		release := state.Lock()
		defer release()
		return canTake(stored(d))
	}(); err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" || Slugify(name) == "" {
		return "", fmt.Errorf("name the branch, or the work a branch should be cut for")
	}
	root = root.Normalize()

	// Where the work would go, worked out before anything is made, so that a
	// checkout or branch a live dispatcher has is refused before it is entered.
	var branch, path, entered string
	existing, err := branchExists(r.Path, name)
	if err != nil {
		return "", err
	}
	if existing {
		if !root.IsDefault() {
			return "", reusedRootErr(name, root)
		}
		branch = name
		if isTrunk(r, name) {
			return "", fmt.Errorf("%s is where %s's work merges, and a dispatcher's work goes on a branch of its own — cut one from it: --root %s and a new name",
				name, r.Name, name)
		}
		entered = worktreeOn(r.Path, name)
		if entered != "" && samePath(entered, r.Path) {
			return "", fmt.Errorf("%s is checked out in %s, the checkout this conversation reads — cut a branch of your own from it: --root %s and a new name",
				name, entered, name)
		}
		path = entered
	} else {
		branch = branchFor(r, Slugify(name))
	}
	if path == "" {
		path = worktreePath(r, Slugify(name))
	}
	if live := liveIn(r.Name, branch, path, d.ID); live != nil {
		return "", heldErr(live, branch)
	}

	// Cut only when this call ran git worktree add: a checkout that was
	// already there — a branch's own, or one an earlier dispatch of this name
	// left behind — is entered, and kill and tidy leave it where it was.
	var cutFrom string
	cut := false
	switch {
	case entered != "":
	case existing:
		if err := addWorktree(r.Path, path, name); err != nil {
			return "", err
		}
		cut = true
	default:
		_, statErr := os.Stat(path)
		fresh := os.IsNotExist(statErr)
		from, err := ensureWorktreeFrom(r.Path, path, branch, root, r.RootBranch)
		if err != nil {
			return "", err
		}
		cutFrom, cut = from, fresh
	}
	// A human who opens claude in it should not meet the trust dialog either.
	InheritTrustFor(d.ConfigDir, r.Path, path)

	release := state.Lock()
	defer release()
	cur := stored(d)
	// Asked again under the lock that the write is made under: between the
	// first look and this one, a second `worktree` for the same record may
	// have taken a branch, or the session may have ended.
	if err := canTake(cur); err != nil {
		if cut {
			CleanupWorktree(r.Path, path)
		}
		return "", err
	}
	cur.RepoName, cur.RepoPath = r.Name, r.Path
	cur.Branch, cur.Root = branch, cutFrom
	cur.BaseSHA = headSHA(path)
	cur.Checkout, cur.CheckoutCut = path, cut
	if err := state.Save(cur); err != nil {
		return "", err
	}
	*d = *cur
	return path, nil
}

// stored is d as the store has it now, or d itself when it is not stored. The
// caller holds the hook lock.
func stored(d *state.Dispatch) *state.Dispatch {
	for _, rec := range state.LoadAll() {
		if rec.ID == d.ID {
			return rec
		}
	}
	return d
}

// canTake says why d cannot take a branch, or nil. Only a conversation that is
// still going does: a finished one is resumed first, so that whatever runs
// `worktree --id` cannot turn a record nobody is working under into a branch's.
func canTake(d *state.Dispatch) error {
	switch {
	case !d.Branchless:
		return fmt.Errorf("%q was dispatched on %s; only a dispatch that started with no branch takes one", d.Feature, d.Branch)
	case d.Branch != "":
		return fmt.Errorf("%q is already working on %s in %s — send further work out with dispatch", d.Feature, d.Branch, d.Checkout)
	case !unfinished(d.Status):
		return fmt.Errorf("%q has finished (%s) — resume it, and take the branch from its session", d.Feature, d.Status)
	}
	return nil
}

// isTrunk reports whether name is a branch r's work merges into: its own root
// branch (config's [root_branches]), the default origin names now, the one
// origin/HEAD still remembers, or main or master. Work taken on one of them
// would be committed straight to it, and the tracker, which prefers a merged
// pull request for a branch, would hand the record the last merge into it and
// call the work live at its first stop.
func isTrunk(r repos.Repo, name string) bool {
	if name == "main" || name == "master" || (r.RootBranch != "" && name == r.RootBranch) {
		return true
	}
	if name == remoteDefaultBranch(r.Path) {
		return true
	}
	cached, err := gitOut(r.Path, fetchTimeout, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	return err == nil && strings.TrimPrefix(cached, "origin/") == name
}

// branchExists reports whether name is a branch to go into: one of this
// clone's own, or one origin has. A remote-tracking ref alone is not proof of
// the second — a plain fetch never deletes one, so a branch origin retired
// months ago is still there (the quiet failure ADR 0010 is about) — so origin
// is asked, and a branch it no longer has is refused by name rather than
// entered as live work or quietly read as the name of new work. Unreachable,
// the ref is taken at its word, as it was: offline is not a refusal.
func branchExists(repoPath, name string) (bool, error) {
	if resolves(repoPath, "refs/heads/"+name) {
		return true, nil
	}
	if !resolves(repoPath, "refs/remotes/origin/"+name) {
		return false, nil
	}
	out, err := gitOut(repoPath, fetchTimeout, "ls-remote", "--heads", "origin", "refs/heads/"+name)
	if err == nil && out == "" {
		return false, fmt.Errorf("origin no longer has %s — the origin/%s here is what a fetch left behind; name another branch, or the work to cut one for", name, name)
	}
	return true, nil
}

// worktreeOn is the checkout git's registry has on branch, or "".
func worktreeOn(repoPath, branch string) string {
	out, err := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return ""
	}
	path := ""
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return path
		}
	}
	return ""
}

// addWorktree checks out an existing branch in a new worktree at path. A
// branch only origin has is left to git, which makes a local branch tracking
// it — the branch is somebody else's, and a plain push should go back to it.
func addWorktree(repoPath, path, branch string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists — it may be one of your own; name the work differently", path)
	}
	_ = exec.Command("git", "-C", repoPath, "worktree", "prune").Run()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if out, err := exec.Command("git", "-C", repoPath, "worktree", "add", path, branch).CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add %s: %s", branch, strings.TrimSpace(string(out)))
	}
	return nil
}
