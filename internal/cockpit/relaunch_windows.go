//go:build windows

package cockpit

import "fmt"

// relaunch says what happened and stops.
//
// Windows has no exec-in-place: CreateProcess always makes a new process, so
// the closest equivalent is spawning a child and exiting under it, which hands
// the console to two programs at once and leaves the shell's job tracking
// pointing at a process that has gone. Telling the human one true sentence is
// better than that. Scoop and winget have both already replaced the binary by
// the time this runs, so the next start is the new build.
func relaunch() error {
	fmt.Println("upgraded — start claude-dispatcher again to run the new build")
	return nil
}
