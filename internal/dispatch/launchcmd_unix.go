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

// shellQuote single-quotes a string for POSIX shells, escaping embedded quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
