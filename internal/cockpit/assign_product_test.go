package cockpit

// assign_product_test.go pins the one promise the assignment editor makes: what
// it saves is what every lens then groups by. It was broken in both directions —
// the save never reached the running cockpit, and a dispatch carried the product
// it was launched under regardless of what the config said afterwards — so a repo
// assigned to a product appeared under it on the products lens while triage went
// on calling the same work unassigned.

import (
	"os"
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// The editor writes the config file, and the cockpit re-reads nothing: the
// object it hands every collector is the one loaded at startup. Assigning has to
// publish into it, or the reload that follows regroups by the mapping the human
// just replaced.
func TestClPersistPublishesToTheRunningConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)

	mm, cmd := m.clAssign([]string{"acme-api"}, "acme")
	if cmd == nil {
		t.Fatal("assigning should return a save command")
	}
	cmd()

	if got := mm.cfg.ProductFor("acme-api"); got != "acme" {
		t.Errorf("the cockpit's own config still says %q — the next reload regroups by the old mapping", got)
	}
	// The same object the collectors read, not a copy left behind by the save.
	ctx := &collectCtx{cfg: mm.cfg}
	if got := ctx.productFor(&state.Dispatch{RepoName: "acme-api"}); got != "acme" {
		t.Errorf("triage resolves %q for a repo just assigned to acme", got)
	}
}

// Unassigning has to reach the running config too, or a repo taken out of a
// product goes on being counted in it until the cockpit is restarted.
func TestClUnassignPublishesToTheRunningConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.cfg = &config.Config{Products: map[string][]string{"acme": {"acme-api"}}}

	mm, cmd := m.clAssign([]string{"acme-api"}, "")
	if cmd != nil {
		cmd()
	}
	if got := mm.cfg.ProductFor("acme-api"); got != "" {
		t.Errorf("the cockpit's own config still has acme-api under %q", got)
	}
}

// A write that fails must leave the cockpit grouping by what is still on disk.
// Publishing regardless would put the screen and the file permanently at odds,
// under a notice saying nothing had been saved.
func TestClPersistDoesNotPublishAFailedSave(t *testing.T) {
	// A file where the config directory has to go: every write below fails.
	blocker := filepath.Join(t.TempDir(), "home")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", blocker)

	m := clFixture(t)
	mm, cmd := m.clAssign([]string{"acme-api"}, "acme")
	msg, ok := cmd().(actionMsg)
	if !ok || msg.notice == "" {
		t.Fatalf("a failed save must say so, got %#v", msg)
	}
	if got := mm.cfg.ProductFor("acme-api"); got != "" {
		t.Errorf("nothing was written, but the cockpit now groups acme-api under %q", got)
	}
}

// The record's product is what it was launched under. The config is what the
// human has said since, so it wins — otherwise reassigning a repo moves it on
// the products lens (which reads the config) and leaves every dispatch of it
// where it was on triage (which read the record).
func TestProductForFollowsTheConfigNotTheRecord(t *testing.T) {
	ctx := &collectCtx{cfg: &config.Config{
		Products: map[string][]string{"shop": {"shop-api"}, "warehouse": {}},
	}}

	rec := &state.Dispatch{RepoName: "shop-api", Product: "warehouse"}
	if got := ctx.productFor(rec); got != "shop" {
		t.Errorf("a repo reassigned to shop resolves %q", got)
	}
	// Taken out of every product: the record remembering one does not put it back.
	loose := &state.Dispatch{RepoName: "loose-repo", Product: "shop"}
	if got := ctx.productFor(loose); got != "unassigned" {
		t.Errorf("an unassigned repo resolves %q, want unassigned", got)
	}
}

// The two lenses must resolve a record the same way or they will disagree again
// the next time the mapping changes: one function, both callers.
func TestTriageAndProductsGroupARecordAlike(t *testing.T) {
	cfg := &config.Config{Products: map[string][]string{"shop": {"shop-api"}}}
	ctx := &collectCtx{cfg: cfg, records: []*state.Dispatch{
		{ID: "1", RepoName: "shop-api", Product: "retired", Feature: "one"},
		{ID: "2", RepoName: "loose", Feature: "two"},
	}}
	var s snapshot
	collectProducts(ctx, &s)

	// shop-api's dispatch counts towards shop, not towards the product the
	// record was launched under — the same answer triage's productFor gives.
	if got := ctx.productFor(ctx.records[0]); got != "shop" {
		t.Errorf("triage puts the shop-api dispatch in %q", got)
	}
	for _, p := range s.products {
		switch p.name {
		case "shop":
			if p.inflight != 1 {
				t.Errorf("shop should carry the shop-api dispatch, inflight = %d", p.inflight)
			}
		case "unassigned":
			if p.inflight != 1 {
				t.Errorf("unassigned should carry the loose dispatch, inflight = %d", p.inflight)
			}
		}
	}
}
