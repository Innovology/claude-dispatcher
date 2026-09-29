package cockpit

import (
	"strings"
	"testing"

	dispatchpkg "claude-dispatcher/internal/dispatch"
)

func withTidySeams(t *testing.T, plan []dispatchpkg.TidyItem, ran *[]dispatchpkg.TidyItem) {
	t.Helper()
	prevPlan, prevDo := planTidy, doTidy
	t.Cleanup(func() { planTidy, doTidy = prevPlan, prevDo })
	planTidy = func() []dispatchpkg.TidyItem { return plan }
	doTidy = func(items []dispatchpkg.TidyItem) []dispatchpkg.TidyItem {
		*ran = items
		return items
	}
}

func openPalette(m model, text string) model {
	mm, _ := m.handleKey(":")
	m = mm.(model)
	m.paletteText = text
	return m
}

// `: tidy` reads the plan in the background, puts it on the confirm bar, and
// only a y carries it out — the plan it confirmed, not a fresh guess.
func TestTidyReadsThenConfirmsThenRuns(t *testing.T) {
	plan := []dispatchpkg.TidyItem{
		{Path: "/w/a", Action: dispatchpkg.TidyRemove},
		{Path: "/w/b", Action: dispatchpkg.TidyRemove},
		{Path: "/w/c", Action: dispatchpkg.TidyStrip, NodeModules: []string{"node_modules"}},
		{Path: "/w/d", Action: dispatchpkg.TidyKeep, Reason: "still working"},
	}
	var ran []dispatchpkg.TidyItem
	withTidySeams(t, plan, &ran)

	m := openPalette(newModel(), "tidy")
	mm, cmd := m.handleKey("enter")
	m = mm.(model)
	if cmd == nil || !m.tidyReading || m.confirm != nil {
		t.Fatalf("tidy must start reading, not confirm: reading=%v confirm=%v", m.tidyReading, m.confirm)
	}
	// A second press while it reads is the same request, not another read.
	m2, again := m.startTidy()
	if again != nil || !m2.tidyReading {
		t.Fatal("a second tidy while reading must not start another read")
	}

	mm, _ = m.Update(cmd())
	m = mm.(model)
	if m.confirm == nil || m.confirm.kind != "tidy" {
		t.Fatalf("the plan must go on the confirm bar, got %+v", m.confirm)
	}
	for _, want := range []string{"remove 2 finished worktrees", "strip node_modules from 1", "1 kept"} {
		if !strings.Contains(m.confirm.label, want) {
			t.Errorf("confirm %q does not say %q", m.confirm.label, want)
		}
	}
	if ran != nil {
		t.Fatal("nothing may be removed before the confirm")
	}

	mm, cmd = m.handleKey("y")
	m = mm.(model)
	if cmd == nil {
		t.Fatal("y must run the tidy")
	}
	msg := cmd()
	if len(ran) != len(plan) {
		t.Fatalf("ran %d items, want the %d confirmed", len(ran), len(plan))
	}
	am, ok := msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "removed 2 worktrees") || !strings.Contains(am.notice, "from 1") {
		t.Fatalf("notice = %+v", msg)
	}
}

// n on the bar leaves every folder where it is.
func TestTidyCancelledTouchesNothing(t *testing.T) {
	var ran []dispatchpkg.TidyItem
	withTidySeams(t, []dispatchpkg.TidyItem{{Path: "/w/a", Action: dispatchpkg.TidyRemove}}, &ran)
	m, cmd := newModel().startTidy()
	mm, _ := m.Update(cmd())
	mm, _ = mm.(model).handleKey("n")
	if mm.(model).confirm != nil || ran != nil {
		t.Fatal("a cancelled tidy must not run")
	}
}

// With nothing to take, there is nothing to confirm — and what was kept is
// counted rather than silently dropped.
func TestTidyWithNothingToTakeSaysSo(t *testing.T) {
	var ran []dispatchpkg.TidyItem
	withTidySeams(t, []dispatchpkg.TidyItem{{Path: "/w/a", Action: dispatchpkg.TidyKeep, Reason: "parked"}}, &ran)
	m, cmd := newModel().startTidy()
	mm, _ := m.Update(cmd())
	m = mm.(model)
	if m.confirm != nil || m.tidyReading {
		t.Fatal("nothing to take must not ask for a confirm")
	}
	if !strings.Contains(m.notice, "nothing to tidy") || !strings.Contains(m.notice, "1 worktree kept") {
		t.Fatalf("notice = %q", m.notice)
	}
}
