package cockpit

import (
	"testing"

	"claude-dispatcher/internal/config"
)

// dxRestModel is the triage model with three repos on offer, so there is a
// second row for the arrows to reach.
func dxRestModel(t *testing.T) model {
	t.Helper()
	m := cqModel(t)
	m.cfg = &config.Config{Roots: []string{seedRepoRoot(t, "alpha-api", "beta-svc", "player-app")}}
	return m
}

// The report: pick player-app with the arrows, press enter, type a title that
// starts with "d" — and the form jumped back to WHERE with the pick gone.
// Nothing had been typed yet, so the form counted as untouched and "d"
// re-opened it. Once a repo is chosen, every key that leaves a form at rest is
// a letter, whatever the title starts with.
func TestCQFormKeepsAPickedRepoWhenTheTitleStartsWithANavKey(t *testing.T) {
	for _, title := range []string{"dispatch-test", "hotfix", "2fa-login", ":colon"} {
		t.Run(title, func(t *testing.T) {
			m := dxRestModel(t)
			m = press(m, "d")
			lens := m.lens
			m = press(m, "down")
			picked := m.dxRepo
			if picked == 0 {
				t.Fatal("precondition: the arrow did not move off the first repo")
			}
			m = press(m, "enter")
			if m.dxField != dxTitleF {
				t.Fatalf("enter should move to TITLE, got field %d", m.dxField)
			}
			for _, r := range title {
				m = press(m, string(r))
			}
			if !m.cqDispatch || m.lens != lens {
				t.Fatalf("the form was left: open=%v lens=%q, was %q", m.cqDispatch, m.lens, lens)
			}
			if m.dxTitle != title {
				t.Errorf("title = %q, want %q", m.dxTitle, title)
			}
			if m.dxRepo != picked || m.dxField != dxTitleF {
				t.Errorf("the pick moved: repo %d field %d, want repo %d on TITLE", m.dxRepo, m.dxField, picked)
			}
		})
	}
}

// Moving along WHERE is choosing too: after an arrow, "d" is a filter letter,
// not a reset back to the first row.
func TestCQFormArrowOnWhereIsAChoice(t *testing.T) {
	m := dxRestModel(t)
	m = press(m, "d")
	m = press(m, "down")
	if m.dxRepo == 0 {
		t.Fatal("precondition: the arrow did not move off the first repo")
	}
	m = press(m, "d")
	if m.dxFilter != "d" || !m.cqDispatch {
		t.Errorf("d after an arrow should filter: filter %q open=%v", m.dxFilter, m.cqDispatch)
	}
}
