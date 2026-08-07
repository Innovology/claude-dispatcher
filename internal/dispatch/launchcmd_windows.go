//go:build windows

package dispatch

import (
	"fmt"
	"strings"
)

// launchCommand builds the cmd.exe command that runs claude for a dispatch on
// Windows. CLAUDE_DISPATCHER_ID is the join key the lifecycle hook reads to
// attribute events back to the record. The trailing ` & pause` keeps the
// detached console window open after claude exits so it stays available for
// inspection instead of vanishing (the Windows analogue of dropping to a
// login shell on Unix).
func launchCommand(dispatcherID, prompt string) string {
	return fmt.Sprintf(`set "CLAUDE_DISPATCHER_ID=%s" && claude %s & pause`,
		dispatcherID, winQuote(prompt))
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
