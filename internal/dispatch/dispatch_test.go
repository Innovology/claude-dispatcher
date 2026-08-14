//go:build !windows

package dispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init", "-b", "main")
	git("commit", "--allow-empty", "-m", "base")
	return repo
}

func worktreeBranch(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse in %s: %v", path, err)
	}
	return strings.TrimSpace(string(out))
}

func TestEnsureWorktree(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt", "feat")

	if err := ensureWorktree(repo, wt, "feature/feat"); err != nil {
		t.Fatal(err)
	}
	if got := worktreeBranch(t, wt); got != "feature/feat" {
		t.Fatalf("worktree on %q, want feature/feat", got)
	}
	// The repo's own checkout must be untouched.
	if got := worktreeBranch(t, repo); got != "main" {
		t.Fatalf("repo checkout moved to %q", got)
	}
	// Re-dispatch of the same feature reuses the worktree.
	if err := ensureWorktree(repo, wt, "feature/feat"); err != nil {
		t.Fatalf("reuse failed: %v", err)
	}
	// A path occupied by a different branch's worktree is refused.
	if err := ensureWorktree(repo, wt, "feature/second"); err == nil {
		t.Fatal("expected error for occupied path")
	}
	// An existing branch is checked out rather than recreated.
	wt2 := filepath.Join(t.TempDir(), "feat-again")
	if out, err := exec.Command("git", "-C", repo, "worktree", "remove", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree remove: %s", out)
	}
	if err := ensureWorktree(repo, wt2, "feature/feat"); err != nil {
		t.Fatalf("existing branch: %v", err)
	}
	if got := worktreeBranch(t, wt2); got != "feature/feat" {
		t.Fatalf("worktree on %q, want feature/feat", got)
	}
}

// A dispatch must start from the repo's default branch, never from whatever
// the human happens to have checked out — otherwise it inherits an unrelated
// unmerged feature and carries it into its own PR.
func TestEnsureWorktreeBranchesFromDefaultNotHEAD(t *testing.T) {
	repo := initRepo(t)
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	// The human is mid-feature in the repo's own checkout.
	git("checkout", "-b", "feature/human-wip")
	git("commit", "--allow-empty", "-m", "human WIP")

	wt := filepath.Join(t.TempDir(), "feat")
	if err := ensureWorktree(repo, wt, "feature/feat"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", wt, "log", "--format=%s", "main..HEAD").Output()
	if err != nil {
		t.Fatalf("log in worktree: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "" {
		t.Errorf("dispatch branch carries commits not on main: %q", got)
	}
	// The human's checkout is left exactly where they had it.
	if got := worktreeBranch(t, repo); got != "feature/human-wip" {
		t.Errorf("repo checkout moved to %q", got)
	}
}

// baseRef prefers the remote's view of the default branch, so a local main
// that has fallen behind origin is not used as the base.
func TestBaseRefPrefersRemoteDefault(t *testing.T) {
	repo := initRepo(t)
	// No remote at all: fall back to the local default branch.
	if got := baseRef(repo); got != "refs/heads/main" {
		t.Errorf("baseRef with no remote = %q, want refs/heads/main", got)
	}
	// With a remote-tracking ref present, prefer it over the local branch.
	if out, err := exec.Command("git", "-C", repo, "update-ref",
		"refs/remotes/origin/main", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("update-ref: %s", out)
	}
	if got := baseRef(repo); got != "refs/remotes/origin/main" {
		t.Errorf("baseRef = %q, want refs/remotes/origin/main", got)
	}
}

// A new feature branch must not inherit the base as its upstream: with a
// remote-tracking base, push.default=simple would refuse to push it.
func TestEnsureWorktreeLeavesBranchUntracked(t *testing.T) {
	repo := initRepo(t)
	if out, err := exec.Command("git", "-C", repo, "update-ref",
		"refs/remotes/origin/main", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("update-ref: %s", out)
	}
	wt := filepath.Join(t.TempDir(), "feat")
	if err := ensureWorktree(repo, wt, "feature/feat"); err != nil {
		t.Fatal(err)
	}
	err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet",
		"feature/feat@{upstream}").Run()
	if err == nil {
		t.Error("feature branch inherited an upstream from its base")
	}
}

func TestCleanupWorktree(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "feat")
	if err := ensureWorktree(repo, wt, "feature/feat"); err != nil {
		t.Fatal(err)
	}
	// Dirty worktree is kept — killing a dispatcher must not discard work.
	if err := os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if CleanupWorktree(repo, wt) {
		t.Fatal("dirty worktree must not be removed")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("dirty worktree directory vanished")
	}
	// Clean worktree is removed.
	if err := os.Remove(filepath.Join(wt, "wip.txt")); err != nil {
		t.Fatal(err)
	}
	if !CleanupWorktree(repo, wt) {
		t.Fatal("clean worktree should be removed")
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree directory still present")
	}
	// "Gone" is the contract, so a second call and a dispatch that never had
	// a worktree both report gone rather than a spurious "kept".
	if !CleanupWorktree(repo, wt) {
		t.Error("an already-removed worktree should report gone")
	}
	if !CleanupWorktree(repo, "") {
		t.Error("a dispatch with no worktree should report gone")
	}
}

// Two live dispatches of one feature would share worktrees/<repo>/<slug> —
// two claude sessions in a single checkout, the collision worktrees exist to
// prevent — so the second is refused while the first is still running.
func TestLaunchRefusesDuplicateOfLiveFeature(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)

	rec := &state.Dispatch{
		ID: state.NewID(), Feature: "Payment Retry", Slug: "payment-retry",
		RepoPath: repo, RepoName: "acme", Branch: "feature/payment-retry",
		TmuxSession: "disp-payment-retry", Status: state.StatusWorking,
	}
	if err := state.Save(rec); err != nil {
		t.Fatal(err)
	}

	alive := map[string]bool{"disp-payment-retry": true}
	restore := sessionAlive
	sessionAlive = func(name string) bool { return alive[name] }
	defer func() { sessionAlive = restore }()

	_, err := Launch(repos.Repo{Name: "acme", Path: repo}, "payment retry", "go")
	if err == nil {
		t.Fatal("expected a duplicate of a live feature to be refused")
	}
	if !strings.Contains(err.Error(), "already live") {
		t.Errorf("unhelpful error: %v", err)
	}

	// Once the session has ended, the feature can be dispatched again — that
	// path deliberately reuses the worktree left behind.
	alive["disp-payment-retry"] = false
	if got := liveDispatch("payment-retry"); got != nil {
		t.Errorf("dead session still reported live: %#v", got)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Payment Retry Flow!":   "payment-retry-flow",
		"--already-sluggy--":    "already-sluggy",
		"  spaces  every/where": "spaces-every-where",
		"":                      "",
		"???":                   "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShellQuoteRoundTrip(t *testing.T) {
	prompts := []string{
		"plain",
		"it's got 'quotes' in it",
		`double "quotes" and $vars and ` + "`backticks`",
		"multi\nline\nprompt",
	}
	for _, p := range prompts {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(p)).Output()
		if err != nil {
			t.Fatalf("sh failed for %q: %v", p, err)
		}
		if string(out) != p {
			t.Errorf("round trip of %q gave %q", p, string(out))
		}
	}
}

func TestSlugifyStripsNonASCII(t *testing.T) {
	if got := Slugify("héllo wörld"); strings.Contains(got, "é") || strings.Contains(got, "ö") {
		t.Errorf("expected non-ascii stripped, got %q", got)
	}
}

// TestSlugifyCaps guards the names a dispatch creates. The slug names three
// things at once — the branch, the worktree directory and the tmux session —
// and it used to take whatever was typed as the feature name whole. Real
// records ended up with 91-character slugs and 96-character session names,
// because the dispatch prompt fed the entire typed sentence in as the name.
func TestSlugifyCaps(t *testing.T) {
	long := "several improvements here: Use the claude_design MCP (https://api.anthropic.com/v1/design/mcp, auth via /design-login) to import this project"
	got := Slugify(long)

	if n := len(strings.Split(got, "-")); n > SlugWords {
		t.Errorf("slug kept %d words, want at most %d: %q", n, SlugWords, got)
	}
	if len(got) > 60 {
		t.Errorf("slug is %d chars, too long for a path component: %q", len(got), got)
	}
	if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
		t.Errorf("slug has a dangling separator: %q", got)
	}

	// Short names are untouched — the cap must not mangle the ordinary case.
	for _, s := range []string{"retry backoff", "csv export", "seat limits"} {
		if got := Slugify(s); got != strings.ReplaceAll(s, " ", "-") {
			t.Errorf("Slugify(%q) = %q, want it unchanged", s, got)
		}
	}

	// A single enormous word still has to fit.
	if got := Slugify(strings.Repeat("x", 200)); len(got) > 60 {
		t.Errorf("one long word not bounded: %d chars", len(got))
	}

	// Slugifying twice is the same as once — the cockpit names the feature from
	// the slug, and Launch slugifies that name again.
	if a, b := Slugify(long), Slugify(strings.ReplaceAll(Slugify(long), "-", " ")); a != b {
		t.Errorf("not idempotent: %q vs %q", a, b)
	}

	if Slugify("") != "" || Slugify("!!!") != "" {
		t.Error("an unnameable feature should still produce an empty slug")
	}
}
