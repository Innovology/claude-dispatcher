package cockpit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

// TestSettingsEditPersists opens the settings editor, edits the weekly token
// budget, commits it, and checks the value reached config and re-saves. HOME is
// sandboxed so it never touches the real config.toml.
func TestSettingsEditPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{Roots: []string{"~/repos"}}
	m.lens = "products" // ',' is swallowed by the triage lens; the palette is its way in

	m = press(m, ",") // open settings
	if m.settings == nil {
		t.Fatal("',' did not open settings")
	}
	if strings.TrimSpace(m.View()) == "" {
		t.Fatal("settings render empty")
	}

	// Move to the Azure project field (index 3) and edit it.
	for i := 0; i < 3; i++ {
		m = press(m, "j")
	}
	m = press(m, "enter") // begin editing
	if !m.settings.editing {
		t.Fatal("enter did not begin editing")
	}
	for _, ch := range strings.Split("boards", "") {
		m = press(m, ch)
	}
	m = press(m, "enter") // commit

	if m.cfg.AzureProject != "boards" {
		t.Fatalf("azure project not committed: got %q", m.cfg.AzureProject)
	}
	// It should have been saved to config.toml under the sandboxed HOME.
	got, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.AzureProject != "boards" {
		t.Errorf("saved config azure project = %q, want boards", got.AzureProject)
	}

	// esc closes the editor.
	m = press(m, "esc")
	if m.settings != nil {
		t.Error("esc did not close settings")
	}
}

// settingsAt opens the settings editor on the field with key, the way the
// human gets there: the palette-free `,` and then j down to it.
func settingsAt(t *testing.T, m model, key string) model {
	t.Helper()
	m.lens = "products" // ',' is swallowed by the triage lens
	m = press(m, ",")
	if m.settings == nil {
		t.Fatal("',' did not open settings")
	}
	for i, f := range m.settings.fields {
		if f.key == key {
			for range i {
				m = press(m, "j")
			}
			return m
		}
	}
	t.Fatalf("settings has no %q field", key)
	return m
}

// A prefix git would refuse is refused where it is typed, and the editor stays
// open on it: what was typed is still there to fix, and nothing reached the
// config. Closing the field on a refusal would make the human type it again.
func TestSettingsRefusesABadPrefixAndKeepsWhatWasTyped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{}
	m = settingsAt(t, m, "branch_prefix")

	m = press(m, "enter")
	m.settings.input.SetValue("a..b/")
	m = press(m, "enter")

	if !strings.HasPrefix(m.notice, "branch prefix:") {
		t.Errorf("notice = %q, want the refusal named for the field", m.notice)
	}
	if !m.settings.editing || m.settings.input.Value() != "a..b/" {
		t.Errorf("the refusal closed the field (editing=%v, value=%q)", m.settings.editing, m.settings.input.Value())
	}
	if m.cfg.BranchPrefix != nil {
		t.Errorf("a refused prefix reached the config: %q", *m.cfg.BranchPrefix)
	}
}

// While a field is being edited its row shows the input, not the saved value,
// and a commit leaves a flash naming what was saved. Without the first the
// human types blind; without the second "enter" looks like it did nothing.
func TestSettingsShowsTheFieldBeingTypedAndWhatWasSaved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{AzureOrg: "https://dev.azure.com/old"}
	m = settingsAt(t, m, "azure_org")

	m = press(m, "enter")
	m.settings.input.SetValue("https://dev.azure.com/typing")
	if v := m.View(); !strings.Contains(v, "typing") || strings.Contains(v, "/old") {
		t.Error("the row being edited does not show what is being typed")
	}
	m = press(m, "enter")
	if !strings.Contains(m.View(), "✓ Azure DevOps org saved") {
		t.Error("a committed field leaves no saved flash")
	}
	// Moving off the field clears it: the flash is about that field.
	m = press(m, "j")
	if strings.Contains(m.View(), "✓ Azure DevOps org saved") {
		t.Error("the saved flash outlived a move to another field")
	}
}

// A save that fails says why, on a typed field and on the theme cycle alike,
// and does not claim the setting was saved. The config dir is a file here,
// which is what an unwritable config looks like to MkdirAll.
func TestASettingsSaveThatFailsSaysSo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(config.Dir()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Dir(), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setTheme(darkTheme) })

	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{}
	m = settingsAt(t, m, "azure_project")
	m = press(m, "enter")
	m.settings.input.SetValue("boards")
	m = press(m, "enter")
	if !strings.HasPrefix(m.notice, "save failed: ") || m.settings.saved != "" {
		t.Errorf("a failed save reads notice=%q saved=%q", m.notice, m.settings.saved)
	}

	m.notice = ""
	m.themeMode = lightTheme.name
	m = settingsAt(t, press(m, "esc"), "theme")
	m = press(m, "enter")
	if m.themeMode != darkTheme.name {
		t.Fatalf("enter on theme went to %q, want the next mode", m.themeMode)
	}
	if !strings.HasPrefix(m.notice, "save failed: ") || m.settings.saved != "" {
		t.Errorf("a failed theme save reads notice=%q saved=%q", m.notice, m.settings.saved)
	}
}

// `e` hands config.toml to the editor from settings, writing the commented
// template first when there is no file yet, so the editor never opens on an
// empty buffer. Settings closes behind it: coming back re-reads the file.
func TestSettingsEOpensTheWholeFileInTheEditor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VISUAL", "true")
	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{}
	m = settingsAt(t, m, "roots")

	next, cmd := m.handleKey("e")
	m = next.(model)
	if cmd == nil {
		t.Fatal("e started no editor")
	}
	if m.settings != nil {
		t.Error("settings stayed open under the editor")
	}
	if _, err := os.Stat(config.Path()); err != nil {
		t.Errorf("no config.toml was written for the editor to open: %v", err)
	}
}
