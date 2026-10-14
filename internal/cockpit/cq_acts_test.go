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
	// Not on a menu either — no session state brings it back without commits.
	for _, a := range cqActs(testRec("one", 0), "permission") {
		if a.k == "y" {
			t.Errorf("a permission ask with no commits should not offer %q", a.d)
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

// A running dispatcher is not asking for anything: it can be watched, stopped,
// or declared shipped, and nothing else. Offering "approve merge" or "skip"
// there would advertise an answer to a question nobody asked.
func TestCQActsForARunningRow(t *testing.T) {
	rec := testRec("one", 4)
	rec.Status = state.StatusWorking
	var keys []string
	for _, a := range cqActs(rec, "running") {
		keys = append(keys, a.k+" "+a.d)
	}
	if got := strings.Join(keys, " · "); got != "⏎ attach · y mark shipped · x kill" {
		t.Errorf("running acts = %q", got)
	}
}

// Marking shipped is the human's override, so no session state hides it — the
// robustness dispatcher sat at "working" after saying it was done, and y did
// nothing. What the state decides is whether y asks first: only once the turn
// has ended does it act on the keypress alone.
func TestMarkShippedAsksFirstWhileTheSessionIsGoing(t *testing.T) {
	for _, c := range []struct {
		status state.Status
		kind   string
		asks   bool
	}{
		{state.StatusWorking, "running", true},
		{state.StatusBlocked, "question", true},
		{state.StatusBlocked, "permission", true},
		{state.StatusLaunching, "startup", true},
		{state.StatusWorking, "parked", true},
		{state.StatusNeedsInput, "turn-done", false},
		{state.StatusNeedsInput, "parked", false},
		{state.StatusExited, "parked", false},
	} {
		rec := testRec("one", 2)
		rec.Status = c.status
		var ship *cqAct
		for _, a := range cqActs(rec, c.kind) {
			if a.k == "y" {
				ship = &a
			}
		}
		if ship == nil || ship.d != "mark shipped" {
			t.Errorf("%s %s: no mark shipped on the row", c.status, c.kind)
			continue
		}
		if got := ship.confirm != ""; got != c.asks {
			t.Errorf("%s %s: asks first = %v, want %v (%q)", c.status, c.kind, got, c.asks, ship.confirm)
		}
	}
}

// y on a row whose session is still going puts the question on the confirm bar
// and does nothing else; n there leaves the record alone, and y runs the act
// exactly as the plain key would on a finished turn.
func TestTheConfirmBarIsWhereAGoingSessionIsMarked(t *testing.T) {
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	m := cqModel(t)
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	rec := testRec("robustness", 3)
	rec.ID, rec.Status = "rob1", state.StatusWorking
	if err := state.Save(rec); err != nil {
		t.Fatal(err)
	}
	liveRecords = map[string]*state.Dispatch{rec.Feature: rec}
	liveByID = map[string]*state.Dispatch{rec.ID: rec}
	fleet = append(fleet, fleetRow{id: rec.ID, kind: "run", feature: rec.Feature, acts: cqActs(rec, "running")})
	m = m.fleetSync()
	m = press(m, "G")
	if r, _ := m.fleetSel(); r.id != rec.ID {
		t.Fatalf("cursor on %q, want the robustness row", r.feature)
	}

	next, cmd := m.handleKey("y")
	m = next.(model)
	if cmd != nil || m.cqFlash != "" {
		t.Fatal("y on a working session acted without asking")
	}
	if m.confirm == nil || !strings.Contains(m.confirm.label, "still working") {
		t.Fatalf("confirm = %+v, want the question naming what the session is doing", m.confirm)
	}
	m = press(m, "n")
	if m.confirm != nil || state.LoadAll()[0].Status != state.StatusWorking {
		t.Fatal("n should cancel and leave the record as it was")
	}

	m = press(m, "y")
	next, cmd = m.handleKey("y")
	m = next.(model)
	if m.confirm != nil || cmd == nil {
		t.Fatal("y on the bar should run the act")
	}
	if !strings.Contains(m.cqFlash, "marked shipped") || m.cqFlashID != rec.ID {
		t.Errorf("flash = %q on %q, want the act's own confirmation on the row asked about", m.cqFlash, m.cqFlashID)
	}
	// The flash batches the act's command beside its own timer; the command
	// is the one the plain key runs, so run that and read the record back.
	_, run := m.cqRun(fleetRow{id: rec.ID, kind: "run", feature: rec.Feature}, cqAct{k: "y", d: "mark shipped"})
	run()
	if got := state.LoadAll()[0]; got.Status != state.StatusDone || got.StatusReason != "marked shipped" {
		t.Errorf("record = %s %q, want marked shipped", got.Status, got.StatusReason)
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
