package cockpit

// cluster_test.go covers the assignment editor. It is the only screen that
// writes to the user's config, so what it persists — and what it refuses to —
// is pinned here rather than left to a render test.

import (
	"os"
	"path/filepath"
	"strings"
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

// The editor's two unassign keys have to survive the global router, which gets
// every key first. "Start over" was a capital U and never arrived: handleKey
// resolves U as the upgrade key before any lens is asked, so the one key the
// help sheet promised here opened an upgrade confirm instead. And `u` was
// stolen from this screen whenever a triage act had left an undo behind.
func TestClUnassignKeysSurviveTheGlobalRouter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// A fresh editor per key: clAssign writes through the working map, which a
	// copied model shares with the one it was copied from.
	fresh := func() model {
		m := clFixture(t)
		m.lens = "products"
		m.clMap = map[string]string{"acme-api": "acme", "acme-web": "acme", "orbit-billing": "orbit"}
		m.undo = "ship widget" // a pending undo must not eat this screen's `u`
		return m
	}

	// `u` takes the cursor row out of its product and nothing else.
	m := press(fresh(), "u")
	if m.undo != "ship widget" {
		t.Error("u undid a triage act instead of unassigning a repo")
	}
	if got := m.clMap["acme-api"]; got != "" {
		t.Errorf("u left acme-api in %q", got)
	}
	if m.clMap["acme-web"] != "acme" {
		t.Error("u unassigned more than the row it was on")
	}

	// ctrl+u is start over: every repo out of every product.
	m = press(fresh(), "ctrl+u")
	for _, r := range m.clRepos() {
		if r.product != "" {
			t.Errorf("start over left %s in %q", r.name, r.product)
		}
	}
	if !strings.Contains(m.notice, "every repo unassigned") {
		t.Errorf("start over notice = %q", m.notice)
	}

	// U is the upgrade key here as everywhere — never a second unassign.
	m = press(fresh(), "U")
	if m.clMap["acme-api"] != "acme" || m.clMap["orbit-billing"] != "orbit" {
		t.Error("U unassigned something in the editor")
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

// TestBacklogCtrlDActuallyDispatches guards a notice that lied. ctrl+d used to
// announce "dispatched N tickets · one session each" and launch nothing at all,
// so the user believed work was out when none was. Every ticket it claims must
// have a command behind it.
func TestBacklogCtrlDActuallyDispatches(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	backlogTickets = []ticket{
		{id: "T-1", title: "first", repo: "acme-api", prompt: "do one"},
		{id: "T-2", title: "second", repo: "acme-web", prompt: "do two"},
		{id: "T-3", title: "taken", repo: "acme-hq", prompt: "do three", taken: "someone"},
	}

	m := newModel()
	m.lens = "backlog"
	m.picked = map[string]bool{"T-1": true, "T-2": true}
	mm, cmd := m.updateBacklog("ctrl+d")
	if cmd == nil {
		t.Fatal("ctrl+d claimed a dispatch but returned no command")
	}
	if !strings.Contains(mm.notice, "2") {
		t.Errorf("notice should count what it launched, got %q", mm.notice)
	}
	if len(mm.picked) != 0 {
		t.Error("picks should clear once dispatched")
	}

	// A ticket that already has a dispatcher is skipped, and said so.
	m2 := newModel()
	m2.lens = "backlog"
	m2.picked = map[string]bool{"T-3": true}
	mm2, cmd2 := m2.updateBacklog("ctrl+d")
	if cmd2 != nil {
		t.Error("a ticket that already has a dispatcher must not be launched again")
	}
	if !strings.Contains(mm2.notice, "already") {
		t.Errorf("notice should explain why nothing went out, got %q", mm2.notice)
	}

	// Nothing picked is not a dispatch either.
	m3 := newModel()
	m3.lens = "backlog"
	mm3, cmd3 := m3.updateBacklog("ctrl+d")
	if cmd3 != nil || !strings.Contains(mm3.notice, "nothing") {
		t.Errorf("empty pick set: cmd=%v notice=%q", cmd3 != nil, mm3.notice)
	}
}
