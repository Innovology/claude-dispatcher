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

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/accountcmd"
	"claude-dispatcher/internal/cockpit"
	"claude-dispatcher/internal/fleetcmd"
	"claude-dispatcher/internal/hookcmd"
	"claude-dispatcher/internal/initcmd"
	"claude-dispatcher/internal/steward"
	"claude-dispatcher/internal/version"
)

const usage = `claude-dispatcher — dispatch cockpit for Claude Code sessions

Usage:
  claude-dispatcher            open the cockpit (six lenses)
  claude-dispatcher init       write config, discover repos, install the status hook
  claude-dispatcher status     the live fleet and what each dispatcher wants (--json)
  claude-dispatcher reply <id|feature> <text>
                               type one line into a waiting dispatcher's session
  claude-dispatcher park <id|feature> <reason>
                               shelve a dispatcher · unpark <id|feature> takes it back
  claude-dispatcher note <id|feature> <text>
                               the steward's reading of a dispatcher's wait
  claude-dispatcher tidy       list the worktrees finished dispatchers left on disk
                               · tidy --yes removes them (: tidy in the cockpit)
  claude-dispatcher steward    every product steward and its state · steward new <product>
                               starts one, steward open <id> reopens an older one,
                               steward end <product|id> ends one (t in the cockpit)
  claude-dispatcher account    the Claude subscriptions a dispatch can run under, and what
                               each has left · account add <name> adds and logs one in
  claude-dispatcher ruling add|end|list
                               a product's standing rulings · a steward records one only
                               with your say-so, and merges only under one that allows it
  claude-dispatcher swap <id|feature> <account>
                               move a dispatcher onto another subscription and resume
                               its conversation there (a on its row in the cockpit)
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
	case "steward":
		os.Exit(steward.Run(args[1:], os.Stdout, os.Stderr))
	case "status", "reply", "park", "unpark", "note", "tidy", "swap", "ruling", "merge":
		// The triage table's reading and its three hands, for a session
		// stewarding the fleet — see internal/fleetcmd.
		os.Exit(fleetcmd.Run(args[0], args[1:], os.Stdout, os.Stderr))
	case "account", "accounts":
		os.Exit(accountcmd.Run(args[1:], os.Stdout, os.Stderr))
	case "statusline":
		// The status line every account runs: it records the subscription's
		// 5-hour and weekly limits (internal/account), then draws whatever the
		// human's own status line drew. Like the hook, it never fails.
		os.Exit(account.RunStatusLine(args[1:]))
	case "hook":
		// Never fail loudly: a hook error must not disturb the Claude session.
		os.Exit(hookcmd.Run(args[1:]))
	case "version", "--version", "-v":
		fmt.Println("claude-dispatcher", version.Display())
		// How this build was installed, and what would replace it. This is the
		// second line because it is the answer to "why is the cockpit not
		// offering me the upgrade key?" — an install we cannot place says so
		// here rather than staying silent about it.
		if in := version.Detect(); in.CanUpgrade() {
			fmt.Println(string(in.Method), "·", in.Hint())
		} else {
			fmt.Println(in.Hint())
		}
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
