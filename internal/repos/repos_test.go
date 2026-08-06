package repos

import (
	"os"
	"path/filepath"
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
