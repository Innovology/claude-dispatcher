package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTranscript lays down a session transcript under a sandboxed HOME with
// `fhTokens` of opus usage an hour ago (counted in both the 5-hour and weekly
// windows) and an optional 429 rate-limit line `limitAgo` before now.
func writeTranscript(t *testing.T, home string, now time.Time, fhTokens int, limitAgo time.Duration) {
	t.Helper()
	proj := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines string
	lines += fmt.Sprintf(`{"type":"assistant","timestamp":%q,"session_id":"s1","message":{"model":"claude-opus-4-5","usage":{"input_tokens":%d,"output_tokens":0,"cache_read_input_tokens":9999999,"cache_creation_input_tokens":0}}}`+"\n",
		now.Add(-time.Hour).UTC().Format(time.RFC3339), fhTokens)
	if limitAgo > 0 {
		lines += fmt.Sprintf(`{"type":"assistant","timestamp":%q,"isApiErrorMessage":true,"apiErrorStatus":429,"error":"rate_limit","message":{"model":"<synthetic>"}}`+"\n",
			now.Add(-limitAgo).UTC().Format(time.RFC3339))
	}
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLearnsLimitFrom429AndRatchets(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Now()

	// 1) A 429 fires with 2.0M countable tokens in the window → learned cap.
	//    (cache_read of ~10M is present but must be excluded from the count.)
	writeTranscript(t, home, now, 2_000_000, 10*time.Minute)
	sum := Compute(stateDir, now)
	if sum.FiveHour.Total != 2_000_000 {
		t.Fatalf("5h total = %d, want 2_000_000 (cache_read must be excluded)", sum.FiveHour.Total)
	}
	if sum.Weekly.Total != 2_000_000 {
		t.Fatalf("weekly total = %d, want 2_000_000 (same line, within 7d)", sum.Weekly.Total)
	}
	if sum.FiveHour.Cap != 2_000_000 || sum.FiveHour.CapSource != "limit" {
		t.Fatalf("5h cap = %d/%s, want 2_000_000/limit", sum.FiveHour.Cap, sum.FiveHour.CapSource)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "usage-limits.json")); err != nil {
		t.Fatalf("learned limits not persisted: %v", err)
	}

	// 2) Later, no 429, more usage than the learned cap → ratchet up.
	writeTranscript(t, home, now, 2_600_000, 0)
	sum = Compute(stateDir, now)
	if sum.FiveHour.Cap != 2_600_000 || sum.FiveHour.CapSource != "observed" {
		t.Fatalf("5h cap did not ratchet: got %d/%s, want 2_600_000/observed", sum.FiveHour.Cap, sum.FiveHour.CapSource)
	}

	// 3) Learned cap persists across a fresh run; a lower load reads below 100%.
	writeTranscript(t, home, now, 1_300_000, 0)
	sum = Compute(stateDir, now)
	if sum.FiveHour.Cap != 2_600_000 {
		t.Fatalf("learned cap not persisted across runs: got %d", sum.FiveHour.Cap)
	}
	if p := sum.FiveHour.Pct(); p != 50 {
		t.Errorf("5h pct = %d, want 50 (1.3M of 2.6M)", p)
	}
}

// TestOverloadIsNotALimit ensures a 529 overloaded_error never trains a cap.
func TestOverloadIsNotALimit(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Now()

	cl := filepath.Join(home, ".claude", "projects", "p")
	_ = os.MkdirAll(cl, 0o755)
	line := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"isApiErrorMessage":true,"apiErrorStatus":529,"error":"overloaded_error","message":{"model":"<synthetic>"}}`+"\n",
		now.Add(-5*time.Minute).UTC().Format(time.RFC3339))
	line += fmt.Sprintf(`{"type":"assistant","timestamp":%q,"session_id":"s","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":500000,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`+"\n",
		now.Add(-time.Hour).UTC().Format(time.RFC3339))
	_ = os.WriteFile(filepath.Join(cl, "s.jsonl"), []byte(line), 0o644)

	sum := Compute(stateDir, now)
	if sum.FiveHour.CapSource == "limit" {
		t.Error("a 529 overload trained a limit cap; it must not")
	}
	if !sum.LastLimit.IsZero() {
		t.Error("529 recorded as a limit hit")
	}
}
