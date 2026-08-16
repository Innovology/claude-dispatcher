//go:build windows

package dispatch

import (
	"fmt"
	"strings"
)

// launchCommand builds the cmd.exe command that runs claude for a dispatch on
// Windows. CLAUDE_DISPATCHER_ID is the join key the lifecycle hook reads to
// attribute events back to the record, and mode is the permission mode the
// session opens in (see mode.go). The trailing ` & pause` keeps the detached
// console window open after claude exits so it stays available for inspection
// instead of vanishing (the Windows analogue of dropping to a login shell on
// Unix).
func launchCommand(dispatcherID, prompt string, mode Mode) string {
	return fmt.Sprintf(`set "CLAUDE_DISPATCHER_ID=%s" && claude%s %s & pause`,
		dispatcherID, modeArgs(mode), winQuote(prompt))
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
		arg = " " + winQuote(prompt)
	}
	return fmt.Sprintf(`set "CLAUDE_DISPATCHER_ID=%s" && claude%s --resume %s%s & pause`,
		dispatcherID, modeArgs(mode), winQuote(sessionID), arg)
}

// modeArgs is the permission-mode flag as a leading-space-prefixed fragment,
// or "" when this claude has no spelling for the mode. The flag's values are
// plain words, so they need no cmd.exe quoting.
func modeArgs(mode Mode) string {
	args := PermissionArgs(mode)
	if len(args) == 0 {
		return ""
	}
	return " " + strings.Join(args, " ")
}

// winQuote wraps a string in double quotes for cmd.exe, doubling any embedded
// double quotes. cmd.exe quoting is best-effort; newlines are collapsed to
// spaces since a cmd command line is single-line.
func winQuote(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
