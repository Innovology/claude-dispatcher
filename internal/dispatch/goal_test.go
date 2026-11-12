package dispatch

import "testing"

// What the form writes is what the record reads back.
func TestGoalRoundTrips(t *testing.T) {
	prompt := "Fix the retry window\n\nIt backs off too fast.\n\n" + GoalSentence("  ci is   green ")
	if got := GoalOf(prompt); got != "ci is green" {
		t.Fatalf("goal = %q", got)
	}
	if GoalSentence(" ") != "" {
		t.Fatal("no goal is no sentence")
	}
}

// A brief that only talks about being done has no goal.
func TestGoalOfNeedsTheWholeShape(t *testing.T) {
	for _, p := range []string{
		"",
		"done when: ci is green",
		"We are done when: ci is green\nKeep working until that is true.",
		"Make it work. Keep working until that is true.",
	} {
		if g := GoalOf(p); g != "" {
			t.Errorf("GoalOf(%q) = %q", p, g)
		}
	}
	if g := GoalOf("x\ndone when: a\nKeep working until that is true.\ndone when: b\nKeep working until that is true."); g != "b" {
		t.Errorf("the last goal wins, got %q", g)
	}
}
