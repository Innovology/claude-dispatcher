package cockpit

// fleet_run_test.go pins what a running row may claim about its own pull
// request. The forge signals are real (a fake `gh` on PATH answers them), so
// these tests exercise the same cqShipDetail path the cockpit runs, not a
// hand-built row.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
)

// fakeGHOnPath stands a `gh` on PATH that answers the batched open-PR list
// with one open PR whose single check has the given rollup state, and empties
// every other question. The gh package's memo cache is dropped around it so
// no other test's answers are served here and none of ours leak out.
func fakeGHOnPath(t *testing.T, rollupState string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  \"pr list --state open\"*) echo '[{\"number\":12,\"headRefName\":\"feature/retry-backoff\",\"statusCheckRollup\":[{\"state\":\"" + rollupState + "\"}]}]' ;;\n" +
		"  *) echo '[]' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	gh.InvalidateCache()
	t.Cleanup(gh.InvalidateCache)
}

// The regression this file exists for: a WORKING dispatcher with an open PR
// and green checks is busy — doing manual verification, or partway through
// its next step — and has not asked anyone to merge. It must not be marked
// amber, ranked above other running rows, or claimed to be waiting on a
// human. The moment it truly stops with that PR open, hookcmd flips the
// record to needs-input, floorState reads "review", and the queue row that
// results is where "approve a merge" is claimed.
func TestRunningRowWithGreenOpenPRIsNotEscalated(t *testing.T) {
	fakeGHOnPath(t, "SUCCESS")
	rec := &state.Dispatch{
		ID: "rec-1", Feature: "retry backoff", RepoName: "alpha-api",
		RepoPath: t.TempDir(), Branch: "feature/retry-backoff",
		Status: state.StatusWorking, PRNumber: 12, PRState: "OPEN",
		UpdatedAt: time.Now(),
	}
	floorBy := map[string]dispatch{"retry backoff": {feature: "retry backoff", forge: "gh"}}

	r, _ := fleetRunRow(&collectCtx{}, &snapshot{}, floorBy, map[string]int{}, rec)

	if r.signal != "green, unmerged" {
		t.Fatalf("signal = %q, want the SIGNAL cell to state the fact — the green path was not exercised", r.signal)
	}
	if r.tone != "normal" {
		t.Errorf("tone = %q: a working dispatcher is not waiting on a human, whatever its PR says", r.tone)
	}
	if r.rank != fleetRank("run", "normal") {
		t.Errorf("rank = %d, want %d — a green unmerged PR must not outrank other running rows", r.rank, fleetRank("run", "normal"))
	}
	if strings.Contains(r.why, "checks are green") {
		t.Errorf("why = %q claims a wait nobody is having", r.why)
	}
}

// And the same record one status later is the row that IS waiting on a human:
// the finished turn with its open, undeployed PR classifies as a review queue
// row, which the table's top half and its "approve a merge" ask already handle.
func TestStoppedGreenPRDispatcherIsAReviewQueueRow(t *testing.T) {
	rec := &state.Dispatch{
		Status: state.StatusNeedsInput, PRNumber: 12, PRState: "OPEN",
	}
	if st := floorState(rec); st != "review" {
		t.Fatalf("floorState = %q, want review — the queue row is where the merge ask lives", st)
	}
}
