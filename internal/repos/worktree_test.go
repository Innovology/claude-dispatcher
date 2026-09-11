package repos

// worktree_test.go covers the part of discovery that reads git's own metadata:
// which repository a checkout belongs to, what that repository is called, and
// which of its checkouts the repo's row acts in.
//
// The fixtures are built as plain files rather than by running git, because
// that is exactly how the code reads them — no git process is spawned during a
// discovery, and a test that shelled out would be testing a different path.

import (
	"os"
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/config"
)

// fixture builds repository layouts under a temp root.
type fixture struct {
	t    *testing.T
	root string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{t: t, root: t.TempDir()}
}

func (f *fixture) mkdir(parts ...string) string {
	f.t.Helper()
	p := filepath.Join(append([]string{f.root}, parts...)...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) write(path, body string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// gitDir writes the skeleton of a git directory: what isGitDir looks for, plus
// a config naming origin (empty origin = a repo with no remote).
func (f *fixture) gitDir(dir, origin string, bare bool) {
	f.t.Helper()
	f.mkdirAt(filepath.Join(dir, "objects"))
	f.mkdirAt(filepath.Join(dir, "refs"))
	f.write(filepath.Join(dir, "HEAD"), "ref: refs/heads/main\n")
	body := "[core]\n\trepositoryformatversion = 0\n"
	if bare {
		body += "\tbare = true\n"
	}
	if origin != "" {
		body += "[remote \"origin\"]\n\turl = " + origin + "\n"
	}
	f.write(filepath.Join(dir, "config"), body)
}

func (f *fixture) mkdirAt(p string) {
	f.t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		f.t.Fatal(err)
	}
}

// worktree registers checkout as a linked worktree of the repository at common,
// writing both halves the way `git worktree add` does: the checkout's `.git`
// file pointing at its private git dir, and that dir's gitdir/commondir/HEAD.
func (f *fixture) worktree(common, checkout, branch string) {
	f.t.Helper()
	name := filepath.Base(checkout)
	wtDir := filepath.Join(common, "worktrees", name)
	f.mkdirAt(checkout)
	f.write(filepath.Join(checkout, ".git"), "gitdir: "+wtDir+"\n")
	f.write(filepath.Join(wtDir, "gitdir"), filepath.Join(checkout, ".git")+"\n")
	f.write(filepath.Join(wtDir, "commondir"), "../..\n")
	f.write(filepath.Join(wtDir, "HEAD"), "ref: refs/heads/"+branch+"\n")
}

func (f *fixture) discover() []Repo {
	f.t.Helper()
	return Discover(&config.Config{Roots: []string{f.root}})
}

func only(t *testing.T, got []Repo) Repo {
	t.Helper()
	if len(got) != 1 {
		names := []string{}
		for _, r := range got {
			names = append(names, r.Name+"@"+r.Path)
		}
		t.Fatalf("expected exactly one repo, got %d: %v", len(got), names)
	}
	return got[0]
}

// The bare layout: the repository in <project>/.bare with its checkouts beside
// it. The project directory itself holds no .git, so nothing about it looks
// like a repo to a scan — it is the worktrees that have to lead back to it.
func TestDiscoverBareWithSiblingWorktrees(t *testing.T) {
	f := newFixture(t)
	proj := f.mkdir("player-app")
	common := filepath.Join(proj, ".bare")
	f.gitDir(common, "git@github.com:Player-Pulse/player-app.git", true)
	f.worktree(common, filepath.Join(proj, "main"), "main")
	f.worktree(common, filepath.Join(proj, "ci-scheduling"), "ci-scheduling")
	f.worktree(common, filepath.Join(proj, "r2w-wearables"), "r2w-wearables")

	r := only(t, f.discover())
	if r.Name != "player-app" {
		t.Errorf("name: got %q, want player-app", r.Name)
	}
	if want := filepath.Join(proj, "main"); r.Path != want {
		t.Errorf("path: got %q, want %q", r.Path, want)
	}
	if r.GitDir != common {
		t.Errorf("git dir: got %q, want %q", r.GitDir, common)
	}
	if len(r.Worktrees) != 3 {
		t.Fatalf("worktrees: got %d, want 3", len(r.Worktrees))
	}
	// A bare repository has no main working tree — none of its checkouts is one.
	for _, w := range r.Worktrees {
		if w.Main {
			t.Errorf("%s marked as the main worktree of a bare repo", w.Path)
		}
	}
}

// The other layout on the reporting machine: an ordinary clone that grew
// worktrees later, as siblings of itself. The clone is the main working tree
// and stays the checkout the row acts in.
func TestDiscoverCloneWithSiblingWorktrees(t *testing.T) {
	f := newFixture(t)
	clone := f.mkdir("playerpulse")
	common := filepath.Join(clone, ".git")
	f.gitDir(common, "git@github.com:Player-Pulse/playerpulse.git", false)
	f.worktree(common, filepath.Join(f.root, "playerpulse-joins"), "joins")
	f.worktree(common, filepath.Join(f.root, "playerpulse-o6"), "o6")

	r := only(t, f.discover())
	if r.Name != "playerpulse" {
		t.Errorf("name: got %q, want playerpulse", r.Name)
	}
	if r.Path != clone {
		t.Errorf("path: got %q, want the clone %q", r.Path, clone)
	}
	if len(r.Worktrees) != 3 || !r.Worktrees[0].Main || r.Worktrees[0].Path != clone {
		t.Fatalf("worktrees: want the clone first and marked main, got %+v", r.Worktrees)
	}
}

// The layout neither of the above is: worktrees in a hidden folder inside the
// clone. The scan never walks into a hidden directory, so a list built from
// what the scan found would report none of these — which is why the list comes
// from git's registry instead.
func TestDiscoverHiddenWorktreesDir(t *testing.T) {
	f := newFixture(t)
	clone := f.mkdir("acme")
	common := filepath.Join(clone, ".git")
	f.gitDir(common, "https://github.com/acme/acme.git", false)
	f.worktree(common, filepath.Join(clone, ".worktrees", "feat-a"), "feat-a")
	f.worktree(common, filepath.Join(clone, ".worktrees", "feat-b"), "feat-b")

	r := only(t, f.discover())
	if len(r.Worktrees) != 3 {
		t.Fatalf("worktrees: got %d, want 3 (the clone and two hidden)", len(r.Worktrees))
	}
	if r.Path != clone {
		t.Errorf("path: got %q, want %q", r.Path, clone)
	}
}

// A repository is named by its remote, not by the folder it was cloned into.
// The folder is the fallback, and only for a repo that has no remote to ask.
func TestDiscoverNameComesFromOrigin(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct{ dir, origin, want string }{
		{"ord-ai-n", "git@github.com:kolchurinvv/ordain.git", "ordain"},
		{"k3s-migration", "https://github.com/vk/k3s-personal.git", "k3s-personal"},
		{"scp-no-suffix", "git@github.com:acme/plain", "plain"},
		{"ssh-url", "ssh://git@host:22/acme/deep/named.git", "named"},
		{"local-path", "/srv/git/mirror.git", "mirror"},
		{"no-remote-at-all", "", "no-remote-at-all"},
	} {
		f.gitDir(filepath.Join(f.mkdir(tc.dir), ".git"), tc.origin, false)
	}

	byPath := map[string]string{}
	for _, r := range f.discover() {
		byPath[filepath.Base(r.Path)] = r.Name
	}
	for _, tc := range []struct{ dir, want string }{
		{"ord-ai-n", "ordain"},
		{"k3s-migration", "k3s-personal"},
		{"scp-no-suffix", "plain"},
		{"ssh-url", "named"},
		{"local-path", "mirror"},
		{"no-remote-at-all", "no-remote-at-all"},
	} {
		if got := byPath[tc.dir]; got != tc.want {
			t.Errorf("%s: got name %q, want %q", tc.dir, got, tc.want)
		}
	}
}

// The bare layout puts the repository one level deeper than the checkouts the
// scan is looking for, so the project directory sits at the depth limit with
// nothing about it that says "repo". It was cut, and everything inside it with
// it — the reported symptom was a project that simply never appeared.
func TestDiscoverBareContainerAtDepthLimit(t *testing.T) {
	f := newFixture(t)
	// The reported shape: <root>/org/team/player-app, the project directory
	// sitting exactly on the limit with its checkouts one level below it. A
	// clone here would be found — its .git is tested before the depth is — and
	// the bare layout's evidence is one level further in, which is the whole of
	// the difference.
	proj := f.mkdir("org", "team", "player-app")
	common := filepath.Join(proj, ".bare")
	f.gitDir(common, "git@github.com:acme/player-app.git", true)
	f.worktree(common, filepath.Join(proj, "main"), "main")

	r := only(t, f.discover())
	if r.Name != "player-app" {
		t.Errorf("name: got %q, want player-app", r.Name)
	}

	// An ordinary directory at the same depth stays cut. A container buys the
	// one level that reaches its checkouts because it proves a repository is
	// there; the budget itself has not grown.
	deep := f.mkdir("org", "team", "plain", "repo")
	f.gitDir(filepath.Join(deep, ".git"), "", false)
	if got := f.discover(); len(got) != 1 {
		names := []string{}
		for _, r := range got {
			names = append(names, r.Name)
		}
		t.Errorf("expected the depth limit to still hold, got %v", names)
	}
}

// And the budget is still a budget: a container below the limit is out of
// reach, and the answer to that is a scan root nearer to it. Pinned because it
// is the edge a user meets by pointing one root at their home directory.
func TestDiscoverBareContainerBelowDepthLimitNeedsANearerRoot(t *testing.T) {
	f := newFixture(t)
	proj := f.mkdir("org", "team", "nested", "player-app")
	common := filepath.Join(proj, ".bare")
	f.gitDir(common, "git@github.com:acme/player-app.git", true)
	f.worktree(common, filepath.Join(proj, "main"), "main")

	if got := f.discover(); len(got) != 0 {
		t.Errorf("expected nothing at this depth, got %d", len(got))
	}
	nearer := Discover(&config.Config{Roots: []string{filepath.Join(f.root, "org", "team")}})
	if r := only(t, nearer); r.Name != "player-app" {
		t.Errorf("name: got %q, want player-app", r.Name)
	}
}

// A worktree removed with rm rather than `git worktree remove` leaves its
// registration behind until someone prunes. It is not a place to work.
func TestDiscoverSkipsStaleWorktreeRegistration(t *testing.T) {
	f := newFixture(t)
	proj := f.mkdir("proj")
	common := filepath.Join(proj, ".bare")
	f.gitDir(common, "git@github.com:acme/proj.git", true)
	f.worktree(common, filepath.Join(proj, "main"), "main")
	gone := filepath.Join(proj, "deleted")
	f.worktree(common, gone, "deleted")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	r := only(t, f.discover())
	if len(r.Worktrees) != 1 || filepath.Base(r.Worktrees[0].Path) != "main" {
		t.Fatalf("worktrees: want only main, got %+v", r.Worktrees)
	}
}

// Canonical checkout: a bare repo has no main working tree, so the default
// branch decides, and a repo with neither still has to answer.
func TestCanonicalCheckout(t *testing.T) {
	cases := []struct {
		name string
		wts  []Worktree
		want string
	}{
		{"main worktree wins", []Worktree{
			{Path: "/r/zzz", Branch: "main"},
			{Path: "/r", Branch: "wip", Main: true},
		}, "/r"},
		{"else the default branch", []Worktree{
			{Path: "/p/aaa", Branch: "feature"},
			{Path: "/p/main", Branch: "main"},
		}, "/p/main"},
		{"master counts too", []Worktree{
			{Path: "/p/zzz", Branch: "feature"},
			{Path: "/p/master", Branch: "master"},
		}, "/p/master"},
		{"otherwise stable and first", []Worktree{
			{Path: "/p/beta", Branch: "b"},
			{Path: "/p/alpha", Branch: "a"},
		}, "/p/alpha"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonical(tc.wts, nil); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	// No metadata at all: the scan's own answer, which is what every checkout
	// used to be on its own.
	if got := canonical(nil, []string{"/found/first", "/found/second"}); got != "/found/first" {
		t.Errorf("fallback: got %q, want /found/first", got)
	}
}

// The automatic choice knows three spellings of a trunk, and a repo that merges
// into `dev` is none of them. [checkouts] is how the human says so.
func TestDiscoverPinnedCheckout(t *testing.T) {
	f := newFixture(t)
	clone := f.mkdir("playerpulse")
	common := filepath.Join(clone, ".git")
	f.gitDir(common, "git@github.com:acme/playerpulse.git", false)
	dev := filepath.Join(f.root, "playerpulse-dev")
	f.worktree(common, dev, "dev")

	base := config.Config{Roots: []string{f.root}}
	if r := only(t, Discover(&base)); r.Path != clone || r.Pinned {
		t.Fatalf("without a pin the main worktree wins: path %q pinned %v", r.Path, r.Pinned)
	}

	pinned := base
	pinned.Checkouts = map[string]string{"playerpulse": dev}
	r := only(t, Discover(&pinned))
	if r.Path != dev {
		t.Errorf("path: got %q, want the pinned %q", r.Path, dev)
	}
	if !r.Pinned {
		t.Error("a pinned repo should say the path was named, not chosen")
	}

	// A pin is a choice between the checkouts that exist. One naming anything
	// else is stale or mistyped, and obeying it would point every read at a
	// directory outside the repository.
	for _, bogus := range []string{
		filepath.Join(f.root, "not-a-worktree"),
		"/nowhere/at/all",
	} {
		off := base
		off.Checkouts = map[string]string{"playerpulse": bogus}
		r := only(t, Discover(&off))
		if r.Path != clone || r.Pinned {
			t.Errorf("pin %q should be ignored, got path %q pinned %v", bogus, r.Path, r.Pinned)
		}
	}
}

// Two checkouts of one repository are one repository, and the products map
// keyed by the repo's name reaches it.
func TestDiscoverGroupsAndMapsProduct(t *testing.T) {
	f := newFixture(t)
	proj := f.mkdir("dispatcher")
	common := filepath.Join(proj, ".bare")
	f.gitDir(common, "git@github.com:Innovology/claude-dispatcher.git", true)
	f.worktree(common, filepath.Join(proj, "main"), "main")
	f.worktree(common, filepath.Join(proj, "nix-pkgs"), "nix-pkgs")

	got := Discover(&config.Config{
		Roots:    []string{f.root},
		Products: map[string][]string{"tools": {"claude-dispatcher"}},
	})
	r := only(t, got)
	if r.Product != "tools" {
		t.Errorf("product: got %q, want tools — the map is keyed by the repo's name", r.Product)
	}
}
