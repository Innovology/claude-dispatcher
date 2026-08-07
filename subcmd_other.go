//go:build !windows

package main

// maybeWindowsSubcommand is a no-op off Windows: there are no platform-specific
// hidden subcommands here, so main() proceeds to its normal dispatch.
func maybeWindowsSubcommand(args []string) (handled bool, code int) {
	return false, 0
}
