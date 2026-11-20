package repos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	mk := func(parts ...string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(append([]string{root}, parts...)...), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("alpha", ".git")
	mk("group", "sub", "beta", ".git")
	mk("alpha", "nested", ".git")      // inside a repo: not descended into
	mk("node_modules", "dep", ".git")  // skipped dir
	mk(".hidden", "gamma", ".git")     // hidden dir skipped
	mk("group", "a", "b", "c", ".git") // beyond maxDepth
	mk("plain", "not-a-repo")

	cfg := &config.Config{
		Roots:    []string{root},
		Products: map[string][]string{"prod": {"alpha"}},
	}
	got := Discover(cfg)

	if len(got) != 2 {
		names := []string{}
		for _, r := range got {
			names = append(names, r.Name)
		}
		t.Fatalf("expected [alpha beta], got %v", names)
	}
	if got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Errorf("expected sorted [alpha beta], got [%s %s]", got[0].Name, got[1].Name)
	}
	if got[0].Product != "prod" {
		t.Errorf("alpha should map to product 'prod', got %q", got[0].Product)
	}
	if got[1].Product != "" {
		t.Errorf("beta should have no product, got %q", got[1].Product)
	}
}

// A repo's siblings are the other repos found in its product: where one of its
// dispatchers can hand work on to. A product entry naming a repo no root reaches
// names nowhere a dispatch could go, and a repo in no product has none.
func TestDiscoverNamesEachReposSiblings(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"api", "app", "web", "loner"} {
		if err := os.MkdirAll(filepath.Join(root, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{
		Roots:    []string{root},
		Products: map[string][]string{"shop": {"web", "api", "app", "gone"}},
	}
	got := map[string][]string{}
	for _, r := range Discover(cfg) {
		got[r.Name] = r.Siblings
	}
	want := map[string]string{"api": "app web", "app": "api web", "web": "api app", "loner": ""}
	for name, w := range want {
		if g := strings.Join(got[name], " "); g != w {
			t.Errorf("%s's siblings = %q, want %q", name, g, w)
		}
	}
}
