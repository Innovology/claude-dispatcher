package cockpit

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/state"
)

func TestCqActsOffersReplyExceptOnAPermission(t *testing.T) {
	rec := &state.Dispatch{Feature: "f"}
	has := func(acts []cqAct) bool {
		for _, a := range acts {
			if a.k == "r" {
				return true
			}
		}
		return false
	}
	for _, kind := range []string{"turn-done", "idle", "review", "api-error", "needs"} {
		if !has(cqActs(rec, kind)) {
			t.Errorf("%s: a waiting row must offer r reply", kind)
		}
	}
	if has(cqActs(rec, "permission")) {
		t.Error("a permission prompt is a menu; typing at it picks options")
	}
	if has(cqActs(rec, "running")) {
		t.Error("a running dispatcher has asked nothing")
	}
}

func TestReplyable(t *testing.T) {
	if !replyable(fleetRow{kind: "queue", ask: "turn-done"}) {
		t.Error("a finished turn is answerable")
	}
	for _, r := range []fleetRow{
		{kind: "queue", ask: "permission"},
		{kind: "run"},
		{kind: "done"},
		{kind: "parked"},
	} {
		if replyable(r) {
			t.Errorf("%+v must not open a reply", r)
		}
	}
}

// The input owns the keyboard, sends nothing on an empty enter, and hands the
// typed line to replyCmd for the row it was opened on.
func TestUpdateReply(t *testing.T) {
	m := newModel()
	m.replyOpen, m.replyAt = true, &replyTarget{id: "a1", feature: "f", ask: "Want me to merge it?"}

	mm, cmd := m.updateReply("enter")
	if cmd != nil || !mm.replyOpen || mm.notice == "" {
		t.Fatalf("empty enter must send nothing and say why: open=%v notice=%q", mm.replyOpen, mm.notice)
	}
	for _, k := range []string{"m", "e", "r", "g", "e", " ", "i", "t"} {
		mm, _ = mm.updateReply(k)
	}
	if mm.replyText != "merge it" {
		t.Fatalf("typed %q", mm.replyText)
	}
	mm, cmd = mm.updateReply("enter")
	if cmd == nil || mm.replyOpen || mm.replyAt != nil {
		t.Fatalf("enter must close the input and send: open=%v", mm.replyOpen)
	}
	// With no record for the id, the command reports rather than typing.
	saved := captureVars()
	defer restoreVars(saved)
	liveByID = map[string]*state.Dispatch{}
	if am, ok := cmd().(actionMsg); !ok || am.notice == "" {
		t.Fatalf("got %#v", cmd())
	}

	m.replyOpen, m.replyText = true, "half typed"
	mm, cmd = m.updateReply("esc")
	if cmd != nil || mm.replyOpen || mm.replyText != "" {
		t.Fatal("esc must cancel and forget the line")
	}
	var _ tea.Cmd = cmd
}
