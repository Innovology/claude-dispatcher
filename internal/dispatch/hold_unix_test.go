//go:build !windows

package dispatch

import (
	"os"
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/supervisor"
)

// The machine's answer is Claude Code's own: the registry file for any claude
// under the pane, else a transcript one holds open. Any claude, not the first —
// a pane can hold more than one, and the first need not be the dispatcher's.
func TestASessionRunsAConversationAnyClaudeInItRuns(t *testing.T) {
	const want = "2a6c3883-09ae-482e-acbd-badf5c138369"
	withPane(t, 964367, 964400, 964500) // the pane's shell, a nested claude, the dispatcher's
	fakeProc(t, map[int][]string{
		964400: {"/dev/pts/6"},
		964500: {"/home/u/.claude/projects/-wt/" + want + ".jsonl"},
	})
	writeProcFile(t, 964400, "stat", statLine(964400, "700"))
	writeRegistry(t, filepath.Join(os.Getenv("HOME"), ".claude"), 964400, "some-other-conversation", "700")
	sess := supervisor.Session{Name: "disp-ds", Socket: "player-app"}

	if !sessionRunsConversation(sess, want) {
		t.Error("the conversation a claude in the pane holds open was not found")
	}
	if !sessionRunsConversation(sess, "some-other-conversation") {
		t.Error("the conversation the registry names was not found")
	}
	if sessionRunsConversation(sess, "not-here") || sessionRunsConversation(sess, "") {
		t.Error("a conversation no claude in the pane runs was found")
	}
}
