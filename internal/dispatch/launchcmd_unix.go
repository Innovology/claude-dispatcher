//go:build !windows

package dispatch

import (
	"fmt"
	"strings"
)

// launchCommand builds the shell command that runs claude for a dispatch on
// Unix. When claude exits we drop to a login shell so the tmux session stays
// open for inspection instead of vanishing. CLAUDE_DISPATCHER_ID is the join
// key the lifecycle hook reads to attribute events back to the record.
func launchCommand(dispatcherID, prompt string) string {
	return fmt.Sprintf("CLAUDE_DISPATCHER_ID=%s claude %s; exec ${SHELL:-/bin/sh}",
		dispatcherID, shellQuote(prompt))
}

// resumeCommand is launchCommand for a session that already exists: claude
// picks the recorded conversation back up instead of starting a new one, and an
// empty prompt is left off entirely rather than passed as an empty argument,
// which claude would read as a first message with nothing in it.
func resumeCommand(dispatcherID, sessionID, prompt string) string {
	arg := ""
	if prompt != "" {
		arg = " " + shellQuote(prompt)
	}
	return fmt.Sprintf("CLAUDE_DISPATCHER_ID=%s claude --resume %s%s; exec ${SHELL:-/bin/sh}",
		dispatcherID, shellQuote(sessionID), arg)
}

// shellQuote single-quotes a string for POSIX shells, escaping embedded quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
