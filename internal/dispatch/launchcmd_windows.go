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
//
// The prompt arrives as a PATH and is read inside the session, for the reason
// the Unix build gives — and here the old inline form was failing two ways at
// once. A cmd.exe command line is capped at 8191 characters, so a long prompt
// took the launch down; and cmd.exe has no multi-line command, so winQuote was
// flattening every newline in the prompt to a space before it ever got there.
// A prompt is not a single line, and a dispatcher was reading a paragraph that
// had been run together.
//
// cmd.exe has no command substitution, so the reading is done by PowerShell,
// which ships with every supported Windows. Everything between the -Command
// quotes is one cmd argument: the `&` inside it is PowerShell's call operator,
// not a cmd separator, and the trailing `& pause` stays outside so the window
// still waits when claude exits.
func launchCommand(dispatcherID, promptPath string, mode Mode, model Model) string {
	return fmt.Sprintf(`set "CLAUDE_DISPATCHER_ID=%s" && %s & pause`,
		dispatcherID, psRun(fmt.Sprintf("claude%s%s %s",
			modeArgs(mode), modelArgs(model), psReadFile(promptPath))))
}

// resumeCommand is launchCommand for a session that already exists: claude
// picks the recorded conversation back up instead of starting a new one, and an
// empty prompt is left off entirely rather than passed as an empty argument,
// which claude would read as a first message with nothing in it. The mode and
// the model are passed again because both are properties of the new session,
// not of the transcript it reopens.
// StewardCommand is the steward session's command — see the Unix build.
func StewardCommand(opening string) string {
	return psRun(fmt.Sprintf("claude%s %s", modeArgs(ModeAuto), psQuote(opening))) + " & pause"
}

func resumeCommand(dispatcherID, sessionID, promptPath string, mode Mode, model Model) string {
	arg := ""
	if promptPath != "" {
		arg = " " + psReadFile(promptPath)
	}
	return fmt.Sprintf(`set "CLAUDE_DISPATCHER_ID=%s" && %s & pause`,
		dispatcherID, psRun(fmt.Sprintf("claude%s%s --resume %s%s",
			modeArgs(mode), modelArgs(model), psQuote(sessionID), arg)))
}

// psRun wraps a PowerShell statement in the invocation that runs it. -NoProfile
// so a human's own profile cannot change what a dispatcher runs as, and the
// leading `&` is the call operator, which is what lets the command be built
// from expressions rather than a literal line.
func psRun(statement string) string {
	return `powershell -NoProfile -ExecutionPolicy Bypass -Command "& ` + statement + `"`
}

// psReadFile is the PowerShell expression for a file's whole contents as ONE
// argument. ReadAllText rather than Get-Content, which would split the prompt
// into a line array and pass each line as its own argument.
func psReadFile(path string) string {
	return "([IO.File]::ReadAllText(" + psQuote(path) + "))"
}

// psQuote single-quotes a string for PowerShell, doubling embedded quotes. It
// must never emit a double quote: the whole statement already lives inside the
// double-quoted -Command argument that cmd.exe parses.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// modeArgs is the permission-mode flag as a leading-space-prefixed fragment,
// or "" when this claude has no spelling for the mode. The flag's values are
// plain words, so they need no quoting.
func modeArgs(mode Mode) string {
	args := PermissionArgs(mode)
	if len(args) == 0 {
		return ""
	}
	return " " + strings.Join(args, " ")
}

// modelArgs is the model flag the same way, or "" for the default and for an
// alias this claude does not advertise.
func modelArgs(model Model) string {
	args := ModelArgs(model)
	if len(args) == 0 {
		return ""
	}
	return " " + strings.Join(args, " ")
}
