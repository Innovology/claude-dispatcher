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

	// Each step is a separate run of the cockpit, which starts with no
	// transcripts read. Within one run the reading is incremental and the file
	// would only ever grow; rewriting it in place, as these steps do, is not
	// something a transcript does.
	forgetScans()

	// 2) Later, no 429, more usage than the learned cap → ratchet up.
	writeTranscript(t, home, now, 2_600_000, 0)
	sum = Compute(stateDir, now)
	if sum.FiveHour.Cap != 2_600_000 || sum.FiveHour.CapSource != "observed" {
		t.Fatalf("5h cap did not ratchet: got %d/%s, want 2_600_000/observed", sum.FiveHour.Cap, sum.FiveHour.CapSource)
	}

	forgetScans()
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

// A transcript only grows, so a later load reads just what was appended — and
// a line still being written is left for the load that finds it whole. A file
// that shrank is not the one that was read, and is read again from the start.
func TestScanReadsOnlyWhatWasAppended(t *testing.T) {
	forgetScans()
	t.Cleanup(forgetScans)
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Now()
	proj := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, "s.jsonl")
	line := func(tok int) string {
		return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"session_id":"s","message":{"model":"claude-opus-4-5","usage":{"input_tokens":%d}}}`+"\n",
			now.Add(-time.Minute).UTC().Format(time.RFC3339), tok)
	}
	// Tool output that merely mentions usage is escaped, and must not count.
	noise := `{"type":"user","timestamp":"x","message":{"content":"the \"usage\": field"}}` + "\n"
	if err := os.WriteFile(path, []byte(line(100)+noise), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := scanWindows(now); w.wkTotal != 100 {
		t.Fatalf("first read = %d, want 100", w.wkTotal)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	half := line(50)
	if _, err := f.WriteString(line(20) + half[:10]); err != nil { // one whole line, one half
		t.Fatal(err)
	}
	if w := scanWindows(now.Add(time.Second)); w.wkTotal != 120 {
		t.Errorf("after an append = %d, want 120 (the half line is not read yet)", w.wkTotal)
	}
	if _, err := f.WriteString(half[10:]); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if w := scanWindows(now.Add(2 * time.Second)); w.wkTotal != 170 {
		t.Errorf("once the line is whole = %d, want 170", w.wkTotal)
	}

	if err := os.WriteFile(path, []byte(line(7)), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := scanWindows(now.Add(3 * time.Second)); w.wkTotal != 7 {
		t.Errorf("a file that shrank = %d, want 7, read from the start", w.wkTotal)
	}
}

// The figure a window is measured against: the learned cap once there is one,
// the high-water mark while learning, and a percentage only against a cap.
func TestStatMeasuresAgainstItsCap(t *testing.T) {
	learning := Stat{Total: 30, HighWater: 60}
	if learning.Denom() != 60 || learning.Pct() != -1 {
		t.Errorf("while learning: denom %d pct %d, want 60 and -1", learning.Denom(), learning.Pct())
	}
	capped := Stat{Total: 30, Cap: 120, HighWater: 60}
	if capped.Denom() != 120 || capped.Pct() != 25 {
		t.Errorf("with a cap: denom %d pct %d, want 120 and 25", capped.Denom(), capped.Pct())
	}
	if over := (Stat{Total: 300, Cap: 120}); over.Pct() != 100 {
		t.Errorf("past the cap reads %d%%, want 100", over.Pct())
	}
}
