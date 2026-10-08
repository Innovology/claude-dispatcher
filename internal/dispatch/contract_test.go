package dispatch

import (
	"strings"
	"testing"
)

func TestWithContractClosesWithTheModesContract(t *testing.T) {
	got := withContract("fix the flaky upload", ModeAuto)
	if !strings.HasPrefix(got, "fix the flaky upload\n\n") || !strings.HasSuffix(got, Contract(ModeAuto)) {
		t.Fatalf("got\n%s", got)
	}
	// Auto asks for Claude Code's own waiting machinery by name, and every
	// stop to end on its question — the sentence the triage row quotes.
	for _, want := range []string{"background task or Monitor", "end your message on that one question"} {
		if !strings.Contains(got, want) {
			t.Errorf("auto contract lacks %q", want)
		}
	}
	// Plan mode must never be told to commit: it cannot.
	if p := withContract("x", ModePlan); strings.Contains(p, "Commit") || !strings.Contains(p, "put the plan up") {
		t.Errorf("plan contract:\n%s", p)
	}
	if m := withContract("x", ModeManual); !strings.Contains(m, "check in before committing") {
		t.Errorf("manual contract:\n%s", m)
	}
}

// A prompt that already carries some of the contract — the old closing line,
// typed into the + form by hand — gains only what it lacks, and one carrying
// all of it is left alone.
func TestWithContractSaysNothingTwice(t *testing.T) {
	old := "fix it\n\nCommit as you go, open the PR, and fix your own CI failures without stopping to ask."
	got := withContract(old, ModeAuto)
	if strings.Count(got, "Commit as you go") != 1 {
		t.Fatalf("said twice:\n%s", got)
	}
	if !strings.Contains(got, "background task or Monitor") {
		t.Fatalf("the missing sentences were not added:\n%s", got)
	}
	if again := withContract(got, ModeAuto); again != got {
		t.Fatalf("a complete contract was added to again:\n%s", again)
	}
}
