package initcmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// binAndLink builds the shape every packaged install has: the real binary at
// one path, and a stable name on PATH pointing at it.
func binAndLink(t *testing.T) (real, link string) {
	t.Helper()
	dir := t.TempDir()
	// t.TempDir can itself sit behind a symlink (/var → /private/var on
	// macOS); resolve it so the comparisons below are about our links only.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolving temp dir: %v", err)
	}
	real = filepath.Join(dir, "abcdef-claude-dispatcher-2.3.1", "claude-dispatcher")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(dir, "bin", "claude-dispatcher")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	return real, link
}

func found(path string) func(string) (string, error) {
	return func(string) (string, error) { return path, nil }
}

// The whole point: a Nix or Homebrew install is reached through a stable
// symlink, and the hook has to record that rather than the versioned path it
// resolves to, which the next upgrade replaces.
func TestHookExePrefersTheStableName(t *testing.T) {
	real, link := binAndLink(t)

	got := hookExe(real, "claude-dispatcher", found(link), filepath.EvalSymlinks)

	if got != link {
		t.Fatalf("hookExe = %q, want the stable link %q", got, link)
	}
}

// PATH may hold a second, older copy of the dispatcher — the brew versus
// ~/.local/bin trap. Recording it would aim the hook at a different build, so
// a candidate that does not resolve to this binary is refused.
func TestHookExeRefusesADifferentCopyOnPath(t *testing.T) {
	real, _ := binAndLink(t)
	other, _ := binAndLink(t)

	got := hookExe(real, "claude-dispatcher", found(other), filepath.EvalSymlinks)

	if got != real {
		t.Fatalf("hookExe = %q, want the running binary %q", got, real)
	}
}

func TestHookExeFallsBackWhenPathLookupFails(t *testing.T) {
	real, _ := binAndLink(t)
	lookPath := func(string) (string, error) { return "", errors.New("not found") }

	got := hookExe(real, "claude-dispatcher", lookPath, filepath.EvalSymlinks)

	if got != real {
		t.Fatalf("hookExe = %q, want the running binary %q", got, real)
	}
}

// Invoked by path rather than by name: no PATH lookup happens, but the same
// proof is required before the given path is trusted.
func TestHookExeAcceptsAnExplicitPath(t *testing.T) {
	real, link := binAndLink(t)
	never := func(string) (string, error) { t.Fatal("PATH lookup for an explicit path"); return "", nil }

	got := hookExe(real, link, never, filepath.EvalSymlinks)

	if got != link {
		t.Fatalf("hookExe = %q, want %q", got, link)
	}
}

func TestHookExeKeepsTheBinaryWhenNothingResolves(t *testing.T) {
	real, _ := binAndLink(t)
	gone := filepath.Join(t.TempDir(), "bin", "claude-dispatcher")

	got := hookExe(real, gone, found(gone), filepath.EvalSymlinks)

	if got != real {
		t.Fatalf("hookExe = %q, want the running binary %q", got, real)
	}
}
