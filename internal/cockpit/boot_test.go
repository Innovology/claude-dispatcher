package cockpit

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
)

// bootAt returns a model showing the opening screen at the given terminal size.
func bootAt(w, h int) model {
	m := newModel()
	m.width, m.height = w, h
	// Mirrors what Run does when a config loads: the screen goes up, and the
	// cockpit behind it is loading until the snapshot lands.
	m.loading = true
	m.boot = newBootState()
	m.bootCh = make(chan bootUpdate, bootChanBuffer)
	return m
}

// TestBootSequenceCoversEveryReportedStep is the guard that keeps the screen
// honest: every id loadSnapshotReporting reports against must have a line to
// land on, and every line must have something reporting into it. A step drawn
// on screen that nothing ever fills would sit at pending until finish() swept
// it, which reads as work that never happened.
func TestBootSequenceCoversEveryReportedStep(t *testing.T) {
	// An empty state dir and an empty config give a portfolio with no records
	// and no roots, so every stage runs and reports without touching the
	// developer's own dispatches. The reporter is called from
	// loadSnapshotReporting's own goroutine, in order, so the map needs no
	// guarding.
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	defer restoreVars(captureVars())

	seen := map[string]bool{}
	loadSnapshotReporting(&config.Config{}, func(u bootUpdate) { seen[u.id] = true })

	for _, s := range bootSequence {
		if !seen[s.id] {
			t.Errorf("step %q is drawn on the opening screen but nothing reports it", s.id)
		}
		delete(seen, s.id)
	}
	for id := range seen {
		t.Errorf("loadSnapshotReporting reports %q, which no boot step draws", id)
	}
}

// TestLoadSnapshotIgnoresNilReporter checks the ordinary refresh path: no
// screen, no reporter, and the nil-safe methods must not panic.
func TestLoadSnapshotIgnoresNilReporter(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	defer restoreVars(captureVars())

	if s := loadSnapshot(&config.Config{}); s.dataMode != "live" {
		t.Errorf("dataMode = %q, want live", s.dataMode)
	}
}

// TestBootLoadCmdReportsThenCloses covers the loader command end to end: it
// produces the same snapshot as any other load, it fills the channel on the
// way, and it closes it afterwards so the last waitBoot is released rather
// than parked on a channel nothing will ever send to again.
func TestBootLoadCmdReportsThenCloses(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	defer restoreVars(captureVars())

	ch := make(chan bootUpdate, bootChanBuffer)
	msg := bootLoadCmd(&config.Config{}, ch)()
	if snapshot(msg.(snapshotMsg)).dataMode != "live" {
		t.Error("bootLoadCmd did not produce a live snapshot")
	}

	n := 0
	for range ch { // ranges to completion only if the channel was closed
		n++
	}
	if n == 0 {
		t.Error("bootLoadCmd reported nothing to the opening screen")
	}
	if got := waitBoot(ch)(); got != nil {
		t.Errorf("waitBoot on a closed channel = %#v, want nil", got)
	}
}

// TestBootScreenRendersAtEverySize sweeps the sizes the screen has to survive:
// a wide terminal, the classic 80×24, and one too small for the wordmark.
func TestBootScreenRendersAtEverySize(t *testing.T) {
	sizes := [][2]int{{190, 50}, {120, 40}, {80, 24}, {60, 16}, {40, 10}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		m := bootAt(w, h)
		m.boot.apply(bootUpdate{id: bootRepos, detail: "scanning 3 roots…"})
		out := m.View()
		if strings.TrimSpace(out) == "" {
			t.Fatalf("%dx%d: empty render", w, h)
		}
		if got := strings.Count(out, "\n") + 1; got > h {
			t.Errorf("%dx%d: render is %d lines, exceeds height", w, h, got)
		}
		for i, ln := range strings.Split(out, "\n") {
			if got := dispWidth(ln); got > w {
				t.Errorf("%dx%d: line %d is %d columns wide", w, h, i, got)
			}
		}
	}
}

// TestBootScreenScrollsToTheActiveStep checks the shrink behaviour: a terminal
// that cannot hold the whole sequence must still be showing the step being
// worked on, which is the only line that says work is happening.
func TestBootScreenScrollsToTheActiveStep(t *testing.T) {
	m := bootAt(80, 18)
	for _, s := range bootSequence {
		m.boot.apply(bootUpdate{id: s.id, detail: "working…"})
		if out := m.View(); !strings.Contains(out, s.label) {
			t.Errorf("active step %q is off screen at 80x18", s.label)
		}
		m.boot.apply(bootUpdate{id: s.id, detail: "done", complete: true})
	}
}

// TestBootFinishSettlesEveryStep checks that landing the snapshot leaves no
// spinner turning, and that a step whose report never arrived says so rather
// than showing a tick beside a blank.
func TestBootFinishSettlesEveryStep(t *testing.T) {
	b := newBootState()
	b.apply(bootUpdate{id: bootRepos, detail: "57 repos", complete: true})
	if cmd := b.finish(); cmd == nil {
		t.Fatal("finish returned no hand-over tick")
	}
	if !b.ready {
		t.Error("finish did not mark the screen ready")
	}
	for _, s := range b.steps {
		if s.state != bootDone {
			t.Errorf("step %q left at state %d after finish", s.id, s.state)
		}
		if s.detail == "" {
			t.Errorf("step %q finished with no figure beside it", s.id)
		}
	}
	if got := b.finish(); got != nil {
		t.Error("finish scheduled a second hand-over")
	}
}

// TestBootProgressAndLinger covers the two derived figures the screen shows.
func TestBootProgressAndLinger(t *testing.T) {
	b := newBootState()
	b.apply(bootUpdate{id: bootRepos, detail: "57 repos", complete: true})
	b.apply(bootUpdate{id: bootForge, detail: "github cli", complete: true})
	done, total := b.progress()
	if done != 2 || total != len(bootSequence) {
		t.Errorf("progress = %d/%d, want 2/%d", done, total, len(bootSequence))
	}

	// A load that finished instantly still holds the screen up to bootMinShow;
	// one that took longer than that gets the plain READY beat and no more.
	if got := b.linger(); got > bootMinShow || got < bootReadyLinger {
		t.Errorf("linger on a fast load = %v, want between %v and %v", got, bootReadyLinger, bootMinShow)
	}
	b.started = time.Now().Add(-10 * time.Second)
	if got := b.linger(); got != bootReadyLinger {
		t.Errorf("linger on a slow load = %v, want %v", got, bootReadyLinger)
	}
}

// TestBootAnyKeySkips checks the offer the screen makes. Skipping leaves the
// screen, never the load — the snapshot still lands on the cockpit behind it.
func TestBootAnyKeySkips(t *testing.T) {
	m := bootAt(120, 40)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if next.(model).boot != nil {
		t.Fatal("a key press did not retire the opening screen")
	}
	if !next.(model).loading {
		t.Error("skipping the screen must not claim the load is finished")
	}

	// Ctrl-C is the one key that still means quit rather than skip.
	m = bootAt(120, 40)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if next.(model).boot == nil {
		t.Error("ctrl+c retired the screen instead of quitting")
	}
	if cmd == nil {
		t.Fatal("ctrl+c on the opening screen did not quit")
	}
}

// TestBootProgressStopsWhenSkipped checks the goroutine hygiene: once the
// screen is gone nothing re-subscribes to the loader's channel.
func TestBootProgressStopsWhenSkipped(t *testing.T) {
	m := bootAt(120, 40)
	next, cmd := m.Update(bootProgressMsg{id: bootRepos, detail: "scanning…"})
	if cmd == nil {
		t.Fatal("a progress update did not re-subscribe while the screen was up")
	}
	if got := next.(model).boot.steps[3].state; got != bootRunning {
		t.Errorf("REPOSITORIES state = %d, want running", got)
	}

	m.boot = nil
	if _, cmd := m.Update(bootProgressMsg{id: bootRepos}); cmd != nil {
		t.Error("a progress update re-subscribed after the screen was retired")
	}
	if _, cmd := m.Update(bootTickMsg{}); cmd != nil {
		t.Error("the animation kept ticking after the screen was retired")
	}
}

// TestSnapshotRetiresTheOpeningScreen walks the hand-over: the snapshot lands,
// the sequence settles, and bootDoneMsg puts the cockpit on screen.
func TestSnapshotRetiresTheOpeningScreen(t *testing.T) {
	m := bootAt(120, 40)
	next, cmd := m.Update(snapshotMsg(snapshot{dataMode: "live"}))
	mm := next.(model)
	if mm.loading {
		t.Error("snapshotMsg should clear loading")
	}
	if mm.boot == nil || !mm.boot.ready {
		t.Fatal("snapshotMsg did not settle the opening screen")
	}
	if cmd == nil {
		t.Fatal("snapshotMsg did not schedule the hand-over")
	}
	if out := mm.View(); !strings.Contains(out, "PRESS ANY KEY") {
		t.Error("a settled opening screen does not offer the way past it")
	}

	next, _ = mm.Update(bootDoneMsg{})
	if next.(model).boot != nil {
		t.Fatal("bootDoneMsg did not retire the opening screen")
	}
	// The cockpit is what renders now — its lens bar is the proof.
	if out := next.(model).View(); !strings.Contains(out, "dispatch") {
		t.Error("the cockpit did not take over after the opening screen")
	}
}

// TestBootOnlyRunsForTheFirstLoad checks that the screen belongs to the first
// load alone: a plain model never carries one, and never renders one.
func TestBootOnlyRunsForTheFirstLoad(t *testing.T) {
	if newModel().boot != nil {
		t.Error("a plain model should not carry an opening screen")
	}
	m := newModel()
	m.width, m.height = 80, 24
	if out := m.View(); strings.Contains(out, "SUPERVISOR") {
		t.Error("the opening screen rendered without one being started")
	}
}
