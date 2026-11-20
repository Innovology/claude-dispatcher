package cockpit

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

// A dispatcher that handed its work on says so where it says how it ended — a
// handoff is where the work went, which matters more than how the session
// stopped — under a merge of its own, which is further than a handoff gets.
func TestAHandoffIsHowItEnded(t *testing.T) {
	handed := &state.Dispatch{Status: state.StatusExited, HandedOffTo: []string{"b"}}
	if got := cqEnded(handed); got != "handed off" {
		t.Errorf("cqEnded = %q, want handed off", got)
	}
	handed.PRState = "MERGED"
	if got := cqEnded(handed); got != "merged" {
		t.Errorf("a handed-off dispatcher whose own PR merged reads %q", got)
	}
	if got := cqEnded(&state.Dispatch{Status: state.StatusExited, HandedOffFrom: "a"}); got != "stopped" {
		t.Errorf("being handed work is not how a dispatcher ends: %q", got)
	}
}

// Each end names the other; a record the store no longer holds is named by its
// id, which `status` still finds.
func TestFleetHandoffNamesTheOtherEnd(t *testing.T) {
	byID := map[string]*state.Dispatch{
		"a": {ID: "a", Feature: "orders endpoint", RepoName: "api", HandedOffTo: []string{"b", "gone"}},
		"b": {ID: "b", Feature: "show orders", RepoName: "app", HandedOffFrom: "a", HandedOffTo: []string{"c"}},
		"c": {ID: "c", Feature: "ship it", RepoName: "web"},
	}
	cases := map[string]string{
		"a": `handed off to "show orders" in app, gone`,
		"b": `handed off from "orders endpoint" in api · handed off to "ship it" in web`,
		"c": "",
	}
	for id, want := range cases {
		if got := fleetHandoff(byID, byID[id]); got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}
	if got := fleetHandoff(byID, nil); got != "" {
		t.Errorf("no record: %q", got)
	}

	// A run the handoff ended leads its panel with a reason in these very
	// words; the line does not say it again. A second successor is news the
	// reason does not carry, so then the line names them all.
	ended := &state.Dispatch{StatusReason: state.ReasonHandedOff + `"show orders" in app`, HandedOffTo: []string{"b"}}
	if got := fleetHandoff(byID, ended); got != "" {
		t.Errorf("the line repeats the reason: %q", got)
	}
	ended.HandedOffTo = append(ended.HandedOffTo, "c")
	if got, want := fleetHandoff(byID, ended), `handed off to "show orders" in app, "ship it" in web`; got != want {
		t.Errorf("two successors: %q, want %q", got, want)
	}
}

// Through a real load: the dispatcher that handed off holds its row, saying it
// handed off and to whom; the one it started is on the table saying where its
// work came from; and the products lens does not count a handoff as work turned
// down.
func TestALoadShowsBothEndsOfAHandoff(t *testing.T) {
	env := buildEnvScenario(t)
	saved := captureVars()
	defer restoreVars(saved)
	before := loadSnapshot(env.cfg).productStats["shop"].rejected7d

	now := time.Now()
	finished := now.Add(-2 * time.Minute)
	for _, d := range []*state.Dispatch{
		{
			ID: "rec-from", Feature: "orders endpoint", Slug: "orders-endpoint",
			RepoPath: env.repoAPath, RepoName: env.repoAName, Product: "shop", Branch: "main",
			Status: state.StatusExited, StatusReason: state.ReasonHandedOff + `"show orders" in shop-web`,
			FinishedAt: &finished, HandedOffTo: []string{"rec-to"},
			CreatedAt: now.Add(-time.Hour), UpdatedAt: finished,
		},
		{
			ID: "rec-to", Feature: "show orders", Slug: "show-orders",
			RepoPath: env.repoBPath, RepoName: env.repoBName, Product: "shop", Branch: "main",
			Status: state.StatusWorking, HandedOffFrom: "rec-from",
			CreatedAt: finished, UpdatedAt: now,
		},
	} {
		if err := state.Save(d); err != nil {
			t.Fatal(err)
		}
	}

	snap := loadSnapshot(env.cfg)
	rows := map[string]fleetRow{}
	for _, r := range snap.fleet {
		rows[r.id] = r
	}
	from, to := rows["rec-from"], rows["rec-to"]
	if from.kind != "done" || from.signal != "handed off" {
		t.Errorf("the dispatcher that handed off is a %q row saying %q, want a held row saying handed off", from.kind, from.signal)
	}
	if want := `Handed off to "show orders" in shop-web.`; from.why != want || from.handoff != "" {
		t.Errorf("its panel leads %q with line %q, want it to say %q once", from.why, from.handoff, want)
	}
	if to.kind != "run" {
		t.Errorf("the dispatcher it started is a %q row, want it running on the table", to.kind)
	}
	if want := `handed off from "orders endpoint" in shop-api`; to.handoff != want {
		t.Errorf("its handoff line is %q, want %q", to.handoff, want)
	}
	if got := snap.productStats["shop"].rejected7d; got != before {
		t.Errorf("shop counts %d rejected, %d before — the handoff was counted as work turned down", got, before)
	}

	// And the detail panel says it, under everything else about the row.
	m := newModel()
	var lines []string
	for _, r := range m.fleetDetail(120, to) {
		lines = append(lines, stripAnsi(r.s))
	}
	if !strings.Contains(strings.Join(lines, "\n"), `handed off from "orders endpoint" in shop-api`) {
		t.Errorf("the detail panel does not say where the work came from:\n%s", strings.Join(lines, "\n"))
	}
}

// History counts a handoff as neither a ship nor a stop, and says nothing about
// handoffs where there are none.
func TestHistoryCountsAHandoffOnItsOwn(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	rows := []fleetRow{
		{id: "a", kind: "past", rank: fleetPastRank, feature: "csv export", signal: "deployed"},
		{id: "b", kind: "past", rank: fleetPastRank, feature: "orders endpoint", signal: "handed off"},
		{id: "c", kind: "past", rank: fleetPastRank, feature: "spike", signal: "stopped"},
	}
	m := newModel()
	got := plain(m.fleetHistoryHeadline(140, rows))
	for _, want := range []string{"3 finished", "1 shipped", "1 handed off", "1 stopped"} {
		if !strings.Contains(got, want) {
			t.Errorf("history headline %q does not say %q", got, want)
		}
	}
	if got := plain(m.fleetHistoryHeadline(140, rows[:1])); strings.Contains(got, "handed off") {
		t.Errorf("a history with no handoff mentions one: %q", got)
	}
}
