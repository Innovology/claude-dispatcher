package steward

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
)

// A question is saved once however often it is asked, counts as postponed
// once it has waited the away window, and closes with the human's answer.
func TestQuestions(t *testing.T) {
	withFakes(t, &fakeSup{trusted: true}, exePath(t))
	q, added, err := Ask("player", Question{For: "d1", Feature: "goldens", Text: "Approve the  club-history goldens?",
		Options: []Option{{Label: "Approve"}, {Label: "Hold", Description: "keep #931 out of the queue"}}})
	if err != nil || !added {
		t.Fatal(err)
	}
	if q.Text != "Approve the club-history goldens?" || !strings.HasPrefix(q.ID, "q") {
		t.Fatalf("q = %+v", q)
	}
	again, added, err := Ask("player", Question{For: "d1", Text: "Approve the club-history goldens?"})
	if err != nil || added || again.ID != q.ID {
		t.Fatalf("the same open question asked again must be the one saved: %v %v %+v", err, added, again)
	}
	other, _, _ := Ask("player", Question{Text: "Close #801 or refresh it?"})

	if got := OpenQuestions("player", q.AskedAt.Add(time.Minute), 15*time.Minute); len(got) != 2 || got[0].Postponed {
		t.Fatalf("fresh questions = %+v", got)
	}
	late := OpenQuestions("player", q.AskedAt.Add(16*time.Minute), 15*time.Minute)
	if !late[0].Postponed || late[0].ID != q.ID {
		t.Fatalf("a question past the away window must be postponed, oldest first: %+v", late)
	}

	product, answered, err := Answer(q.ID, "approve")
	if err != nil || product != "player" || answered.Answer != "approve" {
		t.Fatalf("answer: %v %s %+v", err, product, answered)
	}
	if _, _, err := Answer(q.ID, "hold"); err == nil {
		t.Fatal("answering an answered question must say so")
	}
	if got := OpenQuestions("player", time.Now(), time.Hour); len(got) != 1 || got[0].ID != other.ID {
		t.Fatalf("open after an answer = %+v", got)
	}
	// Postponed is worked out on read, never written down.
	raw, _ := os.ReadFile(filepath.Join(ProductDir("player"), "questions.json"))
	if strings.Contains(string(raw), "postponed") {
		t.Fatal("postponed was stored")
	}
}

func TestAskRefuses(t *testing.T) {
	withFakes(t, &fakeSup{trusted: true}, exePath(t))
	for name, q := range map[string]Question{
		"empty":     {Text: " "},
		"one":       {Text: "Merge?", Options: []Option{{Label: "Yes"}}},
		"five":      {Text: "Which?", Options: []Option{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}}},
		"unlabeled": {Text: "Which?", Options: []Option{{Label: "a"}, {Label: " "}}},
	} {
		if _, _, err := Ask("player", q); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The brief carries the recap keyword and the away window the config names.
func TestBriefCarriesTheRecap(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, exePath(t))
	loadConfig = func() (*config.Config, error) {
		return &config.Config{RecapKeyword: "back", StewardAwayAfter: "1h30m", Products: map[string][]string{"player": {"player-app"}}}, nil
	}
	s, err := Start("player")
	if err != nil {
		t.Fatal(err)
	}
	brief, _ := os.ReadFile(filepath.Join(s.Dir(), "CLAUDE.md"))
	for _, want := range []string{"When the human's message is `back`", "unanswered for 1h30m", "ask --product player", "saved for back"} {
		if !strings.Contains(string(brief), want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	for d, want := range map[time.Duration]string{15 * time.Minute: "15m", time.Hour: "1h", 90 * time.Minute: "1h30m", 90 * time.Second: "1m30s"} {
		if got := PlainDuration(d); got != want {
			t.Errorf("PlainDuration(%v) = %q", d, got)
		}
	}
}

// A carried note never wakes the steward again, so the brief sends it back
// over its noted waits when a ruling may have settled them (found in review).
func TestBriefRevisitsNotedWaits(t *testing.T) {
	b := Brief("/bin/cd", Facts{Product: "player"})
	if !strings.Contains(b, "act on any the\nruling now decides") || !strings.Contains(b, "over the waits you noted") {
		t.Fatal("the brief does not send the steward back over its noted waits")
	}
}
