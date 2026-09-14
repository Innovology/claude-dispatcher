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

// The Nix package runs wrapProgram, which moves the binary to
// bin/.claude-dispatcher-wrapped and leaves a script in its place. The name on
// PATH resolves to that script, never to os.Executable — and refusing it pinned
// every Nix install's hook to a store path the next garbage collection deleted.
func TestHookExeSeesThroughTheNixWrapper(t *testing.T) {
	real, link := binAndLink(t)
	wrapped := filepath.Join(filepath.Dir(real), ".claude-dispatcher-wrapped")
	if err := os.WriteFile(wrapped, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := hookExe(wrapped, link, found(link), filepath.EvalSymlinks)

	if got != link {
		t.Fatalf("hookExe = %q, want the stable link %q", got, link)
	}
}

// A wrapper is only proof when it sits beside the binary: another build's
// script of the same name is another build.
func TestHookExeRefusesAnotherBuildsWrapper(t *testing.T) {
	real, _ := binAndLink(t)
	_, otherLink := binAndLink(t)
	wrapped := filepath.Join(filepath.Dir(real), ".claude-dispatcher-wrapped")

	got := hookExe(wrapped, otherLink, found(otherLink), filepath.EvalSymlinks)

	if got != wrapped {
		t.Fatalf("hookExe = %q, want the running binary %q", got, wrapped)
	}
}

func commandEntry(matcher string, cmds ...string) any {
	inner := []any{}
	for _, c := range cmds {
		inner = append(inner, map[string]any{"type": "command", "command": c})
	}
	e := map[string]any{"hooks": inner}
	if matcher != "" {
		e["matcher"] = matcher
	}
	return e
}

func commandsFor(hooks map[string]any, event, matcher string) []string {
	var out []string
	for _, e := range hooks[event].([]any) {
		entry := e.(map[string]any)
		if m, _ := entry["matcher"].(string); m != matcher {
			continue
		}
		for _, h := range entry["hooks"].([]any) {
			out = append(out, h.(map[string]any)["command"].(string))
		}
	}
	return out
}

// The reported machine: three builds' worth of hooks, two of them collected,
// all named .claude-dispatcher-wrapped. Re-running init has to leave one set
// pointing at the durable path, not append a fourth.
func TestReconcileRepairsHooksLeftByCollectedBuilds(t *testing.T) {
	const exe = "/run/current-system/sw/bin/claude-dispatcher"
	hooks := map[string]any{}
	for _, spec := range hookSpecs() {
		var entries []any
		for _, hash := range []string{"9vzx", "w5ls", "8gk9"} {
			entries = append(entries, commandEntry(spec.matcher,
				"/nix/store/"+hash+"-claude-dispatcher-dev/bin/.claude-dispatcher-wrapped hook "+spec.arg))
		}
		prior, _ := hooks[spec.event].([]any)
		hooks[spec.event] = append(prior, entries...)
	}
	hooks["SessionStart"] = append([]any{commandEntry("", "/usr/bin/other-tool start")}, hooks["SessionStart"].([]any)...)

	ch := reconcileHooks(hooks, exe)

	n := len(hookSpecs())
	if want := (hookChanges{repointed: n, dropped: 2 * n}); ch != want {
		t.Fatalf("changes = %+v, want %+v", ch, want)
	}
	for _, spec := range hookSpecs() {
		got := commandsFor(hooks, spec.event, spec.matcher)
		if spec.event == "SessionStart" {
			if len(got) != 2 || got[0] != "/usr/bin/other-tool start" {
				t.Fatalf("SessionStart = %q, want the other tool's hook kept first", got)
			}
			got = got[1:]
		}
		if want := exe + " hook " + spec.arg; len(got) != 1 || got[0] != want {
			t.Fatalf("%s/%s = %q, want exactly %q", spec.event, spec.matcher, got, want)
		}
	}
	if again := reconcileHooks(hooks, exe); again != (hookChanges{}) {
		t.Fatalf("second run changed %+v, want nothing", again)
	}
}

// A hook of ours sharing an entry with someone else's is taken out of the entry
// and the entry kept; nothing that is not ours is ever removed.
func TestReconcileKeepsForeignHooksInASharedEntry(t *testing.T) {
	const exe = "/home/u/.local/bin/claude-dispatcher"
	hooks := map[string]any{
		"Stop": []any{commandEntry("", "/old/claude-dispatcher hook Stop", "notify-send done")},
	}

	reconcileHooks(hooks, exe)

	if got := commandsFor(hooks, "Stop", ""); len(got) != 2 || got[0] != "notify-send done" || got[1] != exe+" hook Stop" {
		t.Fatalf("Stop = %q, want the foreign hook kept and ours repointed", got)
	}
}

func TestIsOurCommand(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/home/u/.local/bin/claude-dispatcher hook Stop":                                true,
		"/nix/store/abc-claude-dispatcher-dev/bin/.claude-dispatcher-wrapped hook Stop": true,
		`C:\Program Files\claude dispatcher\claude-dispatcher.exe hook Stop`:            true,
		"/home/u/.local/bin/claude-dispatcher hook SessionEnd":                          false,
		"/usr/bin/not-claude-dispatcher hook Stop":                                      false,
		"echo claude-dispatcher hook Stop":                                              false,
	} {
		if got := isOurCommand(cmd, "Stop"); got != want {
			t.Errorf("isOurCommand(%q) = %v, want %v", cmd, got, want)
		}
	}
}
