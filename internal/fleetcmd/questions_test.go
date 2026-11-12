package fleetcmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/steward"
)

// ask saves, questions lists, answer closes.
func TestQuestionVerbs(t *testing.T) {
	now := time.Now()
	withStore(t, &state.Dispatch{ID: "d1", Feature: "goldens", RepoName: "player-app", Status: state.StatusNeedsInput, WaitingSince: &now})
	var out, errOut bytes.Buffer
	if code := Run("ask", []string{"--product", "player", "--for", "goldens", "--option", "Approve | they ship", "--option", "Hold",
		"--context", "the frames moved 4px", "Approve", "the", "goldens?"}, &out, &errOut); code != 0 {
		t.Fatalf("ask: %s", errOut.String())
	}
	id := strings.TrimSuffix(strings.Fields(out.String())[1], "")
	out.Reset()
	if code := Run("questions", []string{"--product", "player", "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("questions: %s", errOut.String())
	}
	var qs []steward.Question
	if err := json.Unmarshal(out.Bytes(), &qs); err != nil || len(qs) != 1 {
		t.Fatalf("questions = %s", out.String())
	}
	q := qs[0]
	if q.ID != id || q.For != "d1" || q.Feature != "goldens" || q.Context != "the frames moved 4px" ||
		len(q.Options) != 2 || q.Options[0].Description != "they ship" || q.Options[1].Label != "Hold" {
		t.Fatalf("saved %+v", q)
	}
	out.Reset()
	if code := Run("answer", []string{id, "approve", "them"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "approve them") {
		t.Fatalf("answer: %q %q", out.String(), errOut.String())
	}
	out.Reset()
	_ = Run("questions", []string{"--product", "player"}, &out, &errOut)
	if !strings.Contains(out.String(), "nothing saved") {
		t.Fatalf("an answered question is still open: %q", out.String())
	}
	if code := Run("ask", []string{"no", "product"}, &out, &errOut); code == 0 {
		t.Fatal("a question with no product was accepted")
	}
}

// --next wakes the moment a question goes unanswered past the away window —
// the human is away — and carries the open questions; one already past it
// when the wait began does not wake it again.
func TestNextWakesWhenTheHumanIsAway(t *testing.T) {
	withStore(t)
	loadConfig = func() (*config.Config, error) {
		return &config.Config{StewardAwayAfter: "40ms", Products: map[string][]string{"player": {"player-app"}}}, nil
	}
	prev := NextPoll
	NextPoll = 5 * time.Millisecond
	t.Cleanup(func() { NextPoll = prev })

	q, _, err := steward.Ask("player", steward.Question{Text: "Close #801?"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	start := time.Now()
	if err := runNext(&out, "player", time.Second, time.Now); err != nil {
		t.Fatal(err)
	}
	var n Next
	if err := json.Unmarshal(out.Bytes(), &n); err != nil {
		t.Fatal(err)
	}
	if len(n.NowAway) != 1 || n.NowAway[0] != q.ID || n.TimedOut || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("did not wake when the question went past the window: %s", out.String())
	}
	if len(n.Questions) != 1 || !n.Questions[0].Postponed {
		t.Fatalf("questions = %+v", n.Questions)
	}

	out.Reset()
	if err := runNext(&out, "player", 30*time.Millisecond, time.Now); err != nil {
		t.Fatal(err)
	}
	n = Next{}
	_ = json.Unmarshal(out.Bytes(), &n)
	if len(n.NowAway) != 0 || !n.TimedOut || len(n.Questions) != 1 {
		t.Fatalf("an already-postponed question woke it again: %s", out.String())
	}
}

// Found in review: a question that went past the away window while the
// steward was busy between two waits was never announced, because only a
// crossing after the wait began counted. It is announced on the next wait
// now, at once, and stamped so it is announced only that once.
func TestNextAnnouncesAQuestionThatWentAwayBetweenWaits(t *testing.T) {
	withStore(t)
	loadConfig = func() (*config.Config, error) {
		return &config.Config{StewardAwayAfter: "1ms", Products: map[string][]string{"player": {"player-app"}}}, nil
	}
	q, _, err := steward.Ask("player", steward.Question{Text: "Close #801?"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond) // past the window before the wait begins
	var out bytes.Buffer
	start := time.Now()
	if err := runNext(&out, "player", time.Second, time.Now); err != nil {
		t.Fatal(err)
	}
	var n Next
	_ = json.Unmarshal(out.Bytes(), &n)
	if len(n.NowAway) != 1 || n.NowAway[0] != q.ID || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("a question already past the window was not announced: %s", out.String())
	}
	if qs := steward.OpenQuestions("player", time.Now(), time.Millisecond); qs[0].AwayAt == nil {
		t.Fatal("the announcement was not stamped on the question")
	}
}
