package steward

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/state"
)

// A Question is something a steward needs the human to decide, saved so it is
// never lost and never in the way.
//
// The case it exists for: the human has Remote Control on and checks in from
// their phone over a weekend while pre-approved work runs, and does not answer
// in time. A question asked through a menu would stop the steward dead on it —
// a menu holds the session until it is answered — and one asked in prose
// scrolls away under everything that happens after. So every question goes
// here, the steward keeps the work that does not depend on it moving, and a
// question unanswered for the away window (config's steward_away_after) is
// postponed: the human is taken to be away, and nothing is held for the answer
// any longer. When they come back and type the recap keyword, the steward asks
// every saved question at once — each one that is a choice as a menu, now that
// someone is there to answer it, and the rest in prose with the context a
// person gone for hours needs.
type Question struct {
	ID string `json:"id"`
	// For is the dispatcher the question holds up, "" for one about the
	// product as a whole; Feature names it.
	For     string `json:"for,omitempty"`
	Feature string `json:"feature,omitempty"`
	Text    string `json:"text"`
	// Context is what someone who has been away needs to answer it: what
	// happened, why it is blocked, what each answer would set going.
	Context string `json:"context,omitempty"`
	// Options are the answers, when the question is a choice — what becomes
	// a menu at the recap. Two to four, the most a menu takes.
	Options    []Option   `json:"options,omitempty"`
	AskedAt    time.Time  `json:"asked_at"`
	Answer     string     `json:"answer,omitempty"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
	// AwayAt is when the steward was told the human is away for this one
	// (`status --next`'s now_away), so each postponed question is announced
	// exactly once — including one that went past the window while the
	// steward was busy between two waits, which an announcement anchored on
	// when a wait began would miss.
	AwayAt *time.Time `json:"away_at,omitempty"`
	// Postponed is worked out when the questions are read, never stored: an
	// open question older than the away window.
	Postponed bool `json:"postponed,omitempty"`
}

// Option is one answer to a question that is a choice.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// MaxOptions and MinOptions are what a Claude Code question menu takes.
const (
	MinOptions = 2
	MaxOptions = 4
)

// Open reports whether the question still waits on the human.
func (q Question) Open() bool { return q.AnsweredAt == nil }

// PostponedAt is when an open question counts as postponed.
func (q Question) PostponedAt(away time.Duration) time.Time { return q.AskedAt.Add(away) }

// Questions is every question saved for product, oldest first — the order the
// human is asked them in — each marked postponed if it is open and has waited
// longer than away at t.
func Questions(product string, t time.Time, away time.Duration) []Question {
	return questionsIn(ProductDir(product), t, away)
}

// questionsIn reads the questions in a product's folder, addressed by folder
// for the reason rulingsIn is.
func questionsIn(dir string, t time.Time, away time.Duration) []Question {
	raw, err := os.ReadFile(filepath.Join(dir, "questions.json"))
	if err != nil {
		return nil
	}
	var qs []Question
	if json.Unmarshal(raw, &qs) != nil {
		return nil
	}
	sort.SliceStable(qs, func(i, j int) bool { return qs[i].AskedAt.Before(qs[j].AskedAt) })
	for i := range qs {
		qs[i].Postponed = qs[i].Open() && !t.Before(qs[i].PostponedAt(away))
	}
	return qs
}

// OpenQuestions is the questions still waiting on the human.
func OpenQuestions(product string, t time.Time, away time.Duration) []Question {
	var out []Question
	for _, q := range Questions(product, t, away) {
		if q.Open() {
			out = append(out, q)
		}
	}
	return out
}

func saveQuestionsIn(dir string, qs []Question) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i := range qs {
		qs[i].Postponed = false // worked out on read, never stored
	}
	raw, err := json.MarshalIndent(qs, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "questions.json")
	if err := os.WriteFile(path+".tmp", append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// Ask saves a question for the human. The same question about the same
// dispatcher, still open, is the one already saved — a dispatcher that asks
// again on every CI tick must not fill the recap with copies — and keeps the
// time it was first asked, which is what the away window counts from.
func Ask(product string, q Question) (Question, bool, error) {
	q.Text = strings.Join(strings.Fields(q.Text), " ")
	q.Context = strings.TrimSpace(q.Context)
	if q.Text == "" {
		return q, false, errors.New("nothing to ask")
	}
	if n := len(q.Options); n > 0 && (n < MinOptions || n > MaxOptions) {
		return q, false, fmt.Errorf("a choice has %d to %d options (this has %d) — fewer is not a choice, more is an open question", MinOptions, MaxOptions, n)
	}
	for _, o := range q.Options {
		if strings.TrimSpace(o.Label) == "" {
			return q, false, errors.New("an option needs a label")
		}
	}
	release := state.Lock()
	defer release()
	dir := ProductDir(product)
	qs := questionsIn(dir, now(), 0)
	for _, old := range qs {
		if old.Open() && old.For == q.For && old.Text == q.Text {
			return old, false, nil
		}
	}
	q.ID = "q" + state.NewID()[:6]
	q.AskedAt = now()
	q.Answer, q.AnsweredAt = "", nil
	return q, true, saveQuestionsIn(dir, append(qs, q))
}

// Answer closes a question with the human's answer, in their words, wherever
// it is.
func Answer(id, answer string) (product string, q Question, err error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", q, errors.New("say what the answer was")
	}
	release := state.Lock()
	defer release()
	for _, dir := range productDirs() {
		p := filepath.Base(dir)
		qs := questionsIn(dir, now(), 0)
		for i := range qs {
			if qs[i].ID != id {
				continue
			}
			if !qs[i].Open() {
				return p, qs[i], fmt.Errorf("question %s was already answered: %s", id, qs[i].Answer)
			}
			t := now()
			qs[i].Answer, qs[i].AnsweredAt = answer, &t
			return p, qs[i], saveQuestionsIn(dir, qs)
		}
	}
	return "", q, fmt.Errorf("no question %q", id)
}

// MarkAway stamps the questions the steward has just been told the human is
// away for, so none is announced twice.
func MarkAway(product string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	release := state.Lock()
	defer release()
	dir := ProductDir(product)
	qs := questionsIn(dir, now(), 0)
	t := now()
	for i := range qs {
		for _, id := range ids {
			if qs[i].ID == id && qs[i].AwayAt == nil {
				qs[i].AwayAt = &t
			}
		}
	}
	return saveQuestionsIn(dir, qs)
}
