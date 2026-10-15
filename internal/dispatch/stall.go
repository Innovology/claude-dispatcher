package dispatch

import (
	"time"

	"claude-dispatcher/internal/state"
)

// LaunchStallAfter is how long a session the supervisor started may stay
// silent before its silence is read as a wait on the human.
//
// Nothing in claude reports a startup dialog. Its trust prompt, a first-run
// login, an onboarding question — each sits in the pane before the session
// exists, so no hook fires: not SessionStart, not a Notification. Reported on
// a dispatch into design-system whose row read "starting session" while the
// pane asked whether to trust the folder. A claude that is starting normally
// reports SessionStart within a few seconds of its pane opening — the slow part
// of a launch, a nix dev shell, is spent before the supervisor returns and
// SessionStartedAt is stamped — so twenty seconds of silence after that is a
// session waiting on someone.
const LaunchStallAfter = 20 * time.Second

// LaunchStalled reports whether d's session started and has not reported in
// since: a launching record the supervisor accepted, no session id bound by a
// hook, LaunchStallAfter gone by.
func LaunchStalled(d *state.Dispatch, now time.Time) bool {
	return d != nil && d.Status == state.StatusLaunching && d.SessionStartedAt != nil &&
		d.SessionID == "" && now.Sub(*d.SessionStartedAt) >= LaunchStallAfter
}
