package cockpit

// load_test.go covers the loader's queue: one load at a time, requests
// collapsed while one is out, and a late snapshot dropped rather than
// published. See load.go — this is the machinery behind the disappearing
// dispatch.

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
)

// loaded is a model with real-data plumbing and nothing in flight.
func loaded() model {
	m := newModel()
	m.cfg = &config.Config{}
	return m
}

// The pile-up itself: an fsnotify event fires on every record write, and every
// hook event from every session is one. Each used to start a full portfolio
// load of its own — seconds of work, several deep — and the last one to return
// won the table whatever it knew.
func TestLoadsCollapseWhileOneIsOut(t *testing.T) {
	m := loaded()

	m, first := m.requestLoad(loadPlain)
	if first == nil {
		t.Fatal("the first request did not start a load")
	}
	if !m.loadBusy || m.loadSeq != 1 {
		t.Fatalf("loadBusy=%v seq=%d after the first request", m.loadBusy, m.loadSeq)
	}

	for i := 0; i < 5; i++ {
		var cmd tea.Cmd
		m, cmd = m.requestLoad(loadPlain)
		if cmd != nil {
			t.Fatalf("request %d started a second load beside the one in flight", i)
		}
	}
	if m.loadSeq != 1 {
		t.Errorf("loadSeq = %d, want the five collapsed into one", m.loadSeq)
	}
	if m.loadNext != loadPlain {
		t.Errorf("loadNext = %v, want the queued reload", m.loadNext)
	}

	// The snapshot lands: exactly one more load starts, for all five.
	mm, fresh, queued := m.loadLanded(snapshot{seq: 1})
	if !fresh {
		t.Error("the load that was actually out was treated as late")
	}
	if queued == nil {
		t.Fatal("what was asked for during the load was forgotten")
	}
	if mm.loadSeq != 2 || mm.loadNext != loadNone {
		t.Errorf("after landing: seq=%d next=%v", mm.loadSeq, mm.loadNext)
	}
}

// A recheck is not a reload: it drops the forge cache and sweeps session
// liveness first. One asked for while a plain reload is out must survive the
// wait as a recheck.
func TestAQueuedRecheckOutranksAQueuedReload(t *testing.T) {
	m := loaded()
	m, _ = m.requestLoad(loadPlain)
	m, _ = m.requestLoad(loadPlain)
	m, _ = m.requestLoad(loadRecheck)
	m, _ = m.requestLoad(loadPlain)
	if m.loadNext != loadRecheck {
		t.Errorf("loadNext = %v, want the recheck", m.loadNext)
	}
}

// The other half of the fix. Coalescing makes overlap rare rather than
// impossible (see loadStuckAfter), so a snapshot older than the one on screen
// is dropped: publishing it would put the earlier fleet back, which is the
// disappearance seen from the other end.
func TestALateSnapshotIsNotPublishedOverANewerOne(t *testing.T) {
	m := loaded()
	m, _, _ = m.loadLanded(snapshot{seq: 5})
	if m.loadApplied != 5 {
		t.Fatalf("loadApplied = %d", m.loadApplied)
	}
	_, fresh, _ := m.loadLanded(snapshot{seq: 4})
	if fresh {
		t.Error("a snapshot from before the one on screen was published over it")
	}
	_, fresh, _ = m.loadLanded(snapshot{seq: 6})
	if !fresh {
		t.Error("the next load was refused")
	}
}

// A snapshot with no number at all is a test driving Update directly, or any
// caller outside the queue: the guard is about racing loads, not about refusing
// data.
func TestAnUnnumberedSnapshotAlwaysPublishes(t *testing.T) {
	m := loaded()
	if _, fresh, _ := m.loadLanded(snapshot{}); !fresh {
		t.Error("an unnumbered snapshot was dropped")
	}
}

// One-at-a-time is a rule about a load that is going to land. A load that never
// does must not freeze the cockpit's data for the rest of the session.
func TestALoadThatNeverLandsStopsBlockingAfterAWhile(t *testing.T) {
	m := loaded()
	m, _ = m.requestLoad(loadPlain)
	m.loadStarted = time.Now().Add(-loadStuckAfter - time.Minute)

	m, cmd := m.requestLoad(loadPlain)
	if cmd == nil {
		t.Fatal("a load out for longer than any real one still blocks every other")
	}
	if m.loadSeq != 2 {
		t.Errorf("loadSeq = %d, want a new number for the load that went round it", m.loadSeq)
	}
}

// Nothing to load: a config-less cockpit is on demo seed data.
func TestNoConfigStartsNoLoad(t *testing.T) {
	m := newModel()
	if _, cmd := m.requestLoad(loadRecheck); cmd != nil {
		t.Error("a cockpit with no config started a load")
	}
}
