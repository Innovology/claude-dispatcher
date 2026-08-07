package dispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
