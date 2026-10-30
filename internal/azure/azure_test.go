package azure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAz puts an `az` on PATH that replies with out, and configures the org and
// project, so WorkItems runs end to end without Azure behind it. It returns a
// func reading the arguments the fake was called with.
func fakeAz(t *testing.T, out string) func() string {
	t.Helper()
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + args + "\ncat <<'EOF'\n" + out + "\nEOF\n"
	if err := os.WriteFile(filepath.Join(dir, "az"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AZURE_DEVOPS_ORG", "https://dev.azure.com/acme")
	t.Setenv("AZURE_DEVOPS_PROJECT", "shop")
	return func() string {
		b, _ := os.ReadFile(args)
		return string(b)
	}
}

// A work item carries when it was filed and when it last changed, because the
// backlog sorts by both. Before, an Azure row had no date at all: a blank age,
// and every one of them sorted as if filed at the epoch.
func TestWorkItemsCarryTheirDates(t *testing.T) {
	args := fakeAz(t, `[{"id":42,"fields":{"System.Title":"Fix login","System.State":"Active",
  "Microsoft.VSTS.Common.Priority":2,"System.AreaPath":"shop\\web",
  "System.CreatedDate":"2026-07-01T08:00:00.123Z","System.ChangedDate":"2026-08-19T10:00:00Z"}},
 {"id":43,"fields":{"System.Title":"No dates","System.State":"New"}}]`)

	items, err := WorkItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	it := items[0]
	if it.ID != 42 || it.Title != "Fix login" || it.State != "Active" || it.Priority != "high" {
		t.Errorf("item = %+v", it)
	}
	if !it.Created.Equal(time.Date(2026, 7, 1, 8, 0, 0, 123e6, time.UTC)) {
		t.Errorf("Created = %v", it.Created)
	}
	if !it.Changed.Equal(time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("Changed = %v", it.Changed)
	}
	if !items[1].Created.IsZero() || !items[1].Changed.IsZero() || items[1].Priority != "med" {
		t.Errorf("an item with no dates reads as none: %+v", items[1])
	}
	if got := args(); !strings.Contains(got, "System.CreatedDate") || !strings.Contains(got, "System.ChangedDate") {
		t.Errorf("the query does not ask for the dates: %s", got)
	}
}

// A response that is not the JSON the query promises is no signal, not a
// crash and not an empty board.
func TestWorkItemsRefusesWhatItCannotParse(t *testing.T) {
	fakeAz(t, `not json`)
	if items, err := WorkItems(); err == nil || items != nil {
		t.Errorf("WorkItems() = %v, %v; want an error", items, err)
	}
}

// Unconfigured is silent: no org or project means nothing is asked.
func TestWorkItemsUnconfiguredAsksNothing(t *testing.T) {
	fakeAz(t, `[]`)
	t.Setenv("AZURE_DEVOPS_PROJECT", "")
	if Configured() {
		t.Fatal("no project is not configured")
	}
	if items, err := WorkItems(); items != nil || err != nil {
		t.Errorf("WorkItems() = %v, %v; want nil, nil", items, err)
	}
}

// A date the strict decoder would refuse costs that date, not the board. One
// work item from Azure DevOps Server with no zone on its date used to fail the
// whole response, and every Azure ticket left the backlog with it.
func TestAnOddDateCostsTheDateNotTheBoard(t *testing.T) {
	fakeAz(t, `[{"id":1,"fields":{"System.Title":"no zone","System.CreatedDate":"2024-05-01T10:20:30.12"}},
 {"id":2,"fields":{"System.Title":"garbled","System.ChangedDate":"last tuesday"}}]`)
	items, err := WorkItems()
	if err != nil || len(items) != 2 {
		t.Fatalf("WorkItems() = %v, %v; want both items", items, err)
	}
	if want := time.Date(2024, 5, 1, 10, 20, 30, 120e6, time.UTC); !items[0].Created.Equal(want) {
		t.Errorf("a date with no zone is UTC: %v", items[0].Created)
	}
	if !items[1].Changed.IsZero() {
		t.Errorf("an unreadable date is no date: %v", items[1].Changed)
	}
}
