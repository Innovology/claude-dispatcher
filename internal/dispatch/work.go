package dispatch

// work.go is how a conversation turns into work: the point of a branchless
// dispatch is to start talking about a product and then, when the talk arrives
// somewhere, become either a worker — this same session, its context intact,
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
// From then on the record is a worker's. Branch, repo and base are set, so the
// tracker finds its pull request, provenance counts its commits from here, and
// done means live again. WorktreePath is left alone, because the transcript is
// keyed by where the session started and Resume reopens it there.
//
// The canonical checkout is never entered: it is the one every conversation
// reads and the human works in, so a branch already checked out there is
// refused with the way round it — cut a branch of its own from that one.
func TakeBranch(d *state.Dispatch, r repos.Repo, name string, root Root) (string, error) {
	switch {
	case d == nil:
		return "", fmt.Errorf("no dispatch record")
	case !d.Branchless:
		return "", fmt.Errorf("%q was dispatched on %s; only a dispatch that started with no branch takes one", d.Feature, d.Branch)
	case d.Branch != "":
		return "", fmt.Errorf("%q is already working on %s in %s — send further work out with dispatch", d.Feature, d.Branch, d.Checkout)
	}
	name = strings.TrimSpace(name)
	if name == "" || Slugify(name) == "" {
		return "", fmt.Errorf("name the branch, or the work a branch should be cut for")
	}
	root = root.Normalize()

	var branch, path, cutFrom string
	cut := true
	if branchExists(r.Path, name) {
		if !root.IsDefault() {
			return "", reusedRootErr(name, root)
		}
		branch = name
		if at := worktreeOn(r.Path, name); at != "" {
			if samePath(at, r.Path) {
				return "", fmt.Errorf("%s is checked out in %s, the checkout this conversation reads — cut a branch of your own from it: --root %s and a new name",
					name, at, name)
			}
			path, cut = at, false
		} else {
			path = worktreePath(r, Slugify(name))
			if err := addWorktree(r.Path, path, name); err != nil {
				return "", err
			}
		}
	} else {
		branch = branchFor(r, Slugify(name))
		path = worktreePath(r, Slugify(name))
		from, err := ensureWorktreeFrom(r.Path, path, branch, root, r.RootBranch)
		if err != nil {
			return "", err
		}
		cutFrom = from
	}
	// A human who opens claude in it should not meet the trust dialog either.
	InheritTrustFor(d.ConfigDir, r.Path, path)

	release := state.Lock()
	defer release()
	cur := d
	for _, rec := range state.LoadAll() {
		if rec.ID == d.ID {
			cur = rec
			break
		}
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

// branchExists reports whether name is a branch here or on origin.
func branchExists(repoPath, name string) bool {
	for _, ref := range []string{"refs/heads/" + name, "refs/remotes/origin/" + name} {
		if exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--quiet", ref).Run() == nil {
			return true
		}
	}
	return false
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
