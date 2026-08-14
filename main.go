// Claude Dispatcher — a terminal-native dispatch cockpit for running many
// Claude Code sessions across many independent repositories.
//
// Vocabulary: a unit of execution is a "dispatcher"; sending it work is a
// "dispatch"; the human unit of work is a "feature". Never "agent", "bot",
// "runner", or "worker".
package main

import (
	"fmt"
	"os"

	"claude-dispatcher/internal/cockpit"
	"claude-dispatcher/internal/hookcmd"
	"claude-dispatcher/internal/initcmd"
	"claude-dispatcher/internal/version"
)

const usage = `claude-dispatcher — dispatch cockpit for Claude Code sessions

Usage:
  claude-dispatcher            open the cockpit (six lenses)
  claude-dispatcher init       write config, discover repos, install the status hook
  claude-dispatcher hook <ev>  (internal) invoked by Claude Code lifecycle hooks
  claude-dispatcher version    print the version
  claude-dispatcher help       show this help
`

func main() {
	args := os.Args[1:]
	// Platform-specific hidden subcommands (e.g. Windows `win-focus`). A no-op
	// returning false on non-Windows, so this stays cross-platform.
	if handled, code := maybeWindowsSubcommand(args); handled {
		os.Exit(code)
	}
	if len(args) == 0 {
		runCockpit()
		return
	}
	switch args[0] {
	case "v2":
		// Retained as a hidden alias for muscle memory — the cockpit is now the
		// default, so bare `claude-dispatcher` opens it.
		runCockpit()
	case "init":
		if err := initcmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "init:", err)
			os.Exit(1)
		}
	case "hook":
		// Never fail loudly: a hook error must not disturb the Claude session.
		os.Exit(hookcmd.Run(args[1:]))
	case "version", "--version", "-v":
		fmt.Println("claude-dispatcher", version.Display())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		os.Exit(2)
	}
}

func runCockpit() {
	if err := cockpit.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "claude-dispatcher:", err)
		os.Exit(1)
	}
}
