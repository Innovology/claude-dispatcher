package cockpit

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/state"
)

// A worker a conversation sent out says so where a dispatcher's setup is
// described, so a human reading the table can tell an orchestrator's workers
// from their own dispatches. A sender no longer on the store is said by id,
// and a dispatch nobody sent says nothing.
func TestAWorkerSaysWhichConversationSentIt(t *testing.T) {
	conv := &state.Dispatch{ID: "c1", Feature: "shop roadmap", Branchless: true}
	worker := &state.Dispatch{ID: "w1", Feature: "cart totals", DispatchedBy: "c1"}
	ctx := &collectCtx{records: []*state.Dispatch{conv, worker}}

	if got := ctx.senderOf(worker); got != "shop roadmap" {
		t.Errorf("sender = %q, want the conversation's name", got)
	}
	if got := ctx.senderOf(&state.Dispatch{DispatchedBy: "gone"}); got != "gone" {
		t.Errorf("a sender not on the store = %q, want its id", got)
	}
	if got := ctx.senderOf(conv); got != "" {
		t.Errorf("a dispatch nobody sent has sender %q", got)
	}
	if meta := fleetMeta(fleetRow{sender: "shop roadmap"}); !strings.Contains(meta, `sent by "shop roadmap"`) {
		t.Errorf("meta = %q", meta)
	}
}
