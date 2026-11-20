package dispatch

// send.go is the orchestrator half of a conversation turning into work (see
// work.go): what `claude-dispatcher dispatch` needs to send a worker out from
// inside a session and leave the cockpit knowing who sent it.

import (
	"fmt"
	"os"
	"strings"

	"claude-dispatcher/internal/state"
)

// MarkDispatchedBy records on the worker id that parent sent it, under the
// hook lock, for the reason markSessionStarted reloads: the worker's own hooks
// may already have written its record.
func MarkDispatchedBy(id, parent string) error {
	if id == "" || parent == "" {
		return nil
	}
	release := state.Lock()
	defer release()
	for _, d := range state.LoadAll() {
		if d.ID == id {
			d.DispatchedBy = parent
			return state.Save(d)
		}
	}
	return fmt.Errorf("no record %s", id)
}

// ScrubSessionEnv removes from this process the variables a Claude Code
// session puts in its tools' environment, before this process starts another
// session. A pane takes its environment from the client that asked for it (ADR
// 0014), and a worker sent out from inside a conversation would otherwise start
// as that conversation's child — its session id, its messaging socket, its
// dispatcher id, its account (CLAUDE_CONFIG_DIR, which the launch line sets
// only when the account is not the default). CLAUDE_DISPATCHER_STATE is kept:
// it names the store, which is the same one.
func ScrubSessionEnv() {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if sessionVar(name) {
			_ = os.Unsetenv(name)
		}
	}
}

func sessionVar(name string) bool {
	switch name {
	case "CLAUDECODE", "CLAUDE_PID", "CLAUDE_EFFORT", "CLAUDE_DISPATCHER_ID", "CLAUDE_CONFIG_DIR", "AI_AGENT":
		return true
	}
	return strings.HasPrefix(name, "CLAUDE_CODE_")
}
