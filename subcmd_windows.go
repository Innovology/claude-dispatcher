//go:build windows

package main

import (
	"fmt"
	"os"

	"claude-dispatcher/internal/supervisor"
)

// maybeWindowsSubcommand handles the hidden, Windows-only `win-focus <name>`
// subcommand used by the cockpit's "jump in": it raises the named session's
// console window to the foreground. It is intentionally absent from the usage
// text. Returns handled=false for anything else so main() dispatches normally.
func maybeWindowsSubcommand(args []string) (handled bool, code int) {
	if len(args) == 0 || args[0] != "win-focus" {
		return false, 0
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "win-focus: usage: claude-dispatcher win-focus <name>")
		return true, 2
	}
	if err := supervisor.WinFocus(args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "win-focus:", err)
		return true, 1
	}
	return true, 0
}
