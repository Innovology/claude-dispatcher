package cockpit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

// $VISUAL, then $EDITOR, then vi — with arguments split off the way git reads
// core.editor, and anything not installed skipped rather than exec'd to fail.
func TestEditorCommand(t *testing.T) {
	t.Setenv("VISUAL", "no-such-editor-xyz --wait")
	t.Setenv("EDITOR", "sh -e")
	cmd := editorCommand("/tmp/config.toml")
	if cmd == nil || filepath.Base(cmd.Path) != "sh" || strings.Join(cmd.Args[1:], " ") != "-e /tmp/config.toml" {
		t.Fatalf("editorCommand = %v", cmd)
	}
}

// Coming back from the editor applies the file — here a rebinding and a new
// scan root — and a file that no longer parses keeps the config already
// running, since the cockpit's own saves rewrite the file from what it holds.
func TestConfigEditedReloadsAndKeepsTheOldOnABadFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if err := os.MkdirAll(config.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(s string) {
		if err := os.WriteFile(config.Path(), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newModel()
	m.cfg = &config.Config{Roots: []string{"/old"}}

	write("roots = [\"/new\"]\n[keys]\n\"settings\" = \"S\"\n")
	m, _ = m.onConfigEdited(configEditedMsg{})
	if len(m.cfg.Roots) != 1 || m.cfg.Roots[0] != "/new" {
		t.Errorf("roots after the edit = %v", m.cfg.Roots)
	}
	if m.keys.KeyFor("settings") != "S" {
		t.Errorf("the [keys] rebinding was not applied: settings is %q", m.keys.KeyFor("settings"))
	}
	if !strings.Contains(m.notice, "reloaded") {
		t.Errorf("notice = %q", m.notice)
	}

	write("roots = [\"/half\"\n") // unterminated
	m, _ = m.onConfigEdited(configEditedMsg{})
	if m.cfg.Roots[0] != "/new" || !strings.Contains(m.notice, "did not load") {
		t.Errorf("a broken file replaced the running config: roots=%v notice=%q", m.cfg.Roots, m.notice)
	}
}

// An empty branch prefix is "none", and settings must not file it under "not
// set" — that is what an unset prefix (feature/) would be read as.
func TestSettingsShowsAnEmptyPrefixAsNone(t *testing.T) {
	m := newModel()
	m.width, m.height = 160, 40
	empty := ""
	m.cfg = &config.Config{BranchPrefix: &empty}
	m.settings = newSettings(m.cfg)
	for _, ln := range strings.Split(m.View(), "\n") {
		if strings.Contains(ln, "branch prefix") {
			if strings.Contains(ln, "not set") || !strings.Contains(ln, "none") {
				t.Errorf("branch prefix row = %q", ln)
			}
			return
		}
	}
	t.Error("settings has no branch prefix row")
}
