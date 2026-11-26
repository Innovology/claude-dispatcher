package dispatch

import "strings"

// ExitMark opens the line a dispatch's pane prints when its claude exits,
// followed by the exit status: "[claude-dispatcher] claude exited with status 1".
//
// It is printed by the launch line itself, between claude and the exit shell
// (exitShell), so it is in every pane a dispatch, a resume or the steward
// opens on Unix. The Windows console keeps its `pause` and prints none, and
// nothing there reads one: a console's screen cannot be read back.
const ExitMark = "[claude-dispatcher] claude exited with status "

// cutAtExit splits what a pane printed at its last ExitMark: said is what came
// before it — what claude printed, and whatever the pane showed before claude
// started — and status is the exit status the mark carries. found is false
// for a pane with no mark, which is a claude still running, or a pane launched
// by a build that printed none; said is then the whole of it.
//
// The last mark, because a mark is printed only by our own launch line and the
// pane is new with every launch: an earlier one in the same pane can only be
// a claude the human ran again by hand and then left, and the exit that
// matters is the one the launch line saw.
func cutAtExit(screen string) (said, status string, found bool) {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), ExitMark)
		if !ok {
			continue
		}
		return strings.Join(lines[:i], "\n"), strings.TrimSpace(rest), true
	}
	return screen, "", false
}
