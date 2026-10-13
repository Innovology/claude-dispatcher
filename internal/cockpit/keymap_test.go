package cockpit

// keymap_test.go covers the cockpit's half of remappable keys: that a rebind
// actually reaches the lens, that the old key stops working, and that `?` says
// what the keys really are.

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/keymap"
)

func keymapFixture(t *testing.T, overrides map[string]string) model {
	t.Helper()
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	reposByProduct = map[string][]repoRef{
		clUnassigned: {
			{name: "acme-api", forge: "gh", last: "1d", path: "/src/acme-api"},
			{name: "acme-web", forge: "gh", last: "9d", path: "/src/acme-web"},
		},
	}
	km, err := keymap.New(overrides)
	if err != nil {
		t.Fatalf("keymap.New(%v): %v", overrides, err)
	}
	m := newModel()
	m.cfg = &config.Config{Products: map[string][]string{}}
	m.keys = km
	m.width = 200
	return m
}

// The point of the whole layer: a rebound key runs the action, and the key it
// used to be on does not.
func TestRebindReachesTheEditor(t *testing.T) {
	m := keymapFixture(t, map[string]string{"editor.where": "w"})
	m.clOpen = true

	m2, _, _ := m.updateCluster("w")
	if !m2.clExpanded["acme-api"] {
		t.Error("the rebound key should unfold the row")
	}

	// A fresh model, because clExpanded is a map and the copies above share it —
	// reusing m here would be asserting against the previous keypress's writes.
	fresh := keymapFixture(t, map[string]string{"editor.where": "w"})
	fresh.clOpen = true
	m3, _, _ := fresh.updateCluster("p")
	if m3.clExpanded["acme-api"] {
		t.Error("the default key should no longer unfold anything once rebound away")
	}
	if m3.clFoldRow != "" {
		t.Error("the old key should not take the keyboard either")
	}
}

// Rebinding one action must not disturb the rest of its scope.
func TestRebindLeavesTheOtherKeysAlone(t *testing.T) {
	m := keymapFixture(t, map[string]string{"editor.where": "w"})
	m.clOpen = true

	m2, _, _ := m.updateCluster("j")
	if m2.clRepo != 1 {
		t.Errorf("j should still move the cursor, got %d", m2.clRepo)
	}
	m3, _, _ := m2.updateCluster("space")
	if !m3.clMarked["acme-web"] {
		t.Error("space should still mark")
	}
}

// `?` is where someone goes to find out what their keys are, so it is built
// from the keymap rather than written beside it.
func TestHelpSheetShowsTheLiveBindings(t *testing.T) {
	// The fold's row, by what it says rather than by a letter that appears in
	// half the keys on the sheet.
	foldKey := func(m model) string {
		for _, r := range sectionRows(m, "products · lens 2") {
			if strings.Contains(r.d, "unfold") {
				return r.k
			}
		}
		return ""
	}
	if got := foldKey(keymapFixture(t, nil)); got != "p" {
		t.Errorf("the default sheet shows %q for the fold, want p", got)
	}
	if got := foldKey(keymapFixture(t, map[string]string{"editor.where": "w"})); got != "w" {
		t.Errorf("the sheet shows %q for the rebound fold, want w", got)
	}
}

// Every binding that carries a help line has to reach the sheet — a new action
// must not be invisible because a section name was left off an ordering list.
func TestEveryDocumentedBindingIsOnTheSheet(t *testing.T) {
	m := keymapFixture(t, nil)
	printed := map[string]bool{}
	for _, sec := range m.helpSections() {
		for _, r := range sec.keys {
			printed[r.d] = true
		}
	}
	for _, b := range keymap.Defaults {
		if b.Help == "" {
			continue
		}
		if !printed[b.Help] {
			t.Errorf("action %q is documented but never printed by ?", b.Action)
		}
	}
}

// sectionRows is one section of m's help sheet.
func sectionRows(m model, name string) []helpRow {
	for _, s := range m.helpSections() {
		if s.section == name {
			return s.keys
		}
	}
	return nil
}
