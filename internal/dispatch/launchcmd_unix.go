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
//
// configDir is the account's CLAUDE_CONFIG_DIR, carried inline for the same
// reason the id is: tmux starts a session with its server's environment, not
// ours. "" — the default account — sets nothing at all (see account.Env).
func launchCommand(dispatcherID, promptPath, shell string, mode Mode, model Model, configDir string) string {
	return fmt.Sprintf("%sCLAUDE_DISPATCHER_ID=%s claude%s%s %s%s",
		configDirEnv(configDir), dispatcherID, modeArgs(mode), modelArgs(model),
		readFileArg(promptPath), exitShell(shell))
}

// StewardCommand is the command the steward session runs: claude in auto mode,
// opening on a short first message — its standing brief is the CLAUDE.md in
// its own folder, which claude loads itself and which survives compaction and
// restarts where a first message would not. No CLAUDE_DISPATCHER_ID: the
// steward is not a dispatcher, and its hooks must not be attributed to one.
//
// stateDir, when set, is carried inline the way CLAUDE_DISPATCHER_ID is for a
// dispatch: tmux starts a session with its server's environment, not the
// caller's, so a store chosen with CLAUDE_DISPATCHER_STATE would otherwise be
// lost on the way in and the steward would read — and reply into — the default
// one.
//
// It ends in the same exit shell a dispatch does, naming none of its own: the
// steward's pane is a pane like any other, and the human who jumps into it
// should land where they land everywhere else.
func StewardCommand(stateDir, opening string) string {
	env := ""
	if stateDir != "" {
		env = "CLAUDE_DISPATCHER_STATE=" + shellQuote(stateDir) + " "
	}
	return fmt.Sprintf("%sclaude%s %s%s", env, modeArgs(ModeAuto), shellQuote(opening), exitShell(""))
}

// exitShell is the tail of the launch line: what the pane becomes once claude
// has gone. The session stays open for inspection either way, and this decides
// which shell is sitting in it.
//
// Four answers, first one that works. The configured shell (config.toml's
// `shell`) is the human saying it outright. Failing that the pane asks its OWN
// tmux server for `default-shell`, which is the setting their every other pane
// already obeys — asked here, inside the pane, rather than resolved when the
// line was built, because at that point the server did not exist yet and
// because a tmux.conf may compute the value in a `run-shell` job (this
// machine's does), leaving nothing in the file to parse. Then $SHELL, then
// /bin/sh, which is the one shell a POSIX machine is required to have.
//
// `-x` rather than `-n` on the last check, so a shell named in config that is
// not there, and a tmux that answered with something unrunnable, both fall
// through instead of killing the pane the moment claude exits. Nothing here
// touches the line claude is launched with: that is POSIX and runs under
// /bin/sh whatever the human's shell is. Full record: docs/adr/0025.
func exitShell(shell string) string {
	return "; cdsh=" + shellQuote(strings.TrimSpace(shell)) +
		`; [ -n "$cdsh" ] || cdsh=$(tmux display -p '#{default-shell}' 2>/dev/null)` +
		`; [ -x "$cdsh" ] || cdsh=${SHELL:-/bin/sh}; exec "$cdsh"`
}

// resumeCommand is launchCommand for a session that already exists: claude
// picks the recorded conversation back up instead of starting a new one, and an
// empty prompt is left off entirely rather than passed as an empty argument,
// which claude would read as a first message with nothing in it. The mode and
// the model are passed again because both are properties of the new session,
// not of the transcript it reopens. The account is passed again for a harder
// reason: the transcript lives in that account's config directory, and claude
// started under any other cannot find the conversation at all.
func resumeCommand(dispatcherID, sessionID, promptPath, shell string, mode Mode, model Model, configDir string) string {
	arg := ""
	if promptPath != "" {
		arg = " " + readFileArg(promptPath)
	}
	return fmt.Sprintf("%sCLAUDE_DISPATCHER_ID=%s claude%s%s --resume %s%s%s",
		configDirEnv(configDir), dispatcherID, modeArgs(mode), modelArgs(model),
		shellQuote(sessionID), arg, exitShell(shell))
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

// configDirEnv is the account's CLAUDE_CONFIG_DIR as a leading assignment, or
// "" for the default account.
func configDirEnv(configDir string) string {
	if configDir == "" {
		return ""
	}
	return "CLAUDE_CONFIG_DIR=" + shellQuote(configDir) + " "
}
