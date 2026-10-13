package cockpit

import (
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
