package cockpit

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

// With more than one subscription configured, every row says which one it
// spends — the default by name too — and the row keeps its width. With one,
// there is nothing to tell apart and no column.
func TestFleetShowsEachDispatchersAccount(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	fleet = []fleetRow{
		{id: "a", feature: "pay retry", kind: "run", account: "work"},
		{id: "b", feature: "login page", kind: "run", account: ""},
	}
	m := newModel()

	m.cfg = &config.Config{}
	if v := m.viewFleet(150, 30); strings.Contains(v, "ACCOUNT") {
		t.Error("one account, and the table still has an ACCOUNT column")
	}

	m.cfg = &config.Config{Accounts: map[string]string{"work": "~/.claude-work"}}
	v := m.viewFleet(150, 30)
	if !strings.Contains(v, "ACCOUNT") {
		t.Fatalf("no ACCOUNT column:\n%s", v)
	}
	for _, want := range []string{"pay retry", "work", "login page", "default"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	for _, w := range []int{80, 110, 150, 200} {
		cols := fleetColumns(w, fleetProductMin).withAccount(fleetAccountWidth(fleet))
		if line := fleetLine(w, cols, cTransparent, [flCells]string{}, [flCells]string{}); dispWidth(line) != w {
			t.Errorf("@%d: a line with the account is %d wide", w, dispWidth(line))
		}
	}
}

// The column never squeezes the feature past legibility; the detail line
// still names the account there.
func TestFleetAccountGivesWayToTheFeature(t *testing.T) {
	cols := fleetColumns(60, fleetProductMin)
	if got := cols.withAccount(10); got.account != 0 || got.feature != cols.feature {
		t.Errorf("a narrow table gave the feature away: %+v", got)
	}
	if w := fleetAccountWidth([]fleetRow{{account: "a-very-long-account-name"}}); w != fleetAccountMax {
		t.Errorf("width %d, want the cap %d", w, fleetAccountMax)
	}
}
