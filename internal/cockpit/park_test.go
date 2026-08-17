package cockpit

// park_test.go covers the parking shelf: a queue row the human cannot answer
// right now is parked under a typed reason and drops to its own group at the
// bottom of the fleet. The tests pin the three claims the feature makes — the
// shelf is grouped and ranked below everything live, the reason is required
// and displayed, and the shelf survives everything except the human taking
// the dispatcher back up.

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

// parkedFixtureRow is a parked dispatcher on the triage table, appended to the
// standard fleet fixture by the tests that need one.
func parkedFixtureRow(now time.Time) fleetRow {
	return fleetRow{
		id: "id-parked", kind: "parked", rank: fleetParkedRank, product: "alpha",
		feature: "five", repo: "alpha-api", ref: "feature/five",
		signal: "parked · waiting on legal", tone: "normal",
		why: "Waiting on legal.",
		acts: []cqAct{
			{k: "⏎", d: "attach", ok: "attaching to alpha-api session…", keep: true},
			{k: "p", d: "unpark", ok: "\"five\" back on the fleet", keep: true},
			{k: "x", d: "kill", ok: "killed \"five\""},
		},
		parked: now.Add(-time.Hour), moved: now.Add(-time.Hour),
		waited: now.Add(-time.Hour), started: now.Add(-5 * time.Hour),
	}
}

// ---- the collector ----------------------------------------------------------

// A parked record lands in the parked group whatever its machine status says —
// waiting, or a session a reboot took — and leaves it only by shipping.
func TestCollectFleetGroupsParkedRecords(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	at := time.Now().Add(-2 * time.Hour)
	recs := []*state.Dispatch{
		{ID: "p-needs", Feature: "one", Status: state.StatusNeedsInput,
			ParkedReason: "waiting on legal", ParkedAt: &at},
		// The shelf survives the session dying: a reboot kills tmux, not the
		// human's intent to come back.
		{ID: "p-dead", Feature: "two", Status: state.StatusExited,
			ParkedReason: "needs the new api key", ParkedAt: &at},
		// "live" outranks the shelf: the work shipped, so the entry is over.
		{ID: "p-done", Feature: "three", Status: state.StatusDone,
			ParkedReason: "stale", ParkedAt: &at},
		// And an unparked ask is a queue row like any other.
		{ID: "q-plain", Feature: "four", Status: state.StatusNeedsInput},
	}

	s := &snapshot{}
	collectFleet(&collectCtx{records: recs}, s)
	byID := map[string]fleetRow{}
	for _, r := range s.fleet {
		byID[r.id] = r
	}

	for _, id := range []string{"p-needs", "p-dead"} {
		r := byID[id]
		if r.kind != "parked" || r.rank != fleetParkedRank {
			t.Errorf("%s = kind %q rank %d, want a parked row at rank %d", id, r.kind, r.rank, fleetParkedRank)
		}
	}
	if got := byID["p-needs"].signal; got != "parked · waiting on legal" {
		t.Errorf("signal = %q — the reason is the one fact the group exists for", got)
	}
	if got := byID["p-needs"].why; got != "Waiting on legal." {
		t.Errorf("why = %q, want the reason as the panel's lead sentence", got)
	}
	if r := byID["p-done"]; r.kind != "past" {
		t.Errorf("a shipped record = kind %q, want past — done means live, shelf or no shelf", r.kind)
	}
	if r := byID["q-plain"]; r.kind != "queue" {
		t.Errorf("an unparked ask = kind %q, want queue", r.kind)
	}
}

// ---- rank and order ---------------------------------------------------------

func TestParkedRankSitsBetweenRunAndHistory(t *testing.T) {
	if got := fleetRank("parked", "normal"); got != fleetParkedRank {
		t.Fatalf("fleetRank(parked) = %d, want %d", got, fleetParkedRank)
	}
	if fleetParkedRank <= fleetRank("run", "normal") || fleetParkedRank >= fleetPastRank {
		t.Error("parked must sort below every live row and above history")
	}
	if fleetGlyph(fleetParkedRank) != "‖" {
		t.Error("the parked glyph does not match the help sheet's legend")
	}
	if fleetRankColor(fleetParkedRank) != cFaint {
		t.Error("a shelved row must be quiet, not coloured like a demand")
	}
}

func TestFleetSortPutsParkedAfterRunNewestFirst(t *testing.T) {
	now := time.Now()
	rows := []fleetRow{
		{id: "old-park", kind: "parked", rank: fleetParkedRank, parked: now.Add(-3 * time.Hour)},
		{id: "run", kind: "run", rank: 2, moved: now},
		{id: "new-park", kind: "parked", rank: fleetParkedRank, parked: now.Add(-time.Minute)},
		{id: "ask", kind: "queue", ask: "needs", rank: 1, waited: now},
		{id: "gone", kind: "past", rank: fleetPastRank, moved: now},
	}
	fleetSort(rows)
	var order []string
	for _, r := range rows {
		order = append(order, r.id)
	}
	want := "ask,run,new-park,old-park,gone"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
}

// A parked id the user ordered while it was still a queue row must not drag
// its parked self back to the top of the table.
func TestFleetAllKeepsParkedBelowTheUserOrdering(t *testing.T) {
	m := cqModel(t)
	prev := fleet
	t.Cleanup(func() { fleet = prev })
	// The collector's slice is always fleetSort'd, so the parked row arrives
	// after every live one — what this test pins is that cqOrder cannot lift
	// it back out.
	fleet = append(fleet, parkedFixtureRow(time.Now()))
	m.cqOrder = []string{"id-parked", "id-one", "id-two"}

	all := m.fleetAll()
	if got := all[len(all)-1].id; got != "id-parked" {
		t.Errorf("last live row = %q, want the parked one whatever the ordering says", got)
	}
	for i, r := range all[:len(all)-1] {
		if r.kind == "parked" {
			t.Errorf("parked row surfaced at position %d", i)
		}
	}
}

func TestFleetCountSeparatesParkedFromClean(t *testing.T) {
	rows := []fleetRow{
		{kind: "queue"},
		{kind: "run", rank: 2},
		{kind: "parked", rank: fleetParkedRank},
	}
	wants, parked, clean := fleetCount(rows)
	if wants != 1 || parked != 1 || clean != 1 {
		t.Errorf("fleetCount = %d,%d,%d — a shelved row is not running clean", wants, parked, clean)
	}
}

// ---- the divider ------------------------------------------------------------

// The parked group is the one part of the table with a header: a divider line
// above the first parked row. It is a display line, not a row, so the body
// still fits its height exactly and the selection maps across it.
func TestFleetBodyDrawsTheParkedDivider(t *testing.T) {
	now := time.Now()
	rows := []fleetRow{
		{id: "a", kind: "queue", rank: 1, feature: "ask"},
		{id: "b", kind: "parked", rank: fleetParkedRank, feature: "shelved", parked: now},
	}
	cols := fleetColumns(130, 12)
	h := 6
	lines := fleetBody(130, cols, rows, 1, h, "")
	if len(lines) != h {
		t.Fatalf("body = %d lines, want exactly %d — the divider must not overflow the box", len(lines), h)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "parked") || !strings.Contains(joined, "──") {
		t.Error("the parked group has no divider above it")
	}
	if !strings.Contains(joined, "shelved") || !strings.Contains(joined, "ask") {
		t.Error("a row went missing to make room for the divider")
	}
	if strings.Index(joined, "ask") > strings.Index(joined, "parked") {
		t.Error("the divider must sit between the live rows and the shelf")
	}

	// Without a parked row there is no divider to draw.
	plain := strings.Join(fleetBody(130, cols, rows[:1], 0, h, ""), "\n")
	if strings.Contains(plain, "──") {
		t.Error("a table with nothing parked drew a divider anyway")
	}
}

// ---- the keys ---------------------------------------------------------------

// p on a queue row opens the reason input; enter with a reason parks. The
// input owns the keyboard — letters are text, not fleet keys.
func TestParkKeyAsksWhyThenParks(t *testing.T) {
	m := cqModel(t)
	m = press(m, "p")
	if !m.parkOpen || m.parkAt == nil || m.parkAt.id != "id-one" {
		t.Fatalf("p did not open the reason input for the selected row (open=%v at=%v)", m.parkOpen, m.parkAt)
	}
	m = typeBurst(m, "waiting on legal")
	if m.parkText != "waiting on legal" {
		t.Fatalf("typed reason = %q", m.parkText)
	}
	if !strings.Contains(m.View(), "waiting on legal") {
		t.Error("the input does not render what is being typed")
	}

	next, cmd := m.handleKey("enter")
	m = next.(model)
	if m.parkOpen || m.parkAt != nil || m.parkText != "" {
		t.Error("submitting did not close the input")
	}
	if cmd == nil {
		t.Error("submitting a reason must reach the park command")
	}
}

// The reason is the point: enter with nothing typed parks nothing.
func TestParkRefusesAnEmptyReason(t *testing.T) {
	m := cqModel(t)
	m = press(m, "p")
	next, cmd := m.handleKey("enter")
	m = next.(model)
	if !m.parkOpen {
		t.Error("an empty submit closed the input instead of asking for the reason")
	}
	if cmd != nil {
		t.Error("an empty submit must not write anything")
	}
	if m.notice == "" {
		t.Error("refusing silently reads as a dead key — say why")
	}

	m = press(m, "esc")
	if m.parkOpen || m.parkAt != nil {
		t.Error("esc did not cancel the input")
	}
}

// p means park only where there is an ask to park: a running dispatcher has
// not asked anything, and on a parked row the same key takes it back up.
func TestParkKeyOnlyActsWhereItMeansSomething(t *testing.T) {
	m := cqModel(t)
	m = press(m, "j")
	m = press(m, "j") // onto the running row
	if r, _ := m.fleetSel(); r.kind != "run" {
		t.Fatalf("cursor on %q, want the running row", r.kind)
	}
	m = press(m, "p")
	if m.parkOpen {
		t.Error("p opened the park input on a running row")
	}

	// A parked row's p is the unpark act, which fires like any other act.
	prev := fleet
	t.Cleanup(func() { fleet = prev })
	fleet = append(fleet, parkedFixtureRow(time.Now()))
	m = m.fleetSync()
	m = press(m, "G")
	if r, _ := m.fleetSel(); r.kind != "parked" {
		t.Fatalf("cursor on %q, want the parked row", r.kind)
	}
	m = press(m, "p")
	if m.parkOpen {
		t.Error("p on a parked row must unpark, not open the input again")
	}
	if !strings.Contains(m.cqFlash, "back on the fleet") {
		t.Errorf("flash = %q, want the unpark confirmation", m.cqFlash)
	}
}

// ---- the acts ---------------------------------------------------------------

// Every act a parked row advertises reaches a real command, and which ⏎ it
// offers follows the record: a live session attaches, a dead one resumes.
func TestParkedActsFollowTheRecord(t *testing.T) {
	alive := testRec("five", 2)
	alive.Status = state.StatusNeedsInput
	var keys []string
	for _, a := range cqActs(alive, "parked") {
		keys = append(keys, a.k+" "+a.d)
	}
	if got := strings.Join(keys, " · "); got != "⏎ attach · p unpark · x kill" {
		t.Errorf("parked acts = %q", got)
	}

	dead := testRec("five", 2)
	dead.Status = state.StatusExited
	keys = nil
	for _, a := range cqActs(dead, "parked") {
		keys = append(keys, a.k+" "+a.d)
	}
	if got := strings.Join(keys, " · "); got != "⏎ resume · p unpark" {
		t.Errorf("parked acts on a dead session = %q — there is no session to attach or kill", got)
	}

	// And the commands behind them are real (see cq_acts_test's contract).
	row := fleetRow{id: "id-parked", kind: "parked", feature: "five"}
	if _, cmd := newModel().cqRun(row, cqAct{k: "p", d: "unpark"}); cmd == nil {
		t.Error("unpark reaches no command")
	}
	if _, cmd := newModel().cqRun(row, cqAct{k: "⏎", d: "resume"}); cmd == nil {
		t.Error("resume on a parked row with a dead session reaches no command")
	}
}

// The queue rows advertise the park key so the footer can say it, but the act
// deliberately carries no ok: cqRun must never fire it — the reason input is
// the only way onto the shelf.
func TestQueueRowsAdvertiseParkWithoutACommand(t *testing.T) {
	var park *cqAct
	for _, a := range cqActs(testRec("one", 3), "turn-done") {
		if a.k == "p" {
			aa := a
			park = &aa
		}
	}
	if park == nil {
		t.Fatal("queue rows do not advertise the park key")
	}
	if park.ok != "" {
		t.Error("the park act must not flash — nothing is parked until the reason is entered")
	}
}
