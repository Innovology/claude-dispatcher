package cockpit

// loadstate_test.go covers the header's rebuild indicator.
//
// Loads are one at a time and requests coalesce, so a change the human just
// made is applied when the load already out comes back — seconds later, or a
// minute on a cold portfolio. Until this, nothing on screen said so: editing
// scan roots while a poll was in flight left the old repositories up with
// "settings saved" in the footer, which is indistinguishable from a rescan that
// never started.

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
)

func TestLoadStateSaysWhichStateItIsIn(t *testing.T) {
	cases := []struct {
		name string
		busy bool
		next loadKind
		want string
	}{
		{"idle says nothing", false, loadNone, ""},
		{"a load out", true, loadNone, "rescanning"},
		{"and one waiting on it", true, loadRecheck, "rescanning · another queued"},
		// Nothing is out, so whatever was queued has started or been dropped;
		// claiming a queue here would outlive the thing it describes.
		{"queued with none out", false, loadPlain, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := model{loadBusy: tc.busy, loadNext: tc.next}
			if got := m.loadState(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The indicator has to survive the trip through requestLoad, because that is
// the only thing that sets the flags it reads.
func TestRequestLoadIsVisibleInTheHeader(t *testing.T) {
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })

	m := newModel()
	m.cfg = &config.Config{Roots: []string{t.TempDir()}}
	m.width = 200

	if got := m.loadState(); got != "" {
		t.Fatalf("a fresh model is not loading, got %q", got)
	}
	m, cmd := m.requestLoad(loadPlain)
	if cmd == nil {
		t.Fatal("the first request should start a load")
	}
	if !strings.Contains(m.headerView(), "rescanning") {
		t.Errorf("a load in flight should show in the header:\n%s", m.headerView())
	}

	// A second request while that one is out is remembered, not started — and
	// the header says there are two things to wait for, not one.
	m2, cmd2 := m.requestLoad(loadPlain)
	if cmd2 != nil {
		t.Error("a second request should be queued, not started beside the first")
	}
	if got := m2.loadState(); got != "rescanning · another queued" {
		t.Errorf("got %q, want the queued form", got)
	}

	// When that snapshot lands the queued one starts, so the header drops to the
	// singular rather than going quiet — there is still something to wait for,
	// and saying "done" here is what would make the indicator a lie.
	m3, fresh, cmd3 := m2.loadLanded(snapshot{seq: m2.loadSeq})
	if !fresh {
		t.Error("the snapshot it was waiting for should be fresh")
	}
	if cmd3 == nil {
		t.Error("landing should start the load that was waiting on it")
	}
	if got := m3.loadState(); got != "rescanning" {
		t.Errorf("got %q, want the queued load now out on its own", got)
	}

	// Only with nothing left out does it go quiet.
	m4, _, _ := m3.loadLanded(snapshot{seq: m3.loadSeq})
	if got := m4.loadState(); got != "" {
		t.Errorf("with nothing in flight the header should say nothing, got %q", got)
	}
}

// The escape hatch for a wedged load must not make the indicator lie: it is
// still out, and the header still says so, even once another is allowed beside
// it.
func TestLoadStateSurvivesTheStuckWindow(t *testing.T) {
	m := model{loadBusy: true, loadStarted: time.Now().Add(-2 * loadStuckAfter)}
	if got := m.loadState(); got != "rescanning" {
		t.Errorf("got %q, want rescanning", got)
	}
}
