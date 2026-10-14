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

// The product panel's tabs are the keymap's too: a rebound tab key opens the
// tab, and the key it left is swallowed rather than passed on to whatever the
// panel would have done with it.
func TestRebindReachesThePanelTabs(t *testing.T) {
	m := keymapFixture(t, map[string]string{"panel.sessions": "ctrl+y"})
	products = []product{{name: "pp"}}
	m.lens = "product"
	m.height = 44

	if m = press(m, "Y"); m.rightTab == "sessions" {
		t.Error("the key the sessions tab was moved off still opens it")
	}
	if m = press(m, "ctrl+y"); m.rightTab != "sessions" {
		t.Errorf("the rebound key opened %q, want the sessions tab", m.rightTab)
	}
}

// The list lenses resolve their keys too. backlog and decisions share the list
// scope, so one rebind moves the key on both, and the key it left goes nowhere
// on either — not to the cursor it used to move.
func TestRebindReachesTheListLenses(t *testing.T) {
	m := keymapFixture(t, map[string]string{"list.down": "ctrl+j"})
	installFixture(t)
	decisionRepoOrder = []string{"alpha-api"}
	decisions = map[string][]decision{"alpha-api": {
		{id: "0001", title: "first", status: "accepted"},
		{id: "0002", title: "second", status: "proposed"},
	}}

	m.lens = "backlog"
	if m = press(m, "j"); m.backlogCursor != 0 {
		t.Error("j still moves the backlog after list.down left it")
	}
	if m = press(m, "ctrl+j"); m.backlogCursor != 1 {
		t.Errorf("backlog cursor = %d after the rebound key, want 1", m.backlogCursor)
	}

	m.lens = "decisions"
	if m = press(m, "j"); m.decCursor != 0 {
		t.Error("j still moves the decisions after list.down left it")
	}
	if m = press(m, "ctrl+j"); m.decCursor != 1 {
		t.Errorf("decisions cursor = %d after the rebound key, want 1", m.decCursor)
	}
}

// The assignment editor's footer is built from the keymap, so a rebound key is
// the one it names — a footer reciting the shipped keys would send the human to
// a key that no longer does anything. While a product is being named, every
// key is text, and the footer says only how to finish.
func TestTheEditorFooterNamesTheKeysAsBound(t *testing.T) {
	m := keymapFixture(t, map[string]string{"editor.mark": "ctrl+t"})
	m.lens, m.clOpen = "products", true

	got := m.footerHelp()
	for _, want := range []string{prettyKey("ctrl+t") + " mark", "assign", "unassign", "start over", "done"} {
		if !strings.Contains(got, want) {
			t.Errorf("editor footer is missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "space mark") {
		t.Errorf("the footer still names the key mark was moved off: %q", got)
	}

	m.clNaming = true
	if got := m.footerHelp(); got != "type the name · enter creates it · esc cancels" {
		t.Errorf("naming footer = %q", got)
	}
}
