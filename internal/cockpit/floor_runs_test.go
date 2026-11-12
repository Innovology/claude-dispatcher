//go:build !windows

package cockpit

import (
	"os"
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
)

// The runs strip labels each of a branch's last three workflow runs by what
// it came to. It used to be exercised only by accident: records with no branch
// asked the real gh for "every branch's" runs — an empty --branch is no filter
// — and got this repository's. A branch is asked about by name now, so the
// strip is tested against a gh that answers for one.
func TestFloorRunsLabelsEachRunByItsOutcome(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho '[" +
		`{"workflowName":"CI","status":"in_progress","conclusion":"","createdAt":"2026-11-06T10:00:00Z"},` +
		`{"workflowName":"CI","status":"completed","conclusion":"success","createdAt":"2026-11-06T09:00:00Z"},` +
		`{"workflowName":"Deploy","status":"completed","conclusion":"failure","createdAt":"2026-11-06T08:00:00Z"},` +
		`{"workflowName":"Lint","status":"completed","conclusion":"skipped","createdAt":"2026-11-06T07:00:00Z"}` +
		"]'\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	gh.InvalidateCache()
	t.Cleanup(gh.InvalidateCache)

	runs := floorRuns(&state.Dispatch{RepoPath: t.TempDir(), Branch: "feature/x"})
	if len(runs) != 3 {
		t.Fatalf("runs = %+v, want the last three", runs)
	}
	for i, want := range []string{"● running", "✓ passed", "✗ failure"} {
		if runs[i].state != want {
			t.Errorf("run %d = %q, want %q", i, runs[i].state, want)
		}
	}
	if got := floorRuns(&state.Dispatch{RepoPath: t.TempDir()}); len(got) != 0 {
		t.Errorf("a record with no branch has runs: %+v", got)
	}
}
