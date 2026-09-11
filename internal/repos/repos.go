// Package repos discovers the git repositories the cockpit operates across.
// Repos are the organising primitive; each dispatch then gets its own git
// worktree of its repo so concurrent dispatches never share a checkout.
//
// A repository is not a directory with a `.git` in it — that is a *checkout*,
// and one repository can have many. Identity comes from the git directory every
// checkout of a repository shares (its common dir), and the name comes from the
// origin remote, because neither a folder name nor a branch name is the repo.
// See docs/adr/0012-a-repository-is-its-common-dir.md.
package repos

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"claude-dispatcher/internal/config"
)

// Worktree is one checkout of a repository.
type Worktree struct {
	Path   string // absolute path of the working directory
	Branch string // the branch checked out there, "" when detached
	Main   bool   // the repository's main working tree (a clone's own directory)
}

type Repo struct {
	Name    string
	Path    string
	Product string
	// GitDir is the common git directory every checkout of this repo shares:
	// `<clone>/.git` for an ordinary clone, `<project>/.bare` for the bare
	// layout. It is this repo's identity — two checkouts naming it are one repo.
	GitDir string
	// Worktrees is every checkout git knows about, main worktree first. Read
	// from git's own registry rather than from the directory scan, so a worktree
	// in a hidden `.worktrees/` folder or outside every scan root is still here.
	Worktrees []Worktree
	// Socket is the supervisor server this repo's sessions live on, and Env is
	// the command they are launched under so they see this repo's own binaries.
	// Both default to something sensible and are overridable; see env.go.
	Socket string
	Env    string
	// Pinned reports that Path was named by the human in `[checkouts]` rather
	// than chosen. Screens say which, because the two answer different
	// questions: an automatic choice is a guess worth correcting, and a pin is
	// a decision worth seeing before it is changed.
	Pinned bool
}

const maxDepth = 3

var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
}

// discovery collects the checkouts found for one common dir.
type discovery struct {
	common string
	found  []string
	seen   map[string]bool
}

// Discover scans each configured root up to maxDepth for directories
// containing a .git entry, then groups those checkouts into repositories.
//
// The scan itself is unchanged: a directory holding a `.git` is a checkout and
// is not descended into. What is added is the question asked of each one — which
// repository does this belong to — so that the sixty-odd worktrees of one repo
// are one row rather than sixty. A checkout whose git metadata cannot be read
// falls back to being a repository of its own, named for its folder, which is
// exactly what every checkout used to be.
func Discover(cfg *config.Config) []Repo {
	groups := map[string]*discovery{}
	var order []string

	for _, root := range cfg.ExpandedRoots() {
		root := filepath.Clean(root)
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || skipDirs[name]) {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(path, ".git")); statErr == nil {
				common := commonDir(path)
				g := groups[common]
				if g == nil {
					g = &discovery{common: common, seen: map[string]bool{}}
					groups[common] = g
					order = append(order, common)
				}
				if !g.seen[path] {
					g.seen[path] = true
					g.found = append(g.found, path)
				}
				return filepath.SkipDir
			}
			// The depth budget bounds the walk, but it must not cut a directory
			// that demonstrably holds a repository: the bare layout puts the
			// repo in `<project>/.bare` and the checkouts one level below, so
			// the project directory itself carries no `.git` to be found by and
			// was skipped with all its worktrees inside it.
			if tooDeep(root, path) && !isRepoContainer(path) {
				return filepath.SkipDir
			}
			return nil
		})
	}

	out := make([]Repo, 0, len(order))
	for _, common := range order {
		g := groups[common]
		wts := worktrees(common)
		r := Repo{
			Name:      repoName(common),
			Path:      canonical(wts, g.found),
			GitDir:    common,
			Worktrees: wts,
		}
		if p, ok := pinnedCheckout(cfg.Checkouts[r.Name], wts); ok {
			r.Path, r.Pinned = p, true
		}
		r.Product = cfg.ProductFor(r.Name)
		r.Socket = socketFor(cfg, r.Name)
		// Read from the checkout the repo actually works in, which the pin above
		// may just have changed.
		r.Env = envFor(cfg, r.Name, r.Path)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func tooDeep(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	return strings.Count(rel, string(filepath.Separator)) >= maxDepth-1
}

// commonDir resolves the git directory a checkout shares with every other
// checkout of the same repository.
//
// A clone's `.git` is a directory and is itself that shared dir. A linked
// worktree's `.git` is a FILE naming the worktree's own git dir, which carries
// a `commondir` file pointing back at the shared one (usually "../.."). Both
// are plain files, so this costs three small reads and no git process — which
// matters, because a discovery runs on every load and a portfolio like this has
// a hundred checkouts in it.
//
// Anything unreadable resolves to the checkout's own `.git`, which makes it a
// repository of one — the behaviour before repositories were a concept here.
func commonDir(checkout string) string {
	gitPath := filepath.Join(checkout, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return gitPath
	}
	if info.IsDir() {
		return gitPath
	}
	b, err := os.ReadFile(gitPath)
	if err != nil {
		return gitPath
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return gitPath
	}
	wtDir := strings.TrimSpace(rest)
	if !filepath.IsAbs(wtDir) {
		wtDir = filepath.Join(checkout, wtDir)
	}
	cb, err := os.ReadFile(filepath.Join(wtDir, "commondir"))
	if err != nil {
		return filepath.Clean(wtDir)
	}
	common := strings.TrimSpace(string(cb))
	if !filepath.IsAbs(common) {
		common = filepath.Join(wtDir, common)
	}
	return filepath.Clean(common)
}

// worktrees lists every checkout of the repository at common, main worktree
// first, then by path.
//
// The list comes from git's registry (`<common>/worktrees/<name>/gitdir`), not
// from the directory scan, for two reasons: the scan skips hidden directories,
// so the common `.worktrees/<name>` layout would report none of them, and a
// worktree may sit outside every configured scan root. A registry entry whose
// directory is gone is one git has not pruned yet, and is dropped rather than
// offered as a place to work.
func worktrees(common string) []Worktree {
	var out []Worktree
	conf := gitConfig(common)
	// A non-bare repository's main working tree is the parent of its `.git`,
	// and is the one checkout the registry never lists.
	if filepath.Base(common) == ".git" && conf["core.bare"] != "true" {
		if main := filepath.Dir(common); isDir(main) {
			out = append(out, Worktree{Path: main, Branch: headBranch(common), Main: true})
		}
	}
	entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wtDir := filepath.Join(common, "worktrees", e.Name())
		gd, err := os.ReadFile(filepath.Join(wtDir, "gitdir"))
		if err != nil {
			continue
		}
		checkout := filepath.Dir(filepath.Clean(strings.TrimSpace(string(gd))))
		if !isDir(checkout) {
			continue
		}
		out = append(out, Worktree{Path: checkout, Branch: headBranch(wtDir)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Main != out[j].Main {
			return out[i].Main
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// canonical picks the one checkout the repo's row acts in.
//
// Everything downstream — `gh pr list`, a staleness `git log`, the decisions
// lens reading docs/adr off disk, `git worktree add` at dispatch — runs INSIDE
// this directory, so it has to be a real working tree. A bare repository has
// none of its own, which is the whole reason this is a choice: the main
// worktree when there is one, else the checkout on the default branch, else a
// stable pick. found is the scan's own answer, used only when git's metadata
// could not be read at all.
func canonical(wts []Worktree, found []string) string {
	best, bestRank := "", 0
	for _, w := range wts {
		rank := 4
		switch {
		case w.Main:
			rank = 1
		case w.Branch == "main":
			rank = 2
		case w.Branch == "master":
			rank = 3
		}
		if bestRank == 0 || rank < bestRank || (rank == bestRank && w.Path < best) {
			best, bestRank = w.Path, rank
		}
	}
	if best != "" {
		return best
	}
	if len(found) > 0 {
		return found[0]
	}
	return ""
}

// pinnedCheckout resolves a `[checkouts]` entry against the repo's actual
// worktrees.
//
// The automatic choice knows three spellings of a trunk — the main worktree,
// `main`, `master` — and a repository that merges into `dev` matches none of
// them, so its row would read a branch nobody ships from: the staleness log,
// the decisions scan and a dispatch's starting tree all come from this
// directory. The human names it instead.
//
// A pin that git does not list as a worktree of this repo is ignored rather
// than obeyed. It is stale (the worktree was removed), or it was typed by hand
// into the wrong repo's entry — and in both cases pointing Repo.Path at a
// directory outside the repository would make every read from it wrong in a way
// nothing on screen could explain. A pin chooses between the checkouts that
// exist; it does not invent one.
func pinnedCheckout(pin string, wts []Worktree) (string, bool) {
	if pin == "" {
		return "", false
	}
	want := filepath.Clean(pin)
	for _, w := range wts {
		if filepath.Clean(w.Path) == want {
			return w.Path, true
		}
	}
	return "", false
}

// repoName is what the repository is called, which is not what its folder is
// called. The origin remote carries the real name — a repo cloned into
// `ord-ai-n` is `ordain` to git, to GitHub and to `gh`, and gh.AssignedIssues
// keys its results by that name, so naming a checkout for its folder quietly
// dropped every issue and PR belonging to a renamed clone. Only a repo with no
// remote at all falls back to the folder, because then the folder is the only
// name it has.
func repoName(common string) string {
	if n := nameFromURL(gitConfig(common)["remote.origin.url"]); n != "" {
		return n
	}
	return folderName(common)
}

// folderName is the project directory around a git dir: `<x>/.git` and
// `<x>/.bare` are both "x", and a bare `x.git` is "x".
func folderName(common string) string {
	base := filepath.Base(common)
	if strings.HasPrefix(base, ".") {
		return filepath.Base(filepath.Dir(common))
	}
	return strings.TrimSuffix(base, ".git")
}

// nameFromURL takes the repository name out of a remote URL, in any of the
// spellings git accepts: scp-style (git@host:owner/name.git), a URL with a
// scheme, or a local path.
func nameFromURL(url string) string {
	s := strings.TrimSpace(url)
	s = strings.TrimSuffix(strings.TrimRight(s, "/"), ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	if s == "" || s == "." || s == ".." {
		return ""
	}
	return s
}

// headBranch reads the branch a git dir has checked out, "" when detached.
func headBranch(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	ref, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/")
	if !ok {
		return ""
	}
	return ref
}

// gitConfig parses a git config file into "section.key" → value, keeping the
// first value for a repeated key the way git's own last-one-wins does not
// matter here: the two keys read (remote.origin.url, core.bare) are written
// once. A subsection becomes part of the key: `[remote "origin"]` → remote.origin.
func gitConfig(gitDir string) map[string]string {
	b, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	section := ""
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = configSection(line[1 : len(line)-1])
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || section == "" {
			continue
		}
		key := section + "." + strings.ToLower(strings.TrimSpace(k))
		if _, dup := out[key]; !dup {
			out[key] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return out
}

func configSection(s string) string {
	name, sub, ok := strings.Cut(strings.TrimSpace(s), " ")
	name = strings.ToLower(strings.TrimSpace(name))
	if !ok {
		return name
	}
	return name + "." + strings.Trim(strings.TrimSpace(sub), `"`)
}

// isRepoContainer reports whether dir holds a bare repository whose checkouts
// live beside it — the `<project>/.bare` layout.
func isRepoContainer(dir string) bool {
	return isGitDir(filepath.Join(dir, ".bare"))
}

func isGitDir(p string) bool {
	if !isDir(p) {
		return false
	}
	for _, n := range []string{"HEAD", "objects", "refs"} {
		if _, err := os.Stat(filepath.Join(p, n)); err != nil {
			return false
		}
	}
	return true
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
