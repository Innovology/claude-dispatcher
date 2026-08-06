package state

import (
	"testing"
	"time"
)

func TestStatusPriorityOrdering(t *testing.T) {
	order := []Status{StatusBlocked, StatusNeedsInput, StatusLaunching,
		StatusWorking, StatusExited, StatusDone}
	for i := 1; i < len(order); i++ {
		if order[i-1].Priority() >= order[i].Priority() {
			t.Errorf("%s should outrank %s", order[i-1], order[i])
		}
	}
}

func TestSaveLoadAllSortsByUrgency(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	for _, d := range []*Dispatch{
		{ID: "w", Feature: "working", Status: StatusWorking},
		{ID: "b", Feature: "blocked", Status: StatusBlocked},
		{ID: "d", Feature: "done", Status: StatusDone},
		{ID: "n", Feature: "needs", Status: StatusNeedsInput},
	} {
		if err := Save(d); err != nil {
			t.Fatal(err)
		}
	}
	got := LoadAll()
	if len(got) != 4 {
		t.Fatalf("expected 4 records, got %d", len(got))
	}
	wantOrder := []string{"b", "n", "w", "d"}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Errorf("position %d: got %s, want %s", i, got[i].ID, id)
		}
	}
}

func TestSaveStampsUpdatedAt(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &Dispatch{ID: "x", Status: StatusWorking}
	before := time.Now()
	if err := Save(d); err != nil {
		t.Fatal(err)
	}
	if d.UpdatedAt.Before(before) {
		t.Error("UpdatedAt not stamped on save")
	}
}

func TestLoadAllSkipsCorruptRecords(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	if err := Save(&Dispatch{ID: "ok", Status: StatusWorking}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, DispatchesDir()+"/broken.json", "{not json")
	writeFile(t, DispatchesDir()+"/empty-id.json", "{}")
	got := LoadAll()
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("expected only the valid record, got %d", len(got))
	}
}
