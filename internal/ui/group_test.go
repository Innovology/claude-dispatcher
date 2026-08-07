package ui

import (
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

func TestGroupByProduct(t *testing.T) {
	d := func(id, product string, s state.Status) *state.Dispatch {
		return &state.Dispatch{ID: id, Product: product, Status: s}
	}
	// Input is urgency-sorted, as LoadAll delivers it.
	in := []*state.Dispatch{
		d("blocked-other", "", state.StatusBlocked),
		d("needs-acme", "acme", state.StatusNeedsInput),
		d("working-zeta", "zeta", state.StatusWorking),
		d("working-acme", "acme", state.StatusWorking),
		d("done-other", "", state.StatusDone),
	}
	got := groupByProduct(in)
	want := []string{
		// "other" holds the single most urgent dispatch, so it leads.
		"blocked-other", "done-other",
		"needs-acme", "working-acme",
		"working-zeta",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d dispatches, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d: got %s, want %s", i, got[i].ID, id)
		}
	}
}

func TestGroupByProductNamedBeforeOtherOnTies(t *testing.T) {
	in := []*state.Dispatch{
		{ID: "a", Product: "", Status: state.StatusWorking},
		{ID: "b", Product: "acme", Status: state.StatusWorking},
	}
	got := groupByProduct(in)
	if got[0].ID != "b" || got[1].ID != "a" {
		t.Errorf("named product should precede the unnamed bucket on equal urgency: %s, %s", got[0].ID, got[1].ID)
	}
}

func TestGroupRepos(t *testing.T) {
	in := []repos.Repo{
		{Name: "zoo"},
		{Name: "api", Product: "shop"},
		{Name: "adm"},
		{Name: "web", Product: "shop"},
		{Name: "app", Product: "aura"},
	}
	got := groupRepos(in)
	want := []string{"app", "api", "web", "adm", "zoo"}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("position %d: got %s, want %s", i, got[i].Name, name)
		}
	}
	if in[0].Name != "zoo" {
		t.Error("input slice must not be reordered in place")
	}
}
