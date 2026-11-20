package binpath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
func TestDurablePrefersTheStableName(t *testing.T) {
	real, link := binAndLink(t)

	got := Durable(real, "claude-dispatcher", found(link), filepath.EvalSymlinks)

	if got != link {
		t.Fatalf("Durable = %q, want the stable link %q", got, link)
	}
}

// PATH may hold a second, older copy of the dispatcher — the brew versus
// ~/.local/bin trap. Recording it would aim the hook at a different build, so
// a candidate that does not resolve to this binary is refused.
func TestDurableRefusesADifferentCopyOnPath(t *testing.T) {
	real, _ := binAndLink(t)
	other, _ := binAndLink(t)

	got := Durable(real, "claude-dispatcher", found(other), filepath.EvalSymlinks)

	if got != real {
		t.Fatalf("Durable = %q, want the running binary %q", got, real)
	}
}

func TestDurableFallsBackWhenPathLookupFails(t *testing.T) {
	real, _ := binAndLink(t)
	lookPath := func(string) (string, error) { return "", errors.New("not found") }

	got := Durable(real, "claude-dispatcher", lookPath, filepath.EvalSymlinks)

	if got != real {
		t.Fatalf("Durable = %q, want the running binary %q", got, real)
	}
}

// Invoked by path rather than by name: no PATH lookup happens, but the same
// proof is required before the given path is trusted.
func TestDurableAcceptsAnExplicitPath(t *testing.T) {
	real, link := binAndLink(t)
	never := func(string) (string, error) { t.Fatal("PATH lookup for an explicit path"); return "", nil }

	got := Durable(real, link, never, filepath.EvalSymlinks)

	if got != link {
		t.Fatalf("Durable = %q, want %q", got, link)
	}
}

func TestDurableKeepsTheBinaryWhenNothingResolves(t *testing.T) {
	real, _ := binAndLink(t)
	gone := filepath.Join(t.TempDir(), "bin", "claude-dispatcher")

	got := Durable(real, gone, found(gone), filepath.EvalSymlinks)

	if got != real {
		t.Fatalf("Durable = %q, want the running binary %q", got, real)
	}
}

// The Nix package runs wrapProgram, which moves the binary to
// bin/.claude-dispatcher-wrapped and leaves a script in its place. The name on
// PATH resolves to that script, never to os.Executable — and refusing it pinned
// every Nix install's hook to a store path the next garbage collection deleted.
func TestDurableSeesThroughTheNixWrapper(t *testing.T) {
	real, link := binAndLink(t)
	wrapped := filepath.Join(filepath.Dir(real), ".claude-dispatcher-wrapped")
	if err := os.WriteFile(wrapped, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := Durable(wrapped, link, found(link), filepath.EvalSymlinks)

	if got != link {
		t.Fatalf("Durable = %q, want the stable link %q", got, link)
	}
}

// A wrapper is only proof when it sits beside the binary: another build's
// script of the same name is another build.
func TestDurableRefusesAnotherBuildsWrapper(t *testing.T) {
	real, _ := binAndLink(t)
	_, otherLink := binAndLink(t)
	wrapped := filepath.Join(filepath.Dir(real), ".claude-dispatcher-wrapped")

	got := Durable(wrapped, otherLink, found(otherLink), filepath.EvalSymlinks)

	if got != wrapped {
		t.Fatalf("Durable = %q, want the running binary %q", got, wrapped)
	}
}

// A test binary is a go-build artifact, which the toolchain deletes once the run
// is over, so Self names the binary rather than a path that will not be there.
func TestSelfNamesATemporaryBuildByName(t *testing.T) {
	if got := Self(); got != Name {
		t.Errorf("Self() = %q from a test binary, want %q", got, Name)
	}
	if !Temporary("/tmp/go-build123/b001/exe/claude-dispatcher") || Temporary("/usr/local/bin/claude-dispatcher") {
		t.Error("Temporary misread a go-build path")
	}
}

// An installed binary is named by the durable path it was invoked through.
func TestSelfNamesAnInstalledBinaryByItsDurablePath(t *testing.T) {
	real, link := binAndLink(t)
	prevExe, prevArgs := executable, os.Args
	executable = func() (string, error) { return real, nil }
	os.Args = append([]string{link}, os.Args[1:]...)
	t.Cleanup(func() { executable, os.Args = prevExe, prevArgs })
	if got := Self(); got != link {
		t.Errorf("Self() = %q, want %q", got, link)
	}
	executable = func() (string, error) { return "", errors.New("no /proc") }
	if got := Self(); got != Name {
		t.Errorf("Self() with no executable = %q, want %q", got, Name)
	}
	if strings.Contains(Name, "/") {
		t.Errorf("Name %q is a path", Name)
	}
}
