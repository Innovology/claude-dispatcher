//go:build !windows

package dispatch

import (
	"fmt"
	"strings"
)

// launchCommand builds the shell command that runs claude for a dispatch on
// Unix. When claude exits we drop to a login shell so the tmux session stays
// open for inspection instead of vanishing. CLAUDE_DISPATCHER_ID is the join
// key the lifecycle hook reads to attribute events back to the record, mode is
// the permission mode the session opens in (see mode.go), and model is the
// model it runs (see model.go).
//
// The prompt arrives as a PATH, and the session's own shell reads it. It used
// to be interpolated into this string, which made the prompt part of the one
// argument the supervisor is handed — and a supervisor caps that. tmux carries
// a client command to its server in a single imsg, whose ceiling is 16KB
// (MAX_IMSGSIZE); measured against tmux 3.7b, a new-session command of 16313
// bytes went through and 16314 came back "command too long". So a prompt of any
// size a human might paste took the launch down, and every trace of it with it.
// Reading the file inside the session leaves this command a fixed ~120 bytes
// whatever the prompt is; the only ceiling left is the kernel's on one argv
// (see MaxPromptBytes).
func launchCommand(dispatcherID, promptPath string, mode Mode, model Model) string {
	return fmt.Sprintf("CLAUDE_DISPATCHER_ID=%s claude%s%s %s; exec ${SHELL:-/bin/sh}",
		dispatcherID, modeArgs(mode), modelArgs(model), readFileArg(promptPath))
}

// resumeCommand is launchCommand for a session that already exists: claude
// picks the recorded conversation back up instead of starting a new one, and an
// empty prompt is left off entirely rather than passed as an empty argument,
// which claude would read as a first message with nothing in it. The mode and
// the model are passed again because both are properties of the new session,
// not of the transcript it reopens.
func resumeCommand(dispatcherID, sessionID, promptPath string, mode Mode, model Model) string {
	arg := ""
	if promptPath != "" {
		arg = " " + readFileArg(promptPath)
	}
	return fmt.Sprintf("CLAUDE_DISPATCHER_ID=%s claude%s%s --resume %s%s; exec ${SHELL:-/bin/sh}",
		dispatcherID, modeArgs(mode), modelArgs(model), shellQuote(sessionID), arg)
}

// readFileArg is the shell fragment that expands to a file's contents as ONE
// argument. Double-quoted so the prompt's own whitespace, newlines and glob
// characters survive; `$(cat)` drops trailing newlines, which is why the
// prompt is trimmed before it is written rather than relying on this.
func readFileArg(path string) string {
	return `"$(cat ` + shellQuote(path) + `)"`
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

// modelArgs is the model flag the same way, or "" for the default and for an
// alias this claude does not advertise.
func modelArgs(model Model) string {
	args := ModelArgs(model)
	if len(args) == 0 {
		return ""
	}
	return " " + strings.Join(args, " ")
}

// shellQuote single-quotes a string for POSIX shells, escaping embedded quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
