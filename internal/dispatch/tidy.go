package dispatch

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"claude-dispatcher/internal/state"
)

// Tidying is how a finished dispatcher's checkout leaves the disk.
//
// Until it existed only the cockpit's kill removed a worktree, and a dispatcher
// that ended any other way — its session ending, the tracker marking it live, a
// ghost sweep — left its checkout and every dependency it installed exactly
// where they were, for good. Measured on the reporting machine: 99 worktrees
// behind finished records, about 100 GB between them, nearly all of it
// node_modules, and not one of them ever going to be worked in again without a
// resume that rebuilds the checkout anyway (see Resume).
//
// So a tidy removes the worktree of every finished dispatcher it can prove is
// safe to remove, and takes nothing it cannot:
//
//   - Only worktrees a dispatch record names. The worktrees folder also holds
//     checkouts no record owns — a full clone, a worktree a session made for
//     itself — and those are somebody's, not ours.
//   - Only when every record naming it is finished, unparked and has no
//     session left. Finished is not enough on its own: `done` is the tracker's
//     word, written when the deploy succeeds, whatever the session is doing,
//     and a session still open has its cwd in there. A parked one is a
//     promise to come back.
//   - Only a clean worktree is removed, by git, without --force. Ignored files
//     go with it — that is what node_modules, .next and dist are — and the
//     branch stays, so nothing committed is lost and Resume can put the
//     checkout back. One with uncommitted work keeps it and loses only its
//     ignored node_modules folders, which an install rebuilds.
//   - A detached HEAD whose commit no ref contains is kept: the worktree's own
//     HEAD is the only thing holding that commit, and removing it would leave
//     the work to the garbage collector.
//
// PlanTidy says what a tidy would do and writes nothing; Tidy does it.

// TidyAction is what a tidy does, or would do, with one worktree.
type TidyAction string

const (
	// TidyRemove removes the worktree: clean, and nobody is in it.
	TidyRemove TidyAction = "remove"
	// TidyStrip keeps the worktree for its uncommitted work and removes only
	// its ignored node_modules folders.
	TidyStrip TidyAction = "strip"
	// TidyKeep leaves it exactly as it is; Reason says why.
	TidyKeep TidyAction = "keep"
)

// TidyItem is one worktree and what a tidy does with it.
type TidyItem struct {
	Path    string
	Repo    string // the repo it is a worktree of, by name
	Feature string
	// ID is the newest record naming this worktree; Records are all of them.
	// There is more than one when a feature was dispatched again after it
	// finished.
	ID      string
	Records []string
	Action  TidyAction
	// Reason is why it is stripped or kept, in words a human can act on.
	Reason string
	// NodeModules are the ignored node_modules folders a strip removes,
	// relative to Path.
	NodeModules []string
	// Err is set by Tidy when the act itself failed: git's own words.
	Err string
}

// ownerOf is a worktree and every record that names it.
type ownerOf struct {
	path string
	info os.FileInfo
	recs []*state.Dispatch
}

// worktreeOwners groups records by the worktree on disk they name. Paths are
// compared as files, not strings: a repo's name decides its folder, and on a
// case-insensitive disk "Aura" and "aura" are one folder named two ways by two
// records — which the reporting machine has.
func worktreeOwners(ds []*state.Dispatch) []*ownerOf {
	var out []*ownerOf
	for _, d := range ds {
		if d.WorktreePath == "" {
			continue
		}
		info, err := os.Stat(d.WorktreePath)
		if err != nil || !info.IsDir() {
			continue // already gone, which is tidy
		}
		var into *ownerOf
		for _, o := range out {
			if os.SameFile(o.info, info) {
				into = o
				break
			}
		}
		if into == nil {
			into = &ownerOf{path: d.WorktreePath, info: info}
			out = append(out, into)
		}
		into.recs = append(into.recs, d)
	}
	return out
}

// PlanTidy reads what a tidy of these records would do. It writes nothing; it
// asks git and the supervisor, so it costs a few subprocesses per worktree.
func PlanTidy(ds []*state.Dispatch) []TidyItem {
	ready := supervisorReady()
	var items []TidyItem
	for _, o := range worktreeOwners(ds) {
		items = append(items, planOne(o, ready))
	}
	return items
}

func planOne(o *ownerOf, ready bool) TidyItem {
	newest := o.recs[0]
	for _, d := range o.recs {
		if d.CreatedAt.After(newest.CreatedAt) {
			newest = d
		}
	}
	it := TidyItem{Path: o.path, Repo: newest.RepoName, Feature: newest.Feature, ID: newest.ID, Action: TidyKeep}
	for _, d := range o.recs {
		it.Records = append(it.Records, d.ID)
	}
	if why := inUse(o.recs, ready); why != "" {
		it.Reason = why
		return it
	}
	repo := newest.RepoPath
	if !linkedWorktree(o.path) {
		it.Reason = "not a git worktree any more"
		return it
	}
	if why := unreachableHead(o.path); why != "" {
		it.Reason = why
		return it
	}
	dirty, err := worktreeDirty(o.path)
	if err != nil {
		it.Reason = "git could not read it: " + err.Error()
		return it
	}
	if !dirty {
		if repo == "" {
			it.Reason = "its record does not name the repo it came from"
			return it
		}
		it.Action = TidyRemove
		return it
	}
	it.NodeModules = ignoredNodeModules(o.path)
	if len(it.NodeModules) == 0 {
		it.Reason = "uncommitted changes"
		return it
	}
	it.Action = TidyStrip
	it.Reason = "uncommitted changes"
	return it
}

// inUse says why a worktree's records still claim it, or "" when none does.
func inUse(recs []*state.Dispatch, ready bool) string {
	for _, d := range recs {
		switch {
		case !d.Finished():
			return "still " + string(d.Status)
		case d.Parked():
			return "parked"
		case d.TmuxSession == "":
		case !ready:
			// Nothing to ask whether its session is still open, and an open one
			// is somebody working in this folder: not knowing is not "no".
			return "cannot ask the supervisor whether its session is open"
		case sessionAlive(d.TmuxSession):
			return "its session is still open"
		}
	}
	return ""
}

// linkedWorktree reports whether path is the top of a linked worktree — not a
// repo's main checkout, and not a folder inside some other checkout, where
// every git question would be answered about the wrong tree.
func linkedWorktree(path string) bool {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel", "--git-dir", "--git-common-dir").Output()
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 {
		return false
	}
	top, err1 := os.Stat(lines[0])
	here, err2 := os.Stat(path)
	if err1 != nil || err2 != nil || !os.SameFile(top, here) {
		return false
	}
	abs := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(path, p)
		}
		return filepath.Clean(p)
	}
	return abs(lines[1]) != abs(lines[2])
}

// unreachableHead says why a detached HEAD must keep its worktree: when no
// branch, tag or remote-tracking ref contains it, the worktree's HEAD is the
// only thing holding those commits.
func unreachableHead(path string) string {
	if exec.Command("git", "-C", path, "symbolic-ref", "-q", "HEAD").Run() == nil {
		return "" // on a branch, which outlives the worktree
	}
	out, err := exec.Command("git", "-C", path, "for-each-ref", "--count=1", "--contains", "HEAD", "--format=%(refname)").Output()
	if err != nil {
		return "git could not say whether its detached HEAD is on any branch"
	}
	if strings.TrimSpace(string(out)) == "" {
		return "detached HEAD with commits no branch holds"
	}
	return ""
}

// worktreeDirty is the question `git worktree remove` asks before it will
// remove a worktree: anything modified, staged or untracked. Ignored files do
// not count, which is why a clean worktree takes its node_modules with it.
func worktreeDirty(path string) (bool, error) {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain", "--ignore-submodules=none").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// ignoredNodeModules finds the node_modules folders under a worktree that git
// ignores and tracks nothing in — every package's, not just the root's, since a
// monorepo installs one per workspace. It does not descend into one it found,
// nor into .git.
func ignoredNodeModules(path string) []string {
	var found []string
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable corners are not ours to delete anyway
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git":
			return filepath.SkipDir
		case "node_modules":
			rel, err := filepath.Rel(path, p)
			if err == nil && ignoredAndUntracked(path, rel) {
				found = append(found, rel)
			}
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

func ignoredAndUntracked(worktree, rel string) bool {
	rel = filepath.ToSlash(rel)
	if exec.Command("git", "-C", worktree, "check-ignore", "-q", rel+"/").Run() != nil {
		return false
	}
	out, err := exec.Command("git", "-C", worktree, "ls-files", "--", rel).Output()
	return err == nil && strings.TrimSpace(string(out)) == ""
}

// Tidy carries out a plan. Every item is checked again against the records as
// they are now, because a plan can sit on a confirm bar while a dispatch of the
// same feature starts in that very folder; anything that no longer plans the
// same way is kept, with the new reason.
//
// Each removal and strip is written to the event log, so a folder that went
// missing can be traced to the tidy that took it.
func Tidy(plan []TidyItem) []TidyItem {
	owners := worktreeOwners(state.LoadAll())
	ready := supervisorReady()
	out := make([]TidyItem, 0, len(plan))
	for _, want := range plan {
		if want.Action == TidyKeep {
			out = append(out, want)
			continue
		}
		o := findOwner(owners, want.Path)
		if o == nil {
			continue // gone since the plan: nothing left to tidy
		}
		it := planOne(o, ready)
		if it.Action != want.Action {
			if it.Action != TidyKeep {
				it.Action, it.Reason = TidyKeep, "changed since it was planned"
			}
			out = append(out, it)
			continue
		}
		switch it.Action {
		case TidyRemove:
			repo := o.recs[0].RepoPath
			for _, d := range o.recs {
				if d.RepoPath != "" {
					repo = d.RepoPath
				}
			}
			if msg, err := exec.Command("git", "-C", repo, "worktree", "remove", o.path).CombinedOutput(); err != nil {
				it.Action, it.Err = TidyKeep, strings.TrimSpace(string(msg))
				it.Reason = it.Err
			} else {
				_ = exec.Command("git", "-C", repo, "worktree", "prune").Run()
				auditTidy(it, "worktree removed")
			}
		case TidyStrip:
			var failed []string
			for _, rel := range it.NodeModules {
				if err := os.RemoveAll(filepath.Join(o.path, rel)); err != nil {
					failed = append(failed, err.Error())
				}
			}
			if len(failed) > 0 {
				it.Err = strings.Join(failed, "; ")
			}
			auditTidy(it, "node_modules removed ("+strings.Join(it.NodeModules, ", ")+"); worktree kept: "+it.Reason)
		}
		out = append(out, it)
	}
	return out
}

func findOwner(owners []*ownerOf, path string) *ownerOf {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	for _, o := range owners {
		if os.SameFile(o.info, info) {
			return o
		}
	}
	return nil
}

func auditTidy(it TidyItem, what string) {
	state.AppendEvent(state.Event{
		Event: state.EventTidied, DispatcherID: it.ID, Cwd: it.Path,
		Feature: it.Feature, Repo: it.Repo, Reason: what,
	})
}
