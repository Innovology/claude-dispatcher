package cockpit

// root_test.go covers ROOT on both dispatch forms — the branch a dispatch is
// cut from.
//
// It exists because a dispatcher forked a branch that had been dead for months
// and there was nowhere to say otherwise: the base was whatever
// refs/remotes/origin/HEAD happened to name, which is a cache written at clone
// time (see dispatch/root.go for the failure and the fix). The forms are half
// of the answer, so what they assert is what LEAVES them — the argument the
// launch is given — and not the value sitting on their own screen.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
)

// seedGitRepo makes a scan root holding one real git repo with the given
// branches, so the ROOT step has something to read. seedRepoRoot's repos are
// bare `.git` directories: enough for discovery, and nothing a branch list can
// come out of.
func seedGitRepo(t *testing.T, name string, branches ...string) (root, repoPath string) {
	t.Helper()
	root = t.TempDir()
	repoPath = filepath.Join(root, name)
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", repoPath, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	if out, err := exec.Command("git", "init", "--quiet", "-b", "main", repoPath).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", out)
	}
	git("commit", "--allow-empty", "-m", "base")
	for _, b := range branches {
		git("branch", b)
	}
	return root, repoPath
}

// The `+` overlay's ROOT step offers the repo's branches, and the branch picked
// there is the one the launch is asked for.
func TestDispatchFormRootReachesTheLaunch(t *testing.T) {
	root, _ := seedGitRepo(t, "shop-web", "release/24")
	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{Roots: []string{root}}
	m.lens = "products" // '+' is swallowed by the triage lens

	got := captureLaunches(t)

	m = press(m, "+")
	m = press(m, "enter") // the only repo
	m = typeStr(m, "csv export")
	m = press(m, "enter") // → root

	if m.dispatchForm.step != dispatchRoot {
		t.Fatalf("step = %d, want root", m.dispatchForm.step)
	}
	// The branches are on screen, under the default.
	out := m.View()
	for _, want := range []string{"default", "release/24", "main"} {
		if !strings.Contains(out, want) {
			t.Errorf("the root step does not offer %q", want)
		}
	}
	// Type to narrow, exactly as the repo step does, then take what is left.
	m = typeStr(m, "release")
	if vis := m.dispatchForm.rootFiltered(); len(vis) != 1 || vis[0].name != "release/24" {
		t.Fatalf("filtering by 'release' left %v", vis)
	}
	m = press(m, "enter") // → mode
	m = press(m, "enter") // → model
	m = press(m, "enter") // → account
	m = press(m, "enter") // → fan out
	m = press(m, "enter") // → prompt
	m = typeStr(m, "do the thing")
	next, _ := m.handleKey("enter")
	m = next.(model)

	if got.root != dispatchpkg.Root("release/24") {
		t.Errorf("the launch was asked to cut from %q, want release/24", got.root)
	}
	if !strings.Contains(m.notice, "release/24") {
		t.Errorf("notice = %q — it does not say what the dispatch was cut from", m.notice)
	}
}

// Taking the default names no branch. The repo's default is resolved from the
// remote at launch, so a form that filled one in here — from origin/HEAD, the
// only local answer available — would be quoting the very cache that had
// dispatches forking dead branches.
func TestDispatchFormDefaultRootNamesNoBranch(t *testing.T) {
	root, _ := seedGitRepo(t, "shop-web", "release/24")
	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{Roots: []string{root}}
	m.lens = "products"

	got := captureLaunches(t)

	m = press(m, "+")
	m = press(m, "enter")
	m = typeStr(m, "csv export")
	m = press(m, "enter") // → root, on "default"
	if r := m.dispatchForm.root(); r != dispatchpkg.RootDefault {
		t.Fatalf("root opened on %q, want default", r)
	}
	for i := 0; i < 4; i++ {
		m = press(m, "enter") // root → mode → model → fan out → prompt
	}
	m = typeStr(m, "do the thing")
	next, _ := m.handleKey("enter")
	m = next.(model)

	if !got.root.IsDefault() {
		t.Errorf("the launch was asked to cut from %q, want the default", got.root)
	}
	if strings.Contains(m.notice, "from ") {
		t.Errorf("notice = %q — the default names no branch until the launch asks origin", m.notice)
	}
}

// Coming back and picking a different repo takes the branch choice with it: a
// root is a branch in one repo, and carrying it across to a repo with no such
// branch would be carrying a launch failure.
func TestDispatchFormRootResetsWithTheRepo(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"alpha", "beta"} {
		if out, err := exec.Command("git", "init", "--quiet", "-b", "main", filepath.Join(root, n)).CombinedOutput(); err != nil {
			t.Fatalf("git init %s: %s", n, out)
		}
		full := []string{"-C", filepath.Join(root, n), "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "base"}
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("commit in %s: %s", n, out)
		}
	}
	if out, err := exec.Command("git", "-C", filepath.Join(root, "alpha"), "branch", "release/24").CombinedOutput(); err != nil {
		t.Fatalf("branch: %s", out)
	}

	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{Roots: []string{root}}
	m.lens = "products"

	m = press(m, "+")
	m = press(m, "enter") // alpha
	m = typeStr(m, "thing")
	m = press(m, "enter") // → root
	m = typeStr(m, "release")
	m = press(m, "enter") // take release/24, → mode
	if r := m.dispatchForm.root(); r != dispatchpkg.Root("release/24") {
		t.Fatalf("root = %q, want release/24", r)
	}

	// Back to the repo step and over to beta, which has no release/24.
	m = press(m, "esc") // → root
	m = press(m, "esc") // → feature
	m = press(m, "esc") // → repo
	m = press(m, "down")
	m = press(m, "enter") // beta
	m = press(m, "enter") // → root
	if r := m.dispatchForm.root(); r != dispatchpkg.RootDefault {
		t.Errorf("root = %q after switching repo, want the default back", r)
	}
	if got := m.dispatchForm.rootFilter.Value(); got != "" {
		t.Errorf("the branch filter survived the repo change: %q", got)
	}
}

// The dx form's ROOT is typed, and what is typed reaches the launch.
func TestDXSubmitPassesTheRoot(t *testing.T) {
	got := captureLaunches(t)

	m := dxFormModel(t)
	m.dxTitle, m.dxWhat, m.dxRoot = "payment retries", "retry declined cards", "release/24"
	if _, _ = m.dxSubmit(); got.root != dispatchpkg.Root("release/24") {
		t.Errorf("the launch got root %q, want release/24", got.root)
	}

	// And a blank field is the default, not an empty branch name.
	m = dxFormModel(t)
	m.dxTitle, m.dxWhat = "payment retries", "retry declined cards"
	if _, _ = m.dxSubmit(); !got.root.IsDefault() {
		t.Errorf("an untouched ROOT launched as %q, want the default", got.root)
	}
}

// A typo is caught on the form, with the near misses named, rather than at the
// launch: by then the form has closed and the brief typed into it is gone.
func TestDXRefusesARootThatIsNotABranch(t *testing.T) {
	root, repoPath := seedGitRepo(t, "shop-api", "release/24")
	prev := lastDiscovered
	lastDiscovered = []repos.Repo{{Name: "shop-api", Path: repoPath}}
	t.Cleanup(func() { lastDiscovered = prev })

	launched := captureLaunches(t)

	m := newModel()
	m.width, m.height = 130, 40
	m.cfg = &config.Config{Roots: []string{root}}
	m = m.dxOpen("")
	m.dxTitle, m.dxWhat, m.dxRoot = "payment retries", "retry declined cards", "realese/24"

	next, cmd := m.dxSubmit()
	m = next
	if cmd != nil || launched.n > 0 {
		t.Fatal("a root that is not a branch was dispatched anyway")
	}
	if !m.cqDispatch {
		t.Fatal("the form closed on a rejected root, taking the brief with it")
	}
	if m.dxField != dxRootF {
		t.Errorf("the keyboard went to field %d, want ROOT — the human has to fix it there", m.dxField)
	}
	if !strings.Contains(m.notice, "release/24") {
		t.Errorf("notice = %q — a near miss is exactly what a typo needs to see", m.notice)
	}
	if m.dxWhat != "retry declined cards" {
		t.Errorf("the brief was lost: %q", m.dxWhat)
	}

	// The branch that does exist goes straight through.
	m.dxRoot = "release/24"
	if _, cmd := m.dxSubmit(); cmd == nil {
		t.Error("a real branch was refused")
	}
	if launched.root != dispatchpkg.Root("release/24") {
		t.Errorf("the launch got root %q, want release/24", launched.root)
	}
}

// A repo the scan has not turned up has no path to read branches from, and a
// form that refused a branch on the strength of a list it could not read would
// be worse than one that let the launch answer — which it does, out loud.
func TestDXDoesNotRefuseWhatItCannotCheck(t *testing.T) {
	prev := lastDiscovered
	lastDiscovered = nil
	t.Cleanup(func() { lastDiscovered = prev })

	captureLaunches(t)
	m := dxFormModel(t)
	m.dxTitle, m.dxWhat, m.dxRoot = "payment retries", "retry declined cards", "release/24"
	if _, cmd := m.dxSubmit(); cmd == nil {
		t.Error("a root was refused against a branch list that could not be read")
	}
}

// The detail panel names the branch a dispatcher was cut from. Nothing could
// show it before: the record kept the base commit, and a commit does not say
// which branch it was the tip of — so a dispatcher quietly working on top of a
// dead branch looked exactly like one on top of main.
func TestFleetMetaNamesTheRoot(t *testing.T) {
	if got := fleetRootLine("origin/main"); got != "from origin/main" {
		t.Errorf("fleetRootLine = %q, want %q", got, "from origin/main")
	}
	// A re-dispatch cut nothing, and a record written before this recorded
	// nothing. Neither is "cut from the default": which branch that would have
	// been is the one fact nobody wrote down.
	if got := fleetRootLine(""); got != "" {
		t.Errorf("fleetRootLine(\"\") = %q, want silence", got)
	}
	if got := fleetMeta(fleetRow{root: "origin/trunk"}); !strings.Contains(got, "from origin/trunk") {
		t.Errorf("fleetMeta = %q, want the root in it", got)
	}
	if got := fleetMeta(fleetRow{}); strings.Contains(got, "from ") {
		t.Errorf("fleetMeta = %q, want no root clause", got)
	}
}

// A blank ROOT on a repo with a root branch of its own says that branch, on
// both forms — "as origin sees it" would be describing a launch that will not
// happen.
func TestFormsNameTheRepoRootBranch(t *testing.T) {
	m := dxFormModel(t)
	m.cfg.RootBranches = map[string]string{"shop-api": "dev"}
	if h := m.dxRootHint(); !strings.Contains(h, "dev") || strings.Contains(h, "as origin sees it") {
		t.Errorf("dx ROOT hint = %q", h)
	}
	m.dxTitle = "payment retries"
	if s := m.dxSummary(); !strings.Contains(s, "from dev") {
		t.Errorf("dx summary = %q", s)
	}
	df := &dispatchForm{}
	df.repo.RootBranch = "dev"
	if o := df.rootOptions()[0]; !strings.Contains(o.hint, "dev") {
		t.Errorf("+ overlay default row = %+v", o)
	}
	if df.rootLabel() != "default · dev" {
		t.Errorf("+ overlay breadcrumb = %q", df.rootLabel())
	}
}
