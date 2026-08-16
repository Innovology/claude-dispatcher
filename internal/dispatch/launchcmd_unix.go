//go:build !windows

package dispatch

import (
	"fmt"
	"strings"
)

// launchCommand builds the shell command that runs claude for a dispatch on
// Unix. When claude exits we drop to a login shell so the tmux session stays
// open for inspection instead of vanishing. CLAUDE_DISPATCHER_ID is the join
// key the lifecycle hook reads to attribute events back to the record, and
// mode is the permission mode the session opens in (see mode.go).
func launchCommand(dispatcherID, prompt string, mode Mode) string {
	return fmt.Sprintf("CLAUDE_DISPATCHER_ID=%s claude%s %s; exec ${SHELL:-/bin/sh}",
		dispatcherID, modeArgs(mode), shellQuote(prompt))
}

// resumeCommand is launchCommand for a session that already exists: claude
// picks the recorded conversation back up instead of starting a new one, and an
// empty prompt is left off entirely rather than passed as an empty argument,
// which claude would read as a first message with nothing in it. The mode is
// passed again because --permission-mode is a property of the new session, not
// of the transcript it reopens.
func resumeCommand(dispatcherID, sessionID, prompt string, mode Mode) string {
	arg := ""
	if prompt != "" {
		arg = " " + shellQuote(prompt)
	}
	return fmt.Sprintf("CLAUDE_DISPATCHER_ID=%s claude%s --resume %s%s; exec ${SHELL:-/bin/sh}",
		dispatcherID, modeArgs(mode), shellQuote(sessionID), arg)
}

// modeArgs is the permission-mode flag as a leading-space-prefixed fragment,
// or "" when this claude has no spelling for the mode.
func modeArgs(mode Mode) string {
	args := PermissionArgs(mode)
	if len(args) == 0 {
		return ""
	}
	return " " + strings.Join(args, " ")
}

// shellQuote single-quotes a string for POSIX shells, escaping embedded quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
