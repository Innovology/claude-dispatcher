package fleetcmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

func withStore(t *testing.T, recs ...*state.Dispatch) {
	t.Helper()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	for _, r := range recs {
		if err := state.Save(r); err != nil {
			t.Fatal(err)
		}
	}
}

func withSessions(t *testing.T, alive, idle, known bool) *[]string {
	t.Helper()
	pa, pi, ps := sessionAlive, sessionIdle, sendKeys
	t.Cleanup(func() { sessionAlive, sessionIdle, sendKeys = pa, pi, ps })
	sessionAlive = func(string) bool { return alive }
	sessionIdle = func(string) (bool, bool) { return idle, known }
	var typed []string
	sendKeys = func(name, text string) error { typed = append(typed, name+": "+text); return nil }
	return &typed
}

func TestFleetReadsLikeTheTriageTable(t *testing.T) {
	now := time.Now()
	finished := now.Add(-time.Hour)
	recs := []*state.Dispatch{
		{ID: "w", Feature: "working", Status: state.StatusWorking},
		{ID: "a", Feature: "asks", Status: state.StatusNeedsInput,
			Said: "PR #7 is open.\n\nWant me to merge it?"},
		{ID: "r", Feature: "retrying", Status: state.StatusNeedsInput,
			Failure: &state.Failure{Error: "overloaded", At: now}},
		{ID: "l", Feature: "limited", Status: state.StatusNeedsInput,
			Failure: &state.Failure{Error: "rate_limit", At: now}},
		{ID: "b", Feature: "blocked", Status: state.StatusBlocked},
		{ID: "p", Feature: "parked", Status: state.StatusNeedsInput, ParkedReason: "tomorrow"},
		{ID: "h", Feature: "held", Status: state.StatusExited, FinishedAt: &finished},
		{ID: "gone", Feature: "read", Status: state.StatusExited, FinishedAt: &finished, DismissedAt: &finished},
	}
	got := map[string]Entry{}
	var order []string
	for _, e := range Fleet(recs, now) {
		got[e.ID] = e
		order = append(order, e.State)
	}
	want := map[string]string{"w": "running", "a": "wants-you", "r": "running", "l": "wants-you",
		"b": "wants-you", "p": "parked", "h": "finished"}
	for id, st := range want {
		if got[id].State != st {
			t.Errorf("%s: state %q want %q", id, got[id].State, st)
		}
	}
	if _, ok := got["gone"]; ok {
		t.Error("a dismissed dispatcher is history, not fleet")
	}
	if got["a"].Ask != "Want me to merge it?" {
		t.Errorf("ask = %q", got["a"].Ask)
	}
	if !got["r"].Retrying || got["l"].Retrying {
		t.Errorf("retrying: overloaded=%v rate_limit=%v", got["r"].Retrying, got["l"].Retrying)
	}
	if order[0] != "wants-you" || order[len(order)-1] != "parked" {
		t.Errorf("not most urgent first: %v", order)
	}
}

func TestStatusJSON(t *testing.T) {
	withStore(t, &state.Dispatch{ID: "a", Feature: "asks", Status: state.StatusNeedsInput, Said: "Say the word and I'll ship it."})
	var out, errOut bytes.Buffer
	if code := Run("status", []string{"--json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var es []Entry
	if err := json.Unmarshal(out.Bytes(), &es); err != nil || len(es) != 1 || es[0].Ask != "Say the word and I'll ship it." {
		t.Fatalf("got %s (%v)", out.String(), err)
	}
}

func TestReply(t *testing.T) {
	withStore(t,
		&state.Dispatch{ID: "a1", Feature: "asks", Status: state.StatusNeedsInput, TmuxSession: "disp-asks"},
		&state.Dispatch{ID: "b1", Feature: "blocked", Status: state.StatusBlocked, TmuxSession: "disp-blocked"},
	)
	typed := withSessions(t, true, false, true)
	var out, errOut bytes.Buffer
	if code := Run("reply", []string{"asks", "merge", "it"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if len(*typed) != 1 || (*typed)[0] != "disp-asks: merge it" {
		t.Fatalf("typed %q", *typed)
	}
	// A permission prompt is a menu: typed text would pick its options.
	if code := Run("reply", []string{"b1", "yes"}, &out, &errOut); code == 0 || len(*typed) != 1 {
		t.Fatal("replied into a permission prompt")
	}
}

func TestReplyRefusesAShell(t *testing.T) {
	withStore(t, &state.Dispatch{ID: "a1", Feature: "asks", Status: state.StatusNeedsInput, TmuxSession: "disp-asks"})
	typed := withSessions(t, true, true, true) // claude exited, shell in the pane
	var out, errOut bytes.Buffer
	if code := Run("reply", []string{"a1", "continue"}, &out, &errOut); code == 0 || len(*typed) != 0 {
		t.Fatalf("typed into a shell: %q", *typed)
	}
}

func TestFindRefusesAnAmbiguousName(t *testing.T) {
	withStore(t,
		&state.Dispatch{ID: "x1", Feature: "same", Status: state.StatusWorking},
		&state.Dispatch{ID: "x2", Feature: "same", Status: state.StatusNeedsInput},
	)
	if _, err := find("same"); err == nil || !strings.Contains(err.Error(), "use the id") {
		t.Fatalf("got %v", err)
	}
	if d, err := find("x2"); err != nil || d.ID != "x2" {
		t.Fatalf("by id: %v", err)
	}
}

func TestParkAndUnpark(t *testing.T) {
	withStore(t, &state.Dispatch{ID: "a1", Feature: "asks", Status: state.StatusNeedsInput})
	var out, errOut bytes.Buffer
	if code := Run("park", []string{"asks"}, &out, &errOut); code == 0 {
		t.Fatal("a park with no reason must be refused")
	}
	if code := Run("park", []string{"asks", "waiting", "on", "legal"}, &out, &errOut); code != 0 {
		t.Fatalf("park: %s", errOut.String())
	}
	d, _ := find("a1")
	if d.ParkedReason != "waiting on legal" || d.ParkedAt == nil {
		t.Fatalf("not parked: %+v", d)
	}
	if code := Run("unpark", []string{"a1"}, &out, &errOut); code != 0 {
		t.Fatalf("unpark: %s", errOut.String())
	}
	if d, _ := find("a1"); d.Parked() {
		t.Fatal("still parked")
	}
}

// --next returns at once with every wait that has no note on it, leaves out a
// handled one, and times out empty when nothing is waiting.
func TestNext(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Minute)
	withStore(t,
		&state.Dispatch{ID: "a", Feature: "asks", Status: state.StatusNeedsInput, WaitingSince: &now, Said: "Want me to?"},
		&state.Dispatch{ID: "h", Feature: "handled", Status: state.StatusNeedsInput, WaitingSince: &earlier,
			StewardNote: "yours: merge", StewardNoteAt: &now},
		&state.Dispatch{ID: "s", Feature: "stale note", Status: state.StatusNeedsInput, WaitingSince: &now,
			StewardNote: "old", StewardNoteAt: &earlier},
		&state.Dispatch{ID: "w", Feature: "working", Status: state.StatusWorking},
	)
	var out bytes.Buffer
	if err := runNext(&out, time.Hour, time.Now); err != nil {
		t.Fatal(err)
	}
	var n Next
	if err := json.Unmarshal(out.Bytes(), &n); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range n.Waiting {
		ids[e.ID] = true
	}
	if !ids["a"] || !ids["s"] || ids["h"] || ids["w"] || n.TimedOut {
		t.Fatalf("waiting = %v timed_out=%v", ids, n.TimedOut)
	}
}

func TestNextTimesOut(t *testing.T) {
	withStore(t, &state.Dispatch{ID: "w", Feature: "working", Status: state.StatusWorking})
	prev := NextPoll
	NextPoll = time.Millisecond
	t.Cleanup(func() { NextPoll = prev })
	var out bytes.Buffer
	if err := runNext(&out, 5*time.Millisecond, time.Now); err != nil {
		t.Fatal(err)
	}
	var n Next
	if err := json.Unmarshal(out.Bytes(), &n); err != nil || !n.TimedOut || len(n.Waiting) != 0 {
		t.Fatalf("got %s", out.String())
	}
}

func TestNote(t *testing.T) {
	now := time.Now()
	withStore(t,
		&state.Dispatch{ID: "a", Feature: "asks", Status: state.StatusNeedsInput, WaitingSince: &now},
		&state.Dispatch{ID: "w", Feature: "working", Status: state.StatusWorking},
	)
	var out, errOut bytes.Buffer
	if code := Run("note", []string{"asks", "yours:", "the", "merge"}, &out, &errOut); code != 0 {
		t.Fatalf("note: %s", errOut.String())
	}
	d, _ := find("a")
	if d.Note() != "yours: the merge" {
		t.Fatalf("note = %q", d.Note())
	}
	if code := Run("note", []string{"w", "x"}, &out, &errOut); code == 0 {
		t.Fatal("a note on a working dispatcher is about nothing")
	}
	if e := Fleet(state.LoadAll(), time.Now()); !e[0].Handled {
		t.Fatalf("a noted wait is handled: %+v", e[0])
	}
}

// A reply marks the wait answered at the send, so --next does not wake the
// steward for it again in the moment before the session's hook lands — the
// race the first live steward run found.
func TestReplyMarksTheWaitAnswered(t *testing.T) {
	now := time.Now()
	withStore(t, &state.Dispatch{ID: "a1", Feature: "asks", Status: state.StatusNeedsInput,
		TmuxSession: "disp-asks", WaitingSince: &now})
	withSessions(t, true, false, true)
	var out, errOut bytes.Buffer
	if code := Run("reply", []string{"a1", "steward: yes, write the tests"}, &out, &errOut); code != 0 {
		t.Fatalf("reply: %s", errOut.String())
	}
	d, _ := find("a1")
	if d.Answered() != "steward: yes, write the tests" || !d.Handled() {
		t.Fatalf("answer = %q handled = %v", d.Answered(), d.Handled())
	}
	out.Reset()
	prev := NextPoll
	NextPoll = time.Millisecond
	t.Cleanup(func() { NextPoll = prev })
	if err := runNext(&out, 5*time.Millisecond, time.Now); err != nil {
		t.Fatal(err)
	}
	var n Next
	_ = json.Unmarshal(out.Bytes(), &n)
	if len(n.Waiting) != 0 {
		t.Fatalf("an answered wait woke the steward again: %s", out.String())
	}
}
