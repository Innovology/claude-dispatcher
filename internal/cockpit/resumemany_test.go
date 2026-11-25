package cockpit

import (
	"errors"
	"strings"
	"testing"

	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// manyFleet is a table with one running dispatcher, two held ones and one in
// history (which never ran), and the records behind them.
func manyFleet(t *testing.T) {
	t.Helper()
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	resume := []cqAct{{k: "⏎", d: "resume"}}
	fleet = []fleetRow{
		{id: "run", kind: "run", feature: "busy", repo: "r0", acts: []cqAct{{k: "⏎", d: "attach"}}},
		{id: "a", kind: "done", feature: "alpha", repo: "r1", acts: resume},
		{id: "b", kind: "done", feature: "beta", repo: "r2", acts: resume},
		{id: "c", kind: "past", feature: "gamma", repo: "r3", acts: []cqAct{{k: "⏎", d: "start again"}}},
	}
	liveByID = map[string]*state.Dispatch{
		"run": {ID: "run", Feature: "busy", SessionID: "s0"},
		"a":   {ID: "a", Feature: "alpha", SessionID: "sa"},
		"b":   {ID: "b", Feature: "beta", SessionID: "sb"},
		"c":   {ID: "c", Feature: "gamma", Prompt: "do it"},
	}
	// The batch resumes the records on disk, not the snapshot's.
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	for _, d := range liveByID {
		saveRec(t, d)
	}
}

func saveRec(t *testing.T, d *state.Dispatch) {
	t.Helper()
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}
}

// stubManyRun answers for the batch: every record comes back started except
// the ones fail names, which fail with that error.
func stubManyRun(t *testing.T, fail map[string]error) (asked *[]string, said *string) {
	t.Helper()
	prev := resumeManyRun
	t.Cleanup(func() { resumeManyRun = prev })
	var ids []string
	var msg string
	resumeManyRun = func(recs []*state.Dispatch, prompt string, report func(dispatchpkg.ResumeResult)) {
		msg = prompt
		for _, d := range recs {
			ids = append(ids, d.ID)
			r := dispatchpkg.ResumeResult{ID: d.ID, Feature: d.Feature, Mode: dispatchpkg.ResumeStarted, Session: "disp-" + d.ID}
			if err := fail[d.ID]; err != nil {
				r = dispatchpkg.ResumeResult{ID: d.ID, Feature: d.Feature, Err: err}
			}
			if d.ID == "live" {
				r.Mode = dispatchpkg.ResumeLive
			}
			report(r)
		}
	}
	return &ids, &msg
}

// drainMany runs a batch's commands to the end, through Update, the way the
// program would.
func drainMany(t *testing.T, m model, cmd func() any) model {
	t.Helper()
	msg := cmd()
	for i := 0; i < 50; i++ {
		switch v := msg.(type) {
		case manyResultMsg:
			next, _ := m.Update(v)
			m = next.(model)
			msg = manyNext(v.seq, v.ch)()
		case manyDoneMsg:
			next, _ := m.Update(v)
			return next.(model)
		default:
			t.Fatalf("the batch sent %T", msg)
		}
	}
	t.Fatal("the batch never finished")
	return m
}

func (m model) press(k string) model {
	m = m.fleetSync()
	mm, _, _ := m.updateFloorQueue(k)
	return mm.fleetSync()
}

func TestResumesOnIsWhatEnterDoes(t *testing.T) {
	for _, c := range []struct {
		r    fleetRow
		a    cqAct
		want bool
	}{
		{fleetRow{id: "x", kind: "past"}, cqAct{k: "⏎", d: "resume"}, true},
		{fleetRow{id: "x", kind: "done"}, cqAct{k: "⏎", d: "start again"}, true},
		{fleetRow{id: "x", kind: "parked"}, cqAct{k: "⏎", d: "resume"}, true},
		{fleetRow{id: "x", kind: "parked"}, cqAct{k: "⏎", d: "attach"}, false},
		{fleetRow{id: "x", kind: "run"}, cqAct{k: "⏎", d: "attach"}, false},
		{fleetRow{id: "x", kind: "done"}, cqAct{k: "x", d: "dismiss"}, false},
		{fleetRow{id: pendingPrefix + "x", kind: "done"}, cqAct{k: "⏎", d: "resume"}, false},
	} {
		if got := cqResumesOn(c.r, c.a); got != c.want {
			t.Errorf("%s row, %s %s: %v, want %v", c.r.kind, c.a.k, c.a.d, got, c.want)
		}
		c.r.acts = []cqAct{c.a}
		if got := cqResumable(c.r); got != c.want {
			t.Errorf("%s row whose ⏎ is %s: resumable %v, want %v", c.r.kind, c.a.d, got, c.want)
		}
	}
}

func TestSpaceMarksOnlyWhatEnterWouldResume(t *testing.T) {
	manyFleet(t)
	m := newModel().fleetTo(0).press(" ")
	if len(m.fleetMarked) != 0 || !strings.Contains(m.notice, "still running") {
		t.Fatalf("marked a running dispatcher: %v, notice %q", m.fleetMarked, m.notice)
	}
	m = m.fleetTo(1).press(" ")
	if !m.fleetMarked["a"] {
		t.Fatal("space did not mark a finished row")
	}
	m = m.press(" ")
	if m.fleetMarked["a"] {
		t.Fatal("space again did not unmark it")
	}
	m = m.press(" ").fleetTo(2).press(" ").press("esc")
	if len(m.fleetMarked) != 0 || m.notice != "marks cleared" {
		t.Fatalf("esc left %v, notice %q", m.fleetMarked, m.notice)
	}
}

func TestMarksShowOnTheRowAndInTheFooter(t *testing.T) {
	manyFleet(t)
	m := newModel().fleetTo(1)
	if f := m.cqFooterHelp(); !strings.Contains(f, "space mark") || strings.Contains(f, "R resume") {
		t.Errorf("before marking, footer = %q", f)
	}
	m = m.press(" ")
	f := m.cqFooterHelp()
	for _, want := range []string{"space unmark", "R resume 1 marked", "esc clear marks"} {
		if !strings.Contains(f, want) {
			t.Errorf("footer lacks %q: %q", want, f)
		}
	}
	// On a running row there is nothing to mark, and the marks still say what
	// R will do.
	if f := m.fleetTo(0).cqFooterHelp(); strings.Contains(f, "space") || !strings.Contains(f, "R resume 1 marked") {
		t.Errorf("on a running row, footer = %q", f)
	}
	var markedLine string
	for _, ln := range strings.Split(stripAnsi(m.viewFleet(150, 30)), "\n") {
		if strings.Contains(ln, "alpha") {
			markedLine = ln // the table row; the detail panel names it again below
			break
		}
	}
	if !strings.Contains(markedLine, "◆") {
		t.Errorf("the marked row is drawn %q, want the mark in place of its glyph", markedLine)
	}
}

func TestRResumesEveryMarkedRowAndKeepsTheOnesThatFail(t *testing.T) {
	manyFleet(t)
	asked, said := stubManyRun(t, map[string]error{"b": errors.New("tmux: server exited\nthe rest of it")})
	m := newModel().fleetTo(1).press(" ").fleetTo(2).press(" ")
	// The one in history is marked from history, and R still finds it.
	m = m.fleetSetFilter(fleetHistory).fleetTo(0).press(" ").fleetSetFilter(fleetFilters[0])

	m = m.press("R")
	if !m.manyOpen || len(m.manyAt) != 3 {
		t.Fatalf("R opened %v with %+v", m.manyOpen, m.manyAt)
	}
	if m.manyAt[0].id != "a" || m.manyAt[1].id != "b" || m.manyAt[2].id != "c" || !m.manyAt[2].fresh {
		t.Errorf("targets = %+v, want a, b, then c, which never ran", m.manyAt)
	}
	v := stripAnsi(m.viewMany(100, 30))
	for _, want := range []string{"resume 3 dispatchers", "alpha", "beta", "gamma", "never ran", "nothing is attached"} {
		if !strings.Contains(v, want) {
			t.Errorf("the input lacks %q:\n%s", want, v)
		}
	}

	m.manyText = "  continue "
	m, cmd := m.updateMany("enter")
	if m.manyOpen || m.manyRun == nil || cmd == nil {
		t.Fatalf("enter did not start the batch: open %v run %v", m.manyOpen, m.manyRun)
	}
	if !strings.Contains(m.notice, "resuming 3") {
		t.Errorf("progress = %q", m.notice)
	}
	if got := m.press("R"); !strings.Contains(got.notice, "still under way") || got.manyOpen {
		t.Errorf("R during a batch: open %v, notice %q", got.manyOpen, got.notice)
	}
	m = drainMany(t, m, func() any { return cmd() })

	if strings.Join(*asked, ",") != "a,b,c" || *said != "continue" {
		t.Errorf("resumed %v saying %q", *asked, *said)
	}
	if m.manyRun != nil {
		t.Error("the batch is still under way after its last outcome")
	}
	if len(m.fleetMarked) != 1 || !m.fleetMarked["b"] {
		t.Errorf("marks = %v, want only the one that did not come back", m.fleetMarked)
	}
	for _, want := range []string{"resumed 2 of 3", "1 did not and is still marked", `"beta": tmux: server exited`} {
		if !strings.Contains(m.notice, want) {
			t.Errorf("summary lacks %q: %q", want, m.notice)
		}
	}
	if strings.Contains(m.notice, "the rest of it") {
		t.Errorf("summary carries more than the cause: %q", m.notice)
	}
	var detail []string
	for _, r := range m.fleetDetail(120, fleet[2]) {
		detail = append(detail, stripAnsi(r.s))
	}
	if !strings.Contains(strings.Join(detail, "\n"), "did not resume · tmux: server exited") {
		t.Errorf("the failed row does not say why:\n%s", strings.Join(detail, "\n"))
	}
}

func TestRWithNothingMarkedTakesTheRowUnderTheCursor(t *testing.T) {
	manyFleet(t)
	if m := newModel().fleetTo(0).press("R"); m.manyOpen || !strings.Contains(m.notice, "nothing to resume") {
		t.Errorf("R on a running row: open %v, notice %q", m.manyOpen, m.notice)
	}
	m := newModel().fleetTo(2).press("R")
	if !m.manyOpen || len(m.manyAt) != 1 || m.manyAt[0].id != "b" {
		t.Fatalf("R on a held row: %+v", m.manyAt)
	}
	if v := stripAnsi(m.viewMany(100, 30)); !strings.Contains(v, "resume this dispatcher") {
		t.Errorf("one target reads:\n%s", v)
	}
	if m, _ = m.updateMany("esc"); m.manyOpen || m.manyAt != nil {
		t.Error("esc did not close the input")
	}
}

func TestADispatcherStillRunningLosesItsMarkAndIsNotSentTheLine(t *testing.T) {
	manyFleet(t)
	fleet = append(fleet, fleetRow{id: "live", kind: "done", feature: "lively", acts: []cqAct{{k: "⏎", d: "resume"}}})
	liveByID["live"] = &state.Dispatch{ID: "live", Feature: "lively", SessionID: "sl"}
	saveRec(t, liveByID["live"])
	stubManyRun(t, nil)
	m := newModel()
	m.fleetMarked = map[string]bool{"live": true}
	m = m.press("R")
	m.manyText = "go on"
	m, cmd := m.updateMany("enter")
	m = drainMany(t, m, func() any { return cmd() })
	if len(m.fleetMarked) != 0 {
		t.Errorf("marks = %v", m.fleetMarked)
	}
	if !strings.Contains(m.notice, "1 still running, the line not sent to it") || strings.Contains(m.notice, "resumed") {
		t.Errorf("summary = %q", m.notice)
	}
}

func TestAMarkGoesWhenItsRowCanNoLongerBeResumed(t *testing.T) {
	manyFleet(t)
	m := newModel()
	m.fleetMarked = map[string]bool{"a": true, "b": true, "gone": true}
	m.manyFailed = map[string]string{"a": "earlier"}
	// a was resumed some other way and is running; gone has left the store.
	fleet[1].kind, fleet[1].acts = "run", []cqAct{{k: "⏎", d: "attach"}}
	m = m.cqReconcile()
	if len(m.fleetMarked) != 1 || !m.fleetMarked["b"] || m.manyFailed["a"] != "" {
		t.Errorf("after the load: marks %v, reasons %v", m.fleetMarked, m.manyFailed)
	}
}

// The batch reads the records from disk as it starts; one that has gone since
// the table was drawn is a failure like any other, and keeps its mark.
func TestARecordGoneFromDiskDoesNotResumeAndKeepsItsMark(t *testing.T) {
	manyFleet(t)
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	saveRec(t, liveByID["a"])
	asked, _ := stubManyRun(t, nil)
	m := newModel()
	m.fleetMarked = map[string]bool{"a": true, "b": true}
	m = m.press("R")
	m, cmd := m.updateMany("enter")
	m = drainMany(t, m, func() any { return cmd() })
	if strings.Join(*asked, ",") != "a" {
		t.Errorf("resumed %v, want only the record still on disk", *asked)
	}
	if !m.fleetMarked["b"] || m.manyFailed["b"] != "its record is gone" {
		t.Errorf("marks %v, reasons %v", m.fleetMarked, m.manyFailed)
	}
	if !strings.Contains(m.notice, "resumed 1 of 2") {
		t.Errorf("summary = %q", m.notice)
	}
}

// Through the program's own key path and screen, as the human meets it: R on
// the triage lens opens the input over the table, its footer says what enter
// does, an upgrade holds while a line is half typed, and esc lets it go.
func TestTheResumeInputIsTheScreenWhileItIsOpen(t *testing.T) {
	manyFleet(t)
	m := newModel()
	m.lens = "floor"
	m.width, m.height = 160, 40
	m = m.fleetTo(1)

	next, _ := m.handleKey("R")
	mm := next.(model)
	if !mm.manyOpen {
		t.Fatal("R on a held row did not open the input")
	}
	v := stripAnsi(mm.View())
	for _, want := range []string{"resume this dispatcher", "alpha", "enter resumes them"} {
		if !strings.Contains(v, want) {
			t.Errorf("the input is not on screen: missing %q", want)
		}
	}
	if f := mm.cqFooterHelp(); !strings.Contains(f, "enter resumes them") {
		t.Errorf("footer while the input is open = %q", f)
	}
	if !mm.inputPending() {
		t.Error("an upgrade could re-exec over a half-typed line")
	}
	next, _ = mm.handleKey("esc")
	if mm = next.(model); mm.manyOpen || strings.Contains(stripAnsi(mm.View()), "resume this dispatcher") {
		t.Error("esc did not close the input")
	}
}
