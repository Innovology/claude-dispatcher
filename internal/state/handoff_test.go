package state

import (
	"testing"
	"time"
)

// A handoff ends the run it is made from with ReasonHandedOff, and that reason
// is what the hook's hold reads. The lineage on its own is not: a record that
// handed off, was taken back up by the human and then ended is an ordinary
// finished record that happens to have successors.
func TestHandedOffIsTheHandoffsOwnEnding(t *testing.T) {
	now := time.Now()
	d := &Dispatch{Status: StatusWorking}
	if d.HandedOff() {
		t.Fatal("a working record read as handed off")
	}
	d.HandedOffTo = []string{"next"}
	d.Stop(StatusExited, ReasonHandedOff+`"consume it" in app`, now)
	if !d.HandedOff() {
		t.Fatal("the handoff's own ending did not read as handed off")
	}
	if !d.Held() {
		t.Error("a handed-off run must hold its row like any ending nobody watched")
	}
	// The tracker taking it on to done when its own PR deploys keeps it
	// finished; the reason it writes is its own.
	d.Stop(StatusDone, "deployed — live", now.Add(time.Hour))
	if d.HandedOff() {
		t.Error("a deployed record still read as handed off")
	}
	d.Status, d.StatusReason = StatusWorking, ReasonHandedOff+"x"
	if d.HandedOff() {
		t.Error("a live record whose reason merely starts the same way read as handed off")
	}
	var none *Dispatch
	if none.HandedOff() {
		t.Error("a nil record read as handed off")
	}
}

// Both ends of the link survive a save, since the cockpit and `status` read them
// back from disk.
func TestTheHandoffLinkIsSaved(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	from := &Dispatch{ID: "from", Status: StatusExited, HandedOffTo: []string{"to", "to2"}}
	to := &Dispatch{ID: "to", Status: StatusWorking, HandedOffFrom: "from"}
	for _, d := range []*Dispatch{from, to} {
		if err := Save(d); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]*Dispatch{}
	for _, d := range LoadAll() {
		got[d.ID] = d
	}
	if l := got["from"].HandedOffTo; len(l) != 2 || l[0] != "to" || l[1] != "to2" {
		t.Errorf("HandedOffTo came back %v", l)
	}
	if got["to"].HandedOffFrom != "from" {
		t.Errorf("HandedOffFrom came back %q", got["to"].HandedOffFrom)
	}
}
