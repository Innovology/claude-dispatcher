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

	"claude-dispatcher/internal/hookcmd"
	"claude-dispatcher/internal/initcmd"
	"claude-dispatcher/internal/ui"
)

const usage = `claude-dispatcher — dispatch cockpit for Claude Code sessions

Usage:
  claude-dispatcher            open the cockpit
  claude-dispatcher init       write config, discover repos, install the status hook
  claude-dispatcher hook <ev>  (internal) invoked by Claude Code lifecycle hooks
  claude-dispatcher help       show this help
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		if err := ui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "claude-dispatcher:", err)
			os.Exit(1)
		}
		return
	}
	switch args[0] {
	case "init":
		if err := initcmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "init:", err)
			os.Exit(1)
		}
	case "hook":
		// Never fail loudly: a hook error must not disturb the Claude session.
		os.Exit(hookcmd.Run(args[1:]))
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		os.Exit(2)
	}
}
