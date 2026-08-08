package cockpit

// cluster_test.go covers the assignment editor. It is the only screen that
// writes to the user's config, so what it persists — and what it refuses to —
// is pinned here rather than left to a render test.

import (
	"os"
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/config"
)

// clFixture gives the editor a portfolio: three repos, none assigned.
func clFixture(t *testing.T) model {
	t.Helper()
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	reposByProduct = map[string][]repoRef{
		clUnassigned: {
			{name: "acme-api", forge: "gh", out: 2, last: "1d"},
			{name: "acme-web", forge: "gh", out: 0, last: "9d"},
			{name: "orbit-billing", forge: "ado", out: 1, last: "3d"},
		},
	}
	m := newModel()
	m.cfg = &config.Config{Products: map[string][]string{}}
	m.clOpen = true
	return m
}

func TestClReposListsUnassignedFirst(t *testing.T) {
	m := clFixture(t)
	m.clMap = map[string]string{"acme-web": "acme"}

	rows := m.clRepos()
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// The two with no product come first — they are why the screen is open.
	if rows[0].product != "" || rows[1].product != "" {
		t.Errorf("unassigned repos should sort first, got %+v", rows)
	}
	if rows[2].name != "acme-web" || rows[2].product != "acme" {
		t.Errorf("the working copy should win over the snapshot: %+v", rows[2])
	}
}

// Marks are the plural path; with nothing marked the cursor row is the target.
func TestClTargets(t *testing.T) {
	m := clFixture(t)
	if got := m.clTargets(); len(got) != 1 {
		t.Errorf("with nothing marked, the cursor row is the target, got %v", got)
	}
	m.clMarked = map[string]bool{"acme-api": true, "orbit-billing": true}
	got := m.clTargets()
	if len(got) != 2 || got[0] != "acme-api" || got[1] != "orbit-billing" {
		t.Errorf("marked repos are the targets, got %v", got)
	}
}

// "unassigned" is a display bucket, never a product: it must not become one.
func TestClProductsExcludesTheUnassignedBucket(t *testing.T) {
	m := clFixture(t)
	m.clMap = map[string]string{"acme-api": clUnassigned, "acme-web": "acme"}
	for _, p := range m.clProducts() {
		if p == clUnassigned {
			t.Fatalf("%q must never be offered as a product", clUnassigned)
		}
	}
}

func TestClAssignAndUnassignPersist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := clFixture(t)

	// Assign two repos, then run the returned command — that is what saves.
	m.clMarked = map[string]bool{"acme-api": true, "acme-web": true}
	mm, cmd := m.clAssign(m.clTargets(), "acme")
	if cmd == nil {
		t.Fatal("assigning should return a save command")
	}
	if msg, ok := cmd().(actionMsg); ok && msg.notice != "" {
		t.Fatalf("save reported: %s", msg.notice)
	}
	if len(mm.clMarked) != 0 {
		t.Error("marks should clear once they have been moved")
	}

	written, err := os.ReadFile(filepath.Join(home, ".config", "claude-dispatcher", "config.toml"))
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if got := string(written); !contains(got, `acme = ["acme-api", "acme-web"]`) {
		t.Errorf("config.toml does not carry the assignment:\n%s", got)
	}

	// Unassigning writes the product away again rather than leaving a stale key.
	mm2, cmd2 := mm.clAssign([]string{"acme-api", "acme-web"}, "")
	if cmd2 != nil {
		cmd2()
	}
	written, _ = os.ReadFile(filepath.Join(home, ".config", "claude-dispatcher", "config.toml"))
	if contains(string(written), "acme-api") {
		t.Errorf("unassigned repos should not remain in [products]:\n%s", written)
	}
	if mm2.notice == "" {
		t.Error("unassigning should say what it did")
	}
}

// Naming is a text field, so it must take a whole burst — the same hole that
// once made every input in this cockpit dead to typing at speed.
func TestClNamingTakesABurstAndCreatesTheProduct(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.clNaming = true

	// Update sets m.key from the message it is handling; do the same, or the
	// text falls back to the key *name* — which is exactly the path that must
	// never be trusted for input.
	burst := runes("acme")
	m.key = burst
	mm, _, _ := m.updateCluster(burst.String())
	if mm.clNewName != "acme" {
		t.Fatalf("burst dropped: clNewName = %q", mm.clNewName)
	}

	mm.clMarked = map[string]bool{"acme-api": true}
	mm2, cmd, _ := mm.updateCluster("enter")
	if cmd != nil {
		cmd()
	}
	if mm2.clNaming {
		t.Error("enter should leave naming mode")
	}
	if mm2.clMap["acme-api"] != "acme" {
		t.Errorf("the named product should take the marked repos: %v", mm2.clMap)
	}
	if !contains(mm2.notice, "created") {
		t.Errorf("notice = %q", mm2.notice)
	}
}

func TestClNamingRejectsEmptyAndReservedNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"", "   ", clUnassigned} {
		m := clFixture(t)
		m.clNaming, m.clNewName = true, name
		mm, _, _ := m.updateCluster("enter")
		if len(mm.clMap) != 0 {
			t.Errorf("name %q should create nothing, got %v", name, mm.clMap)
		}
	}
}

func TestClEscapeAndPaneToggle(t *testing.T) {
	m := clFixture(t)
	m.clMarked = map[string]bool{"acme-api": true}
	mm, _, _ := m.updateCluster("esc")
	if mm.clOpen || len(mm.clMarked) != 0 {
		t.Error("esc should close the editor and drop the marks")
	}

	m2, _, _ := m.updateCluster("tab")
	if m2.clPane != "products" {
		t.Errorf("tab should move to the product pane, got %q", m2.clPane)
	}
	m3, _, _ := m2.updateCluster("tab")
	if m3.clPane != "repos" {
		t.Errorf("tab should come back, got %q", m3.clPane)
	}
}

// space marks and advances, so a run of repos can be selected without moving
// the hand between keys.
func TestClSpaceMarksAndAdvances(t *testing.T) {
	m := clFixture(t)
	mm, _, _ := m.updateCluster(" ")
	if !mm.clMarked["acme-api"] {
		t.Error("space should mark the row under the cursor")
	}
	if mm.clRepo != 1 {
		t.Errorf("space should advance the cursor, got %d", mm.clRepo)
	}
	mm2, _, _ := mm.updateCluster(" ")
	mm3, _, _ := mm2.updateCluster("k")
	mm4, _, _ := mm3.updateCluster(" ")
	if mm4.clMarked["acme-web"] {
		t.Error("space on a marked row should unmark it")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
