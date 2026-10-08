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

// The appearance file is the other top-level key added for this machine, and
// it has the same "written before the first table" hazard. ~ is expanded on
// the way out, because the human types the path their desktop writes to and
// `os.ReadFile` has never heard of a home directory.
func TestAppearanceFileSurvivesSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := Save(&Config{Roots: []string{dir}, AppearanceFile: "~/.config/tmux/mode"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AppearanceFile != "~/.config/tmux/mode" {
		t.Errorf("appearance_file = %q, want what was saved", got.AppearanceFile)
	}
	if want := dir + "/.config/tmux/mode"; got.AppearancePath() != want {
		t.Errorf("AppearancePath = %q, want %q", got.AppearancePath(), want)
	}
	// Naming none is the ordinary case and must stay empty rather than
	// becoming a path nobody asked for.
	if (&Config{}).AppearancePath() != "" {
		t.Error("an unset appearance file resolved to a path")
	}
	var nilCfg *Config
	if nilCfg.AppearancePath() != "" {
		t.Error("a nil config resolved to a path")
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
