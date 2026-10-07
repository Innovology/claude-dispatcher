package config

import (
	"os"
	"path/filepath"
	"testing"
)

// `shell` is a top-level key, so it has to be written before the first table or
// TOML files it under whichever one precedes it. Saving and loading it back is
// the only test that can tell the difference.
func TestShellSurvivesSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if filepath.Dir(Path()) != Dir() {
		t.Fatalf("config path %q is not under %q", Path(), Dir())
	}

	if err := Save(&Config{Roots: []string{dir}, Shell: "/etc/profiles/per-user/me/bin/nu"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Shell != "/etc/profiles/per-user/me/bin/nu" {
		t.Errorf("shell = %q, want the one that was saved", got.Shell)
	}

	// Unset stays unset rather than becoming a guess at the human's shell:
	// empty means "ask the session's own tmux server", which is a different
	// answer from any path we could have written in.
	if err := Save(&Config{Roots: []string{dir}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if back.Shell != "" {
		t.Errorf("shell = %q, want empty", back.Shell)
	}
	raw, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if !containsLine(string(raw), "# shell = \"/usr/bin/fish\"") {
		t.Errorf("the written config does not show the key as a comment:\n%s", raw)
	}
}

func containsLine(s, want string) bool {
	for _, ln := range splitLines(s) {
		if ln == want {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
