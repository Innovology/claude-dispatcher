package dispatch

import (
	"os"
	"strings"
	"testing"

	"claude-dispatcher/internal/state"
)

// A worker sent out from inside a conversation must not start as that
// conversation's child: every variable claude puts in its tools' environment
// goes, and the store it was sent from stays.
func TestScrubSessionEnvLeavesNoTraceOfTheSessionItRunsIn(t *testing.T) {
	for _, k := range []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_MESSAGING_SOCKET",
		"CLAUDE_PID", "CLAUDE_DISPATCHER_ID", "CLAUDE_CONFIG_DIR", "AI_AGENT", "CLAUDE_EFFORT"} {
		t.Setenv(k, "x")
	}
	t.Setenv("CLAUDE_DISPATCHER_STATE", "/store")
	t.Setenv("PATH_KEEP_ME", "y")
	ScrubSessionEnv()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if sessionVar(name) {
			t.Errorf("%s survived", name)
		}
	}
	if os.Getenv("CLAUDE_DISPATCHER_STATE") != "/store" || os.Getenv("PATH_KEEP_ME") != "y" {
		t.Error("scrubbed a variable that is not the session's")
	}
}

func TestMarkDispatchedByRecordsTheParent(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	w := &state.Dispatch{ID: state.NewID(), Feature: "w"}
	_ = state.Save(w)
	if err := MarkDispatchedBy(w.ID, "parent1"); err != nil {
		t.Fatal(err)
	}
	for _, d := range state.LoadAll() {
		if d.ID == w.ID && d.DispatchedBy != "parent1" {
			t.Errorf("dispatched by %q", d.DispatchedBy)
		}
	}
	if MarkDispatchedBy("nope", "p") == nil {
		t.Error("a record that is not there was marked")
	}
}
