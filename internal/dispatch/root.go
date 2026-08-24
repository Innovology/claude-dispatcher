package dispatch

// root.go answers one question: where does a dispatch's feature branch start?
//
// It used to be answered entirely from `refs/remotes/origin/HEAD`, and that ref
// is a **cache git writes once, at clone time, and never refreshes**. `git
// fetch` does not touch it. So a repo that moved its default branch — the
// trunk-based migration every one of these repos has had — keeps pointing at
// the branch it left behind, and the old answer was taken on trust and handed
// straight to `git branch` without ever being resolved. The two failures that
// produces:
//
//   - the named branch is gone on the remote and pruned locally, so origin/HEAD
//     is a dangling symref and the launch dies on `fatal: not a valid object
//     name`. That is the reported bug, verbatim from the event log:
//     `git branch feature/validate-json-ld from origin/dev: fatal: not a valid
//     object name: 'origin/dev'` — soundbooth-website-nextjs, whose origin/HEAD
//     still names `dev` while `ls-remote` says the remote's HEAD is `main`.
//   - the named branch still exists locally but is dead on the remote, so the
//     launch *succeeds* and the dispatcher spends its whole session on top of a
//     retired branch. Worse than the first, because nothing says so.
//
// So the default is read from the remote itself, and every candidate — the
// remote's answer, the local cache, the conventional names — has to resolve to
// an object before it is offered to git. A ref that does not resolve is not a
// base; it is a fact about a stale cache.
//
// The second half is the human's own answer: Root is the branch a dispatch is
// cut from, chosen on either dispatch form. The default is still the default,
// and RootDefault means "ask the remote at launch" rather than a branch name
// baked into a form, exactly as ModelDefault means "pass no flag".

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Root is the branch a dispatch's feature branch is cut from — a branch name,
// or RootDefault for the repo's default branch as the remote sees it.
type Root string

// RootDefault resolves at launch rather than naming a branch. A form cannot
// know a repo's default branch without going to the network, and a form that
// guessed would be writing the very cache this file exists to distrust.
const RootDefault Root = "default"

// DefaultRoot is what a dispatch is cut from when nothing chose.
const DefaultRoot = RootDefault

// Normalize folds the empty string and the literal word onto RootDefault, so
// "nothing chose" and "chose the default" launch identically. A record that
// predates the choice carries "" and screens read it as unchosen — the same
// distinction Model draws.
func (r Root) Normalize() Root {
	s := strings.TrimSpace(string(r))
	if s == "" || s == string(RootDefault) {
		return RootDefault
	}
	return Root(s)
}

// IsDefault reports whether r leaves the base to the remote.
func (r Root) IsDefault() bool { return r.Normalize() == RootDefault }

// Hint is the one line a form shows beside the choice.
func (r Root) Hint() string {
	if r.IsDefault() {
		return "the repo's default branch, as origin sees it"
	}
	return "cut from " + string(r.Normalize())
}

// fetchTimeout bounds every network call made on the way to a base. Refreshing
// the base is a courtesy; a slow or unreachable remote must never stall a
// launch.
const fetchTimeout = 20 * time.Second

// git runs a read-only git command with the terminal prompt disabled, so a
// repo whose remote wants credentials fails fast instead of holding the launch
// open until the timeout expires.
func gitOut(repoPath string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoPath}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// fetchOrigin refreshes the remote-tracking refs before a base is chosen. Best
// effort: offline is not a launch failure, and everything downstream verifies
// what it found rather than assuming this ran.
func fetchOrigin(repoPath string) {
	_, _ = gitOut(repoPath, fetchTimeout, "fetch", "--quiet", "origin")
}

// resolves reports whether ref names an object in this repo. This is the check
// whose absence was the bug: `symbolic-ref` happily reports the name a symref
// points at whether or not anything is there.
func resolves(repoPath, ref string) bool {
	_, err := gitOut(repoPath, fetchTimeout, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// hasOrigin reports whether the repo has an origin remote to ask at all.
func hasOrigin(repoPath string) bool {
	_, err := gitOut(repoPath, fetchTimeout, "remote", "get-url", "origin")
	return err == nil
}

// remoteDefaultBranch asks origin what its HEAD is, over the network. This is
// the only authoritative answer: origin/HEAD is a local cache of it, written at
// clone time, and no fetch has ever updated it.
//
// Returns "" when there is nothing to ask or the remote cannot be reached —
// both of which are ordinary, and neither of which is a launch failure.
func remoteDefaultBranch(repoPath string) string {
	if !hasOrigin(repoPath) {
		return ""
	}
	out, err := gitOut(repoPath, fetchTimeout, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return ""
	}
	// `ref: refs/heads/main\tHEAD` on the first line, then the sha.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref: ") {
			continue
		}
		f := strings.Fields(strings.TrimPrefix(line, "ref: "))
		if len(f) == 0 {
			continue
		}
		return strings.TrimPrefix(f[0], "refs/heads/")
	}
	return ""
}

// rootRef resolves a branch name to the ref a new branch should be cut from,
// preferring the remote's copy over the local one: a local branch is as stale
// as the last time the human pulled it, and a dispatch starting behind the
// remote opens its PR carrying commits it did not write.
//
// Returns "" when neither exists — the name is not a branch in this repo.
func rootRef(repoPath, branch string) string {
	for _, ref := range []string{"refs/remotes/origin/" + branch, "refs/heads/" + branch} {
		if resolves(repoPath, ref) {
			return ref
		}
	}
	return ""
}

// baseRef resolves the start point for a new feature branch.
//
// Letting git default the start point to the repo's HEAD — what `worktree add
// -b` does with no explicit base — silently cuts the branch from whatever the
// human left checked out, so a dispatch starts on top of an unrelated unmerged
// feature and its PR carries that work onto main. Naming origin/<default>
// rather than the local branch also keeps a stale local main out of the base.
//
// It fetches first, so every candidate below is judged against what origin has
// now rather than whenever the human last pulled.
func baseRef(repoPath string, root Root) (string, error) {
	fetchOrigin(repoPath)

	// A branch the human named. Nothing falls back: they said where to start,
	// and starting somewhere else is the defect this whole file is about.
	if !root.IsDefault() {
		name := string(root.Normalize())
		if ref := rootRef(repoPath, name); ref != "" {
			return ref, nil
		}
		return "", fmt.Errorf("%q is not a branch in this repo — origin has no %s and there is no local one; pick another root branch",
			name, name)
	}

	// The remote's own answer, first, because origin/HEAD is a cache of this
	// from clone time and cannot have been updated since.
	if name := remoteDefaultBranch(repoPath); name != "" {
		if ref := rootRef(repoPath, name); ref != "" {
			return ref, nil
		}
	}
	// The local cache — but only once it has been proved to point at something.
	// An unresolvable origin/HEAD falls through to the names below instead of
	// being handed to git as a base, which is what killed the reported launch.
	if out, err := gitOut(repoPath, fetchTimeout, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		if resolves(repoPath, out) {
			return out, nil
		}
	}
	// The conventional names, for a --single-branch clone, a repo cloned before
	// git recorded origin/HEAD, and a repo with no remote at all.
	for _, cand := range []string{
		"refs/remotes/origin/main", "refs/remotes/origin/master",
		"refs/heads/main", "refs/heads/master",
	} {
		if resolves(repoPath, cand) {
			return cand, nil
		}
	}
	// Whatever is checked out, and only if it is a commit: this is the last
	// resort for a repo that is none of the above, and an unborn HEAD is not a
	// base — it is an empty repository, said plainly here rather than as git's
	// error from inside `git branch`.
	if resolves(repoPath, "HEAD") {
		return "HEAD", nil
	}
	return "", fmt.Errorf("no branch to start from: origin names none, and this repo has no main, no master and no commit checked out")
}

// shortRef is a ref as a human reads it: "refs/remotes/origin/main" is
// origin/main, "refs/heads/main" is main. What goes on the record, so a
// dispatcher's history says what it was actually cut from — which is the
// question the dead-branch launches raised and nothing on the record answered.
func shortRef(ref string) string {
	switch {
	case strings.HasPrefix(ref, "refs/remotes/"):
		return strings.TrimPrefix(ref, "refs/remotes/")
	case strings.HasPrefix(ref, "refs/heads/"):
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	return ref
}

// RootChoice is one branch a dispatch could be cut from, as a form offers it.
type RootChoice struct {
	// Name is the branch name, without any refs/ prefix — what Root carries and
	// what rootRef resolves at launch.
	Name string
	// Where is which copy the offer is made from: "origin" for a branch the
	// remote has, "local" for one only this clone does. It is a warning as much
	// as a label — a local-only root will not be on the remote when the PR is
	// opened.
	Where string
	// When is the branch tip's commit time, which is what the list is ordered
	// by: the branch someone touched this morning is the one being stacked on.
	When time.Time
}

// RootBranches lists the branches this repo can be cut from, most recently
// committed first.
//
// It reads what is already in the repo and makes no network call: it runs while
// a human is picking on a form, and a form that stalls for a fetch on every repo
// is a form nobody uses. The launch is where the branch is fetched and verified,
// which is also where a name that has gone away since is refused out loud.
//
// refs/remotes/origin/HEAD is skipped: it is the cache this file distrusts, it
// is not a branch, and on a repo like the one that reported this bug it does not
// even resolve.
func RootBranches(repoPath string) []RootChoice {
	out, err := gitOut(repoPath, fetchTimeout, "for-each-ref",
		"--sort=-committerdate", "--format=%(refname)\t%(committerdate:unix)",
		"refs/heads", "refs/remotes/origin")
	if err != nil || out == "" {
		return nil
	}
	at := map[string]int{} // branch name → its index in list
	var list []RootChoice
	for _, line := range strings.Split(out, "\n") {
		ref, unix, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || ref == "refs/remotes/origin/HEAD" {
			continue
		}
		name, where := "", ""
		switch {
		case strings.HasPrefix(ref, "refs/remotes/origin/"):
			name, where = strings.TrimPrefix(ref, "refs/remotes/origin/"), "origin"
		case strings.HasPrefix(ref, "refs/heads/"):
			name, where = strings.TrimPrefix(ref, "refs/heads/"), "local"
		default:
			continue
		}
		if name == "" {
			continue
		}
		var when time.Time
		if secs, err := strconv.ParseInt(unix, 10, 64); err == nil {
			when = time.Unix(secs, 0)
		}
		choice := RootChoice{Name: name, Where: where, When: when}
		// One entry per branch name, and when both copies exist the entry
		// describes origin's — because that is the copy rootRef will actually
		// cut from. Describing the local one would put a date on the row that
		// is not the date of the commit the dispatch would start on.
		if i, dup := at[name]; dup {
			if where == "origin" {
				list[i] = choice
			}
			continue
		}
		at[name] = len(list)
		list = append(list, choice)
	}
	return list
}
