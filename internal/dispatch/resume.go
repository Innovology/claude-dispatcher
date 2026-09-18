package dispatch

// resume.go reopens a dispatcher whose session is over.
//
// A dispatch does not stop existing when its claude session ends: the record,
// the branch, the worktree and — the part that matters — the session transcript
// all survive. Claude Code can pick that transcript back up (`claude --resume
// <session id>`), but only from the directory the session ran in, because that
// is what its project store is keyed by. So resuming is: put the worktree back
// if it was reclaimed, and start claude in it with the recorded session id.
//
// What it deliberately does not do is invent a session. With no recorded id and
// no transcript to read one from, there is nothing to resume, and the caller is
// told so rather than handed a fresh session dressed up as the old one.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// The supervisor calls Resume makes, as seams: tests swap them rather than
// stand up real sessions. sessionAlive is shared with Launch.
var (
	sessionIdle = supervisor.SessionIdle
	killSession = supervisor.KillSession
	uniqueName  = supervisor.UniqueName
	newSession  = supervisor.NewSession
)

// ResumeMode is what Resume actually did, so the caller's notice can say it
// rather than assume it.
type ResumeMode string

const (
	// ResumeStarted: a session is running claude --resume on the transcript.
	ResumeStarted ResumeMode = "started"
	// ResumeLive: the dispatcher's own session is still busy, so nothing was
	// started — the thing to do with it is attach, not resume.
	ResumeLive ResumeMode = "live"
)

// Resume reopens d's Claude session in its own worktree, optionally with prompt
// as the first thing said to it, and returns the session name to hand the
// terminal to.
//
// The record is updated in place (new session name, status back to launching)
// and saved: a record left at done would make the lifecycle hook swallow
// everything the resumed session says — done is terminal for every event but
// proof of life (see hookcmd.reopensDone).
func Resume(d *state.Dispatch, prompt string) (ResumeMode, string, error) {
	if d == nil {
		return "", "", fmt.Errorf("no dispatch record")
	}
	sid := SessionIDOf(d)
	if sid == "" {
		return "", "", fmt.Errorf("%q never recorded a session id — there is nothing to resume", d.Feature)
	}
	// The transcript is what --resume reads. An absent one is the honest end of
	// this road: claude would open, fail to find the conversation and leave the
	// human in a session that is not the one they asked for.
	if d.TranscriptPath != "" {
		if _, err := os.Stat(d.TranscriptPath); err != nil {
			return "", "", fmt.Errorf("the transcript for %q is gone (%s)", d.Feature, d.TranscriptPath)
		}
	}

	dir := d.WorktreePath
	if dir == "" {
		dir = d.RepoPath
	}
	if dir == "" {
		return "", "", fmt.Errorf("%q recorded no directory to resume in", d.Feature)
	}
	// Same guard as Launch: one live dispatcher per slug, because the worktree
	// path is keyed by it. Resuming into a checkout another session is working
	// in is the collision per-dispatch worktrees exist to prevent.
	if live := liveDispatch(d.Slug); live != nil && live.ID != d.ID {
		return "", "", fmt.Errorf("%q is live in the same worktree (session %s) — kill it first",
			live.Feature, live.TmuxSession)
	}

	// The session's own supervisor session, if it outlived claude. Idle means
	// claude has ended and the name is free to take back; busy means the
	// dispatcher is still going and resuming it would fork the transcript under
	// a session that is still writing to it.
	//
	// This is why SessionIdle has to be right about a session it can only see a
	// shell in (see tmux.SessionIdle): "idle" here kills the session, and a
	// probe that called every live claude idle was killing the very dispatcher
	// the human had just asked to reopen.
	if d.TmuxSession != "" && sessionAlive(SessionOf(d)) {
		idle, known := sessionIdle(SessionOf(d))
		switch {
		case known && !idle:
			return ResumeLive, d.TmuxSession, nil
		case known && idle:
			_ = killSession(SessionOf(d))
		}
		// Unknown: leave it running and take a new name below. A backend that
		// cannot see into a session must not have it killed on a guess.
	}

	// The worktree may have been reclaimed when the dispatcher was killed
	// (CleanupWorktree removes a clean one). Put it back at the same path —
	// which is what makes the transcript findable again — reusing the feature
	// branch when it still exists.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if d.RepoPath == "" || d.Branch == "" {
			return "", "", fmt.Errorf("%s is gone and there is not enough on the record to rebuild it", dir)
		}
		// RootDefault, not d.Root: a rebuild is meant to put the branch that
		// already exists back on disk, and if it has gone too, the dispatcher is
		// being restarted from the current default rather than from wherever it
		// once began. Naming the recorded root here would refuse the rebuild
		// outright (see reusedRootErr) in the common case where the branch is
		// still there.
		if _, err := ensureWorktree(d.RepoPath, dir, d.Branch, RootDefault); err != nil {
			return "", "", fmt.Errorf("rebuild %s: %w", dir, err)
		}
		InheritTrust(d.RepoPath, dir)
	}

	base := "disp-" + d.Slug
	if base == "disp-" {
		base = "disp-" + d.ID
	}
	// The mode and model it was dispatched in, so reopening a dispatcher does
	// not quietly change what it may do without asking or what it runs on. A
	// record from before either was a choice normalises to the default rather
	// than inheriting nothing: --resume still has to be given some mode, and
	// the default is the one the form would offer.
	// The opening message travels the way a launch prompt does — as a file the
	// session reads — because it is the same command line with the same
	// supervisor ceiling on it, and the resume input is not capped either. It
	// is written under its own name so reopening a dispatcher never overwrites
	// the record of what it was originally sent.
	promptPath := ""
	prompt = strings.TrimRight(prompt, "\n")
	if prompt != "" {
		if len(prompt) > MaxPromptBytes {
			return "", "", fmt.Errorf("the message is %d bytes and the limit is %d — say it in the session instead",
				len(prompt), MaxPromptBytes)
		}
		p, err := state.WritePrompt(d.ID+"-resume", prompt)
		if err != nil {
			return "", "", fmt.Errorf("could not write the message for %q: %w", d.Feature, err)
		}
		promptPath = p
	}

	// The same server and the same environment it went out in, both read off
	// the record rather than resolved again: a repo's config may have changed
	// since, and a session reopened somewhere else is a different session.
	name := uniqueName(supervisor.Session{Name: base, Socket: d.TmuxSocket})
	sess := supervisor.Session{Name: name, Socket: d.TmuxSocket}
	if err := newSession(sess, dir, resumeCommand(d.ID, sid, promptPath, Mode(d.Mode), Model(d.Model)), d.EnvCommand); err != nil {
		return "", "", err
	}

	started := time.Now()
	d.TmuxSession = name
	d.Status = state.StatusLaunching
	d.StatusReason = "resuming its session"
	d.SessionStartedAt = &started
	d.WaitingOnTasks = false
	if err := state.Save(d); err != nil {
		return ResumeStarted, name, err
	}
	return ResumeStarted, name, nil
}

// SessionIDOf is the Claude Code session id to resume: the one the hook bound
// to the record, or — for a record from before that binding, or one whose
// SessionStart was never attributed — the transcript's own filename, which
// Claude Code names after the session. "" when neither exists.
func SessionIDOf(d *state.Dispatch) string {
	if d == nil {
		return ""
	}
	if d.SessionID != "" {
		return d.SessionID
	}
	if d.TranscriptPath == "" {
		return ""
	}
	base := filepath.Base(d.TranscriptPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
