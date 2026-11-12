package cockpit

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

func twoAccounts() *config.Config {
	return &config.Config{Accounts: map[string]string{"work": "~/.claude-work"}}
}

// `a` opens the picker on a row whose turn is over, with the cursor on an
// account it is not already on; with one account there is nothing to pick.
func TestSwapOpens(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	liveByID = map[string]*state.Dispatch{"s1": {ID: "s1", Feature: "pay", Account: ""}}
	r := fleetRow{id: "s1", feature: "pay", kind: "queue", swap: true}

	m := newModel()
	m.cfg = &config.Config{}
	mm, cmd := m.swapOpenFor(r)
	if mm.swapOpen || cmd != nil || !strings.Contains(mm.notice, "account add") {
		t.Fatalf("one account: open=%v notice=%q", mm.swapOpen, mm.notice)
	}

	m.cfg = twoAccounts()
	mm, cmd = m.swapOpenFor(r)
	if !mm.swapOpen || cmd == nil || mm.swapAt.current != "default" {
		t.Fatalf("open=%v target=%+v", mm.swapOpen, mm.swapAt)
	}
	if got := mm.accountNames()[mm.swapCursor]; got != "work" {
		t.Errorf("cursor on %q, want the account it is not on", got)
	}
	if v := mm.viewSwap(100, 20); !strings.Contains(v, "current") || !strings.Contains(v, "work") {
		t.Errorf("picker view:\n%s", v)
	}
}

// Enter on the account it is on moves nothing; enter on another fires the
// swap and closes; esc closes with nothing sent.
func TestUpdateSwap(t *testing.T) {
	m := newModel()
	m.cfg = twoAccounts()
	m.swapOpen, m.swapAt, m.swapCursor = true, &swapTarget{id: "s1", feature: "pay", current: "default"}, 0

	mm, cmd := m.updateSwap("enter")
	if cmd != nil || !mm.swapOpen || !strings.Contains(mm.notice, "already on default") {
		t.Fatalf("enter on the current account: open=%v notice=%q", mm.swapOpen, mm.notice)
	}
	mm, _ = mm.updateSwap("j")
	mm, cmd = mm.updateSwap("enter")
	if cmd == nil || mm.swapOpen || !strings.Contains(mm.notice, "to work") {
		t.Fatalf("enter on work: open=%v notice=%q", mm.swapOpen, mm.notice)
	}

	m.swapOpen, m.swapAt = true, &swapTarget{id: "s1", current: "default"}
	mm, cmd = m.updateSwap("esc")
	if cmd != nil || mm.swapOpen {
		t.Fatal("esc must close and send nothing")
	}
}

// The key reaches the picker from the table only on a row that offers it, and
// the footer names it only there.
func TestSwapKeyOnTheTable(t *testing.T) {
	m := newModel()
	m.cfg = twoAccounts()
	m.cqDispatch = false
	for _, tc := range []struct {
		swap bool
		want bool
	}{{true, true}, {false, false}} {
		saved := captureVars()
		fleet = []fleetRow{{id: "s1", feature: "pay", kind: "queue", ask: "turn-done", swap: tc.swap}}
		liveByID = map[string]*state.Dispatch{"s1": {ID: "s1", Feature: "pay"}}
		mm := m.fleetSync()
		if got := strings.Contains(mm.cqFooterHelp(), "a switch account"); got != tc.want {
			t.Errorf("swap=%v: footer offers it = %v", tc.swap, got)
		}
		mm, _, _ = mm.updateFloorQueue("a")
		if mm.swapOpen != tc.want {
			t.Errorf("swap=%v: picker open = %v", tc.swap, mm.swapOpen)
		}
		restoreVars(saved)
	}
}
