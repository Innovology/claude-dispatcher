package dispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeClaudeConfig lays down a ~/.claude.json with the given projects plus a
// key we do not understand, so the round-trip is exercised.
func writeClaudeConfig(t *testing.T, projects map[string]any) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := map[string]any{
		"projects":         projects,
		"someOtherSetting": "must survive",
		"numberOfStartups": 41,
		"tipsHistory":      map[string]any{"a": 1},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func readProjects(t *testing.T, home string) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Projects map[string]map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("config is no longer valid JSON: %v", err)
	}
	return cfg.Projects
}

// The whole point: a worktree of a trusted repo starts without a prompt.
func TestInheritTrustFromATrustedRepo(t *testing.T) {
	home := writeClaudeConfig(t, map[string]any{
		"/repos/shop": map[string]any{"hasTrustDialogAccepted": true, "allowedTools": []string{"Bash"}},
	})
	if !InheritTrust("/repos/shop", "/state/worktrees/shop/csv-export") {
		t.Fatal("a worktree of a trusted repo should inherit trust")
	}
	got := readProjects(t, home)
	if v, _ := got["/state/worktrees/shop/csv-export"]["hasTrustDialogAccepted"].(bool); !v {
		t.Errorf("worktree not marked trusted: %+v", got)
	}
	// The repo's own entry, and its other settings, must be untouched.
	if v, _ := got["/repos/shop"]["hasTrustDialogAccepted"].(bool); !v {
		t.Error("the repo's own trust was disturbed")
	}
	if got["/repos/shop"]["allowedTools"] == nil {
		t.Error("unrelated project settings were dropped")
	}
}

// Trust is inherited, never invented.
func TestInheritTrustRefusesWhenTheRepoIsNotTrusted(t *testing.T) {
	home := writeClaudeConfig(t, map[string]any{
		"/repos/shop": map[string]any{"hasTrustDialogAccepted": false},
	})
	if InheritTrust("/repos/shop", "/state/worktrees/shop/x") {
		t.Error("an untrusted repo must not confer trust on its worktree")
	}
	if _, ok := readProjects(t, home)["/state/worktrees/shop/x"]; ok {
		t.Error("nothing should have been written for the worktree")
	}

	// A repo Claude Code has never seen is equally not a licence.
	if InheritTrust("/repos/never-opened", "/state/worktrees/never/x") {
		t.Error("an unknown repo must not confer trust")
	}
}

// The config belongs to Claude Code; every key we do not understand round-trips.
func TestInheritTrustPreservesTheRestOfTheConfig(t *testing.T) {
	home := writeClaudeConfig(t, map[string]any{
		"/repos/shop": map[string]any{"hasTrustDialogAccepted": true},
	})
	InheritTrust("/repos/shop", "/state/worktrees/shop/y")

	b, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("config no longer parses: %v", err)
	}
	if cfg["someOtherSetting"] != "must survive" {
		t.Errorf("unknown top-level keys were lost: %v", cfg["someOtherSetting"])
	}
	if cfg["tipsHistory"] == nil {
		t.Error("tipsHistory was dropped")
	}
}

// A missing or unreadable config is not fatal — the dispatch still goes out, it
// just shows a trust prompt.
func TestInheritTrustDegradesQuietly(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no .claude.json at all
	if InheritTrust("/repos/shop", "/state/worktrees/shop/z") {
		t.Error("with no config there is nothing to inherit")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if InheritTrust("/repos/shop", "/state/worktrees/shop/z") {
		t.Error("a corrupt config must not be treated as trust")
	}
	// And it must be left exactly as found, not half-rewritten.
	if b, _ := os.ReadFile(filepath.Join(home, ".claude.json")); string(b) != "{not json" {
		t.Errorf("a config we could not parse was modified: %q", b)
	}
}

// Re-dispatching the same feature must not double-write or flip anything back.
func TestInheritTrustIsIdempotent(t *testing.T) {
	home := writeClaudeConfig(t, map[string]any{
		"/repos/shop": map[string]any{"hasTrustDialogAccepted": true},
	})
	wt := "/state/worktrees/shop/again"
	if !InheritTrust("/repos/shop", wt) {
		t.Fatal("first call should inherit")
	}
	if !InheritTrust("/repos/shop", wt) {
		t.Fatal("second call should report the worktree is already trusted")
	}
	if v, _ := readProjects(t, home)[wt]["hasTrustDialogAccepted"].(bool); !v {
		t.Error("trust was lost on the second pass")
	}
}
