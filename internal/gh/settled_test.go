package gh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// resetForTest empties the in-memory cache, as a new process starts.
func resetForTest(t *testing.T) {
	t.Helper()
	mu.Lock()
	cache = map[string]*entry{}
	mu.Unlock()
}

// A settled answer — a merged PR's checks — outlives the process for what is
// left of its TTL, so a cockpit opened again does not ask for it again. Shorter
// TTLs are not kept: those are the reads meant to be asked again.
func TestSettledAnswersSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gh-settled.json")
	t.Cleanup(func() {
		resetForTest(t)
		mu.Lock()
		diskPath, disk = "", map[string]diskEntry{}
		mu.Unlock()
	})
	resetForTest(t)
	KeepSettled(path)

	calls := 0
	fetch := func() Checks { calls++; return Checks{Passed: 3, Total: 3} }
	if got := memo("checks:repo:1", SettledTTL, fetch); got.Passed != 3 || calls != 1 {
		t.Fatalf("first read = %+v after %d calls", got, calls)
	}
	memo("checks:repo:2", PRTTL, func() Checks { return Checks{Total: 9} })

	// A new launch: nothing in memory, the file read back.
	resetForTest(t)
	KeepSettled(path)
	if got := memo("checks:repo:1", SettledTTL, fetch); got.Passed != 3 || got.Total != 3 || calls != 1 {
		t.Errorf("after a restart = %+v with %d calls, want the kept answer and no new request", got, calls)
	}
	mu.Lock()
	_, kept := disk["checks:repo:2"]
	mu.Unlock()
	if kept {
		t.Error("an answer with a short TTL was kept across launches")
	}

	// Dropping the cache is the human having just changed something; the file
	// goes with it, or the next launch would read the old answer back.
	InvalidateCache()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the kept answers outlived InvalidateCache: %v", err)
	}
	memo("checks:repo:1", SettledTTL, fetch)
	if calls != 2 {
		t.Errorf("after InvalidateCache the answer should be asked for again (calls = %d)", calls)
	}
}

// Nothing is held longer than it would have been in memory.
func TestAnExpiredSettledAnswerIsAskedAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gh-settled.json")
	t.Cleanup(func() {
		resetForTest(t)
		mu.Lock()
		diskPath, disk = "", map[string]diskEntry{}
		mu.Unlock()
	})
	old, _ := json.Marshal(map[string]diskEntry{
		"review:repo:7": {At: time.Now().Add(-SettledTTL - time.Minute), Val: json.RawMessage(`{"Approvals":1}`)},
	})
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	resetForTest(t)
	KeepSettled(path)
	asked := false
	memo("review:repo:7", SettledTTL, func() Review { asked = true; return Review{} })
	if !asked {
		t.Error("an answer older than SettledTTL was served from disk")
	}
}
