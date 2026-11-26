package dispatch

// startdied.go is a launch whose claude died before it reported in: told apart
// from one still starting, retired with the pane's own words, and waited for
// by a caller that is about to tell somebody the launch worked (ADR 0038).
//
// A launch used to count as started the moment the supervisor accepted the
// session. That is the pane existing, not claude running in it: the launch line
// ends in an exit shell on purpose, so a claude that dies at once leaves a live
// session behind, and the sweep that retires a launch only looked for a session
// that had gone. Reported from player-app on 2026-11-26: a handed-off dispatch
// whose claude refused to start in a deleted directory sat at "starting
// session" with nu in its pane, and the session that handed it off told the
// human the handoff had worked.

import (
	"strings"
	"time"
	"unicode"

	"claude-dispatcher/internal/cause"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// StartSettle is how long after the supervisor accepted a session its pane may
// read idle without that meaning anything.
//
// The pane's first process is the launch line's /bin/sh, and until it has
// forked anything — the `cat` that reads the prompt is the first child — it is
// a shell with nothing under it, which is exactly what SessionIdle calls idle.
// That lasts a few milliseconds on an idle machine, but a sweep can land in it,
// since the record is stamped the moment new-session returns and the stamp is
// itself a write the cockpit reloads on. Nothing the session does takes longer
// to show than this: from then on an idle pane is a claude that has ended.
const StartSettle = 3 * time.Second

// capturePane is the seam the dead launch's screen is read through.
var capturePane = supervisor.CapturePane

// diedStarting reports whether d is a launch whose claude has ended before any
// hook fired: still launching, its session accepted at least StartSettle ago
// and alive (the caller has established that), and every pane of it sitting at
// a shell with nothing under it — the launch line's exit shell, which the pane
// becomes only once claude has gone.
//
// Only an idle shell is evidence. A pane that is busy is a claude starting, or
// one stopped on a startup prompt (LaunchStalled), and a pane the backend
// cannot see into is unknown; neither says the start failed.
func diedStarting(d *state.Dispatch, now time.Time) bool {
	if d.Status != state.StatusLaunching || d.SessionStartedAt == nil || now.Sub(*d.SessionStartedAt) < StartSettle {
		return false
	}
	idle, known := sessionIdle(SessionOf(d))
	return known && idle
}

// retireDiedStarting marks d exited with the reason its pane gives and keeps
// the whole screen where `l` opens it (StartOutput). It writes under the hook
// lock against a fresh read, and only if the record is still the launch that
// was judged: a SessionStart landing in between, or a relaunch that has since
// stamped a new session, is not a start that failed. d is updated to what was
// saved, because the sweep's callers read the slice they passed in.
//
// The session is left as it is. It is a pane at a shell, which is the
// inspection the exit shell exists for, and ⏎ on the row kills an idle session
// of its own before starting the dispatch again (Resume).
func retireDiedStarting(d *state.Dispatch, now time.Time) bool {
	screen, err := capturePane(SessionOf(d))
	reason := diedReason(screen, err)

	release := state.Lock()
	defer release()
	var cur *state.Dispatch
	for _, r := range state.LoadAll() {
		if r.ID == d.ID {
			cur = r
			break
		}
	}
	if cur == nil || cur.Status != state.StatusLaunching || cur.SessionStartedAt == nil ||
		!cur.SessionStartedAt.Equal(*d.SessionStartedAt) {
		return false
	}
	cur.Stop(state.StatusExited, reason, now)
	cur.SweepSubagents(now)
	cur.StartOutput = ""
	if strings.TrimSpace(screen) != "" {
		// The reason heads the file as an error line, so the editor `l` opens
		// starts on it (cause.LineOf) rather than on the first complaint the
		// exit shell made after claude had gone.
		if path, werr := state.WriteOutput(cur.ID, "error: "+reason+"\n\n"+screen); werr == nil {
			cur.StartOutput = path
		}
	}
	if state.Save(cur) != nil {
		return false
	}
	*d = *cur
	return true
}

// diedLead opens the reason a launch retired by diedStarting carries.
const diedLead = "claude exited before it reported in"

// diedReason is the one line a dead launch's row shows: that claude exited
// before reporting in, the status the launch line saw, and what claude said —
// the cause (internal/cause) of what the pane printed before the exit mark,
// read the way a failed command's output is.
func diedReason(screen string, err error) string {
	lead := diedLead
	said, status, found := cutAtExit(screen)
	if found && status != "" {
		lead += " (status " + status + ")"
	}
	if c := cause.Of(wordsOnly(said)); c != "" {
		if !found {
			// No mark: a pane from a build that printed none. Its last words
			// may be the exit shell's own, so they are quoted as the pane's.
			return lead + " — its pane last read: " + c
		}
		return lead + ": " + c
	}
	if err != nil {
		return lead + " — its pane could not be read: " + firstLine(err.Error())
	}
	return lead + " and printed nothing"
}

// wordsOnly drops the lines of a screen that hold no letter or digit: the
// borders of a box claude drew, rules, a bare prompt sign. Read from the end
// for a cause, the bottom edge of a dialog is otherwise the last thing claude
// said.
func wordsOnly(screen string) string {
	var keep []string
	for _, ln := range strings.Split(screen, "\n") {
		if strings.IndexFunc(ln, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			keep = append(keep, ln)
		}
	}
	return strings.Join(keep, "\n")
}
