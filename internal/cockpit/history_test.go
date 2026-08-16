package cockpit

// history_test.go covers the disappearing-session bug in both places it bit:
// the triage lens, which collected only what was in flight and dropped every
// finished record on the floor, and the product panel, whose SHIPPED tab is a
// ship log and could not see a dispatcher that ended any other way.
//
// The through-line of every test here is that a dispatcher is still reachable
// after its session ends — a record, a branch and a transcript do not stop
// existing because a turn did — and that the way back into one really resumes
// it rather than announcing that it did.

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// ---- the collector ----------------------------------------------------------

// collectFleet used to switch on blocked/needs/review/working and drop
// everything else, so a shipped dispatcher and a killed one both vanished from
// the cockpit for good.
func TestCollectFleetKeepsFinishedDispatchers(t *testing.T) {
	env := buildEnvScenario(t)
	saved := captureVars()
	defer restoreVars(saved)

	snap := loadSnapshot(env.cfg)
	byID := map[string]fleetRow{}
	for _, r := range snap.fleet {
		byID[r.id] = r
	}

	// rec-done shipped (merged + deployed); rec-exited was killed without ever
	// opening a PR, which is the case floorState dropped entirely.
	for id, wantSignal := range map[string]string{"rec-done": "deployed", "rec-exited": "stopped"} {
		r, ok := byID[id]
		if !ok {
			t.Fatalf("%s is missing from the fleet — a finished dispatcher the cockpit cannot show is one it has lost", id)
		}
		if r.kind != "past" {
			t.Errorf("%s kind = %q, want past", id, r.kind)
		}
		if r.signal != wantSignal {
			t.Errorf("%s signal = %q, want %q", id, r.signal, wantSignal)
		}
		if r.rank != fleetPastRank {
			t.Errorf("%s rank = %d, want %d", id, r.rank, fleetPastRank)
		}
	}

	// And the record is reachable by id, which is how an act on a finished row
	// finds the session to resume.
	if snap.recordsByID["rec-exited"] == nil {
		t.Error("an exited record must still be addressable — nothing else can resume it")
	}
}

// The live table and the history table are separate questions. Every "is
// anything in flight" test in the cockpit is fleetAll's length, so history
// leaking into it would make an idle machine look busy.
func TestHistoryStaysOutOfTheLiveTable(t *testing.T) {
	m := cqModel(t)
	for _, r := range m.fleetAll() {
		if r.kind == "past" {
			t.Fatalf("%q is finished and still on the live table", r.feature)
		}
	}
	past := m.fleetPast()
	if len(past) != 1 || past[0].feature != "four" {
		t.Fatalf("history = %#v", past)
	}
}

// Newest first: the thing that just ended is the one you are most likely to
// want back.
func TestHistoryIsNewestFirst(t *testing.T) {
	now := time.Now()
	rows := []fleetRow{
		{id: "old", kind: "past", rank: fleetPastRank, moved: now.Add(-3 * time.Hour)},
		{id: "new", kind: "past", rank: fleetPastRank, moved: now.Add(-2 * time.Minute)},
		{id: "mid", kind: "past", rank: fleetPastRank, moved: now.Add(-30 * time.Minute)},
	}
	fleetSort(rows)
	if rows[0].id != "new" || rows[1].id != "mid" || rows[2].id != "old" {
		t.Errorf("history order = %s,%s,%s", rows[0].id, rows[1].id, rows[2].id)
	}
}

// ---- the triage lens --------------------------------------------------------

// `h` is the way in and the way out, and the table it shows is the finished
// dispatchers.
func TestHistoryKeyTogglesTheTable(t *testing.T) {
	m := cqModel(t)
	m = press(m, "h")
	if m.fleetFilter() != fleetHistory {
		t.Fatalf("h left the filter at %q", m.fleetFilter())
	}
	if got := fleetFeatures(m); got != "four" {
		t.Errorf("history shows %q, want the finished dispatcher", got)
	}
	if !strings.Contains(m.View(), "four") {
		t.Error("the history table does not render the row it holds")
	}
	m = press(m, "h")
	if m.fleetFilter() != "all" {
		t.Errorf("h should come back to the fleet, filter = %q", m.fleetFilter())
	}
}

// The dispatch form opens over an empty fleet and owns the keyboard. History is
// exactly what a human wants when nothing is in flight, so the form must not
// cover it — and `h` must reach it from inside the form.
func TestHistoryIsReachableWithNothingInFlight(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	now := time.Now()
	fleet = []fleetRow{{
		id: "id-past", kind: "past", rank: fleetPastRank, feature: "shipped thing",
		repo: "acme-api", signal: "merged", moved: now.Add(-time.Hour),
		acts: []cqAct{{k: "⏎", d: "resume", ok: "resuming…", keep: true}},
	}}

	m := newModel()
	m.width, m.height = 130, 40
	if !m.cqFormOn() {
		t.Fatal("with nothing in flight the dispatch form should be the screen")
	}
	m = press(m, "h")
	if m.cqFormOn() {
		t.Fatal("the dispatch form is still covering the history table")
	}
	if m.cqPromptOn() {
		t.Fatal("the dispatch form still owns the keyboard over the history table")
	}
	out := m.View()
	if !strings.Contains(out, "shipped thing") {
		t.Errorf("history did not render:\n%s", out)
	}
	// And the cursor works on it, which it cannot if the form is eating keys.
	m = press(m, "j")
	if r, ok := m.fleetSel(); !ok || r.id != "id-past" {
		t.Errorf("no selectable history row: %#v", r)
	}
}

// ⏎ on a finished row resumes; it must not try to attach to a session that
// ended. With no record behind the fixture row, the command reports that
// rather than doing nothing.
func TestEnterOnAHistoryRowResumes(t *testing.T) {
	m := cqModel(t)
	m = press(m, "h")
	r, ok := m.fleetSel()
	if !ok || r.kind != "past" {
		t.Fatalf("expected a history row under the cursor, got %#v", r)
	}
	if r.acts[0].d != "resume" {
		t.Errorf("the first act on a finished row is %q, want resume", r.acts[0].d)
	}
	mm, cmd := m.cqRun(r, r.acts[0])
	if cmd == nil {
		t.Fatal("⏎ on a finished row returned no command")
	}
	if strings.Contains(mm.notice, "no live") {
		t.Errorf("⏎ tried to attach to a finished session: %q", mm.notice)
	}
	msg, _ := cmd().(actionMsg)
	if !strings.Contains(msg.notice, "resume") {
		t.Errorf("resume notice = %q", msg.notice)
	}
}

// A finished dispatcher has nothing to approve and nothing to kill, and the
// footer only names keys that work right now.
func TestHistoryFooterOffersOnlyWhatWorks(t *testing.T) {
	m := press(cqModel(t), "h")
	got := m.footerHelp()
	for _, gone := range []string{"y ", "x kill", "s skip", "u undo"} {
		if strings.Contains(got, gone) {
			t.Errorf("history footer offers %q: %s", gone, got)
		}
	}
	if !strings.Contains(got, "⏎ resume") || !strings.Contains(got, "h back to the fleet") {
		t.Errorf("history footer = %q", got)
	}
}

// ---- the product panel ------------------------------------------------------

// The product's HISTORY tab lists every finished dispatcher in it, including
// the ones SHIPPED cannot show: a kill, and a feature marked shipped by hand
// with no merge behind it.
func TestProductHistoryTabListsEverySession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	saved := captureVars()
	defer restoreVars(saved)

	now := time.Now()
	merged := now.Add(-2 * time.Hour)
	recs := []*state.Dispatch{
		{ID: "r-ship", Feature: "seat limits", RepoName: "shop-api", Product: "shop",
			PRNumber: 9, PRState: "MERGED", PRMergedAt: &merged, SessionID: "sess-ship",
			Status: state.StatusDone, StatusReason: "deployed — live",
			CreatedAt: now.Add(-5 * time.Hour), UpdatedAt: merged},
		{ID: "r-hand", Feature: "docs tidy", RepoName: "shop-api", Product: "shop",
			SessionID: "sess-hand", Status: state.StatusDone, StatusReason: "marked shipped",
			CreatedAt: now.Add(-4 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: "r-kill", Feature: "abandoned spike", RepoName: "shop-api", Product: "shop",
			SessionID: "sess-kill", Status: state.StatusExited, StatusReason: "killed from cockpit",
			CreatedAt: now.Add(-90 * time.Minute), UpdatedAt: now.Add(-time.Hour)},
	}
	for _, r := range recs {
		if err := state.Save(r); err != nil {
			t.Fatal(err)
		}
	}

	var s snapshot
	cfg := &config.Config{Products: map[string][]string{"shop": {"shop-api"}}}
	collectProducts(&collectCtx{cfg: cfg, records: state.LoadAll()}, &s)

	hist := s.productHistory["shop"]
	if len(hist) != 3 {
		t.Fatalf("history = %#v", hist)
	}
	// Newest first, and each says how it ended.
	want := []struct{ feature, ended string }{
		{"abandoned spike", "stopped"},
		{"docs tidy", "marked shipped"},
		{"seat limits", "merged"},
	}
	for i, w := range want {
		if hist[i].feature != w.feature || hist[i].ended != w.ended {
			t.Errorf("history[%d] = %q/%q, want %q/%q", i, hist[i].feature, hist[i].ended, w.feature, w.ended)
		}
	}
	// The ship log still says only what shipped — history is the wider list, not
	// a replacement for it.
	shippedCount := 0
	for _, d := range s.shipped["shop"] {
		shippedCount += len(d.items)
	}
	if shippedCount != 1 {
		t.Errorf("shipped items = %d, want just the merged one", shippedCount)
	}
}

// enter on a history row opens the resume overlay against that row, and
// submitting it resumes the recorded session — with or without a follow-up,
// because "just give it back to me" is a real ask.
func TestProductHistoryResumesTheSession(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	products = []product{{name: "acme"}}
	productHistory = map[string][]historyItem{"acme": {
		{id: "rec-1", feature: "csv export", repo: "acme-hq", pr: "#144",
			at: "2h ago", ended: "stopped", session: "sess-1", prompt: "export a csv"},
	}}

	m := newModel()
	m.width, m.height = 190, 44
	m.lens = "product"
	m.rightTab = "history"

	mm, _ := m.updateProduct("enter")
	if !mm.resumeOpen {
		t.Fatal("enter on a finished dispatcher should open the resume overlay")
	}
	if mm.resumeAt == nil || mm.resumeAt.id != "rec-1" {
		t.Fatalf("the overlay is about %#v, want the selected history row", mm.resumeAt)
	}
	if !strings.Contains(mm.View(), "sess-1") {
		t.Error("the overlay does not say which session it reopens")
	}

	// Empty is a resume, not a refusal: there is a session to reopen.
	done, cmd := mm.updateProduct("enter")
	if cmd == nil {
		t.Fatal("submitting an empty resume did nothing")
	}
	if strings.Contains(done.notice, "nothing to dispatch") {
		t.Errorf("notice = %q — a recorded session can be reopened with nothing to add", done.notice)
	}
	if done.resumeOpen || done.resumeAt != nil {
		t.Error("submitting should close the overlay and clear its target")
	}
}

// A record with no session id cannot be resumed, and the overlay says so
// instead of promising a full-context resume it cannot perform — which is what
// the SHIPPED tab's copy did while launching a brand new session underneath.
func TestResumeOverlayIsHonestWithoutASession(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	products = []product{{name: "acme"}}
	productHistory = map[string][]historyItem{"acme": {
		{id: "rec-2", feature: "old thing", repo: "acme-hq", ended: "stopped", prompt: "do it"},
	}}

	m := newModel()
	m.width, m.height = 190, 44
	m.lens = "product"
	m.rightTab = "history"
	mm, _ := m.updateProduct("enter")

	out := mm.View()
	if !strings.Contains(out, "no session was recorded") {
		t.Errorf("overlay copy does not admit there is nothing to resume:\n%s", out)
	}
	if strings.Contains(out, "reopens session") {
		t.Error("overlay promises to reopen a session that was never recorded")
	}
	// Without a session a fresh dispatch is the only honest offer, and it needs
	// a prompt.
	empty, cmd := mm.updateProduct("enter")
	if cmd != nil {
		t.Error("an empty prompt with nothing to resume should launch nothing")
	}
	if !strings.Contains(empty.notice, "nothing to dispatch") {
		t.Errorf("notice = %q", empty.notice)
	}
}
