package cockpit

// cq_acts_test.go covers the two places the triage lens can do real damage or
// quietly mislead: cqRun, which turns a keypress into a merge or a kill, and the
// row-shedding that decides what survives on a short terminal.

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/state"
)

// Every act the table advertises must reach a real command. An act shown in the
// footer that fires nothing is worse than no act at all — the user believes the
// dispatcher was handled.
func TestCQRunReachesACommandForEveryAdvertisedAct(t *testing.T) {
	q := func(ask string) fleetRow { return fleetRow{feature: "one", kind: "queue", ask: ask} }
	cases := []struct {
		name string
		it   fleetRow
		key  string
		want bool // a command is expected
	}{
		{"attach", q("needs"), "⏎", false}, // no live session in a test
		{"merge on review", q("review"), "y", true},
		{"mark shipped otherwise", q("turn-done"), "y", true},
		{"kill", q("needs"), "x", true},
		{"kill a running row", fleetRow{feature: "one", kind: "run"}, "x", true},
		{"skip fires nothing", q("needs"), "s", false},
		{"unknown key fires nothing", q("needs"), "z", false},
	}
	for _, c := range cases {
		_, cmd := newModel().cqRun(c.it, cqAct{k: c.key})
		if got := cmd != nil; got != c.want {
			t.Errorf("%s: cqRun(%q) returned cmd=%v, want %v", c.name, c.key, got, c.want)
		}
	}
}

// y is overloaded on purpose: on a review row it squash-merges for real, and
// anywhere else it only marks the record done. Getting this backwards would
// either merge something nobody asked to merge, or silently not merge at all.
func TestCQRunYIsMergeOnlyForReview(t *testing.T) {
	acts := cqActs(testRec("one", 3), "review")
	var labels []string
	for _, a := range acts {
		if a.k == "y" {
			labels = append(labels, a.d)
		}
	}
	if len(labels) != 1 || labels[0] != "approve merge" {
		t.Errorf("review item's y act = %v, want [approve merge]", labels)
	}

	acts = cqActs(testRec("one", 3), "turn-done")
	for _, a := range acts {
		if a.k == "y" && a.d != "mark shipped" {
			t.Errorf("non-review y act = %q, want \"mark shipped\"", a.d)
		}
	}
}

// A dispatcher that produced nothing cannot honestly be "marked shipped" —
// "done means live", and there is nothing to be live.
func TestCQActsHidesShipWithNoCommits(t *testing.T) {
	for _, a := range cqActs(testRec("one", 0), "turn-done") {
		if a.k == "y" {
			t.Errorf("a dispatcher with no commits should not offer %q", a.d)
		}
	}
	// A permission ask never offers it either, commits or not.
	for _, a := range cqActs(testRec("one", 5), "permission") {
		if a.k == "y" {
			t.Errorf("a permission ask should not offer %q", a.d)
		}
	}
}

// When the column is too short, rows are dropped cheapest-first and the ones
// marked fixed always survive — the table itself must never be shed to fit.
func TestCQShedDropsCheapestFirst(t *testing.T) {
	rows := []cqRow{
		cqFixed("table"), {s: "spacer", shed: 1}, cqFixed("footer"), {s: "detail", shed: 3},
	}
	got := rowText(cqShed(rows, 3))
	if !strings.Contains(got, "table") || !strings.Contains(got, "footer") {
		t.Fatalf("fixed rows must survive shedding, got %q", got)
	}
	if strings.Contains(got, "spacer") {
		t.Errorf("the cheapest row should be shed first, got %q", got)
	}
	if n := len(cqShed(rows, 3)); n > 3 {
		t.Errorf("shed to %d rows, want at most 3", n)
	}

	// A budget that cannot be met even by shedding everything sheddable stops
	// rather than looping forever.
	onlyFixed := []cqRow{cqFixed("a"), cqFixed("b")}
	if n := len(cqShed(onlyFixed, 1)); n != 2 {
		t.Errorf("unsheddable rows should be returned intact, got %d", n)
	}
}

// A running dispatcher is not asking for anything: it can be watched or
// stopped, and nothing else. Offering "approve merge" or "skip" there would
// advertise an answer to a question nobody asked.
func TestCQActsForARunningRow(t *testing.T) {
	var keys []string
	for _, a := range cqActs(testRec("one", 4), "running") {
		keys = append(keys, a.k)
	}
	if strings.Join(keys, " ") != "⏎ x" {
		t.Errorf("running acts = %v, want [⏎ x]", keys)
	}
}

func rowText(rows []cqRow) string {
	var out []string
	for _, r := range rows {
		out = append(out, r.s)
	}
	return strings.Join(out, ",")
}

// testRec is a dispatch record with n commits, enough for the act helpers.
func testRec(feature string, n int) *state.Dispatch {
	commits := make([]string, n)
	for i := range commits {
		commits[i] = "sha"
	}
	return &state.Dispatch{Feature: feature, RepoName: "shop-api", PRNumber: 151, Commits: commits}
}
