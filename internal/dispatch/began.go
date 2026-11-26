package dispatch

// began.go answers the question a resume has to ask before it refuses a
// missing transcript: did this conversation ever begin?
//
// A session id on a record is not proof of a conversation. Claude Code gives a
// session its id when claude starts and writes its transcript when the first
// message goes in, so a claude opened and closed again without a word leaves an
// id, a transcript path naming a file that was never written, and nothing for
// `claude --resume` to open. The record that led here got one from a claude the
// human ran by hand in a dead dispatch's pane (ADR 0039): SessionStart at
// 14:20:55, SessionEnd at 14:24:43, nothing between, and every ⏎ after that
// refused with "the transcript … is gone" — eight times between 14:25 and
// 14:33, with no key on the row that could start the dispatch it was.
//
// But a transcript that was written and has since gone looks exactly the same
// from the record, and it is the common case: Claude Code clears old
// transcripts away itself, and measured on the reporting machine, 105 of the
// 126 sessions with turns from before the last 25 days no longer have one.
// Relaunching the brief there would set a fresh claude to do over again the
// work that conversation did. So the answer is read from evidence of the
// session's whole life, not from the missing file, and anything short of that
// evidence is "it began".

import (
	"os"
	"strings"
	"time"

	"claude-dispatcher/internal/state"
)

// neverBegan reports whether sid, the session d records, is one that never had
// a turn — a session start with no conversation behind it, which is to be
// started again from d's brief rather than resumed or refused. All of these
// must hold, the cheapest asked first:
//
//   - no transcript for sid, at the recorded path or under d's config dir. A
//     path that cannot be read is not a path that is gone.
//   - no work on d's branch since it was dispatched: no commits on the record,
//     and none on the branch now. This is the thing a wrong answer would cost,
//     so it is asked directly rather than inferred.
//   - the event log, read to its end, holds sid's SessionStart. That is what
//     makes its silence evidence: the log saw the session begin and has
//     recorded every hook since, so a turn it does not hold did not happen. A
//     log that never saw the session (deleted, a scratch store, hooks not
//     installed at the time) has not said anything.
//   - and no turn in that log, either under sid or under d's own id in any
//     session: a dispatch whose own claude worked under another id, then had
//     a stranger's session put on its record, has a conversation — it is just
//     not the one the record names.
func neverBegan(d *state.Dispatch, sid string) bool {
	if d == nil || sid == "" {
		return false // no session at all is NeverStarted's case
	}
	if transcriptMayExist(d, sid) {
		return false
	}
	if len(d.Commits) > 0 || branchMoved(d) {
		return false
	}
	events, complete := state.EventsMentioning(sid, d.ID)
	if !complete {
		return false
	}
	witnessed := false
	for _, ev := range events {
		mine := ev.SessionID == sid
		if turnEvent(ev.Event) && (mine || ev.DispatcherID == d.ID) {
			return false
		}
		if mine && ev.Event == "SessionStart" {
			witnessed = true
		}
	}
	return witnessed
}

// turnEvent reports whether a hook event is one a turn produces: a prompt
// reaching the session, the session answering (Stop, or StopFailure in its
// place), or anything that only happens inside a turn — a tool, a dialog, a
// subagent.
//
// It is a list of what a turn writes, not of what an idle session does not,
// so that nothing the cockpit writes itself can count. The audit lines carry
// session ids too — every resume of the reported record wrote one, eight of
// them under the turnless session's id — and they say what the cockpit did,
// never what the session did.
//
// Notification:idle_prompt is left out because it is not a turn: measured in
// the reporting log, one session drew it a minute after starting, with no
// prompt and no stop in its whole life. Stop is in even alone, because a turn
// can show without its prompt: one session in the same log has two Stops and
// no UserPromptSubmit.
func turnEvent(name string) bool {
	switch name {
	case "UserPromptSubmit", "Stop", "StopFailure",
		"PermissionRequest", "Notification:permission_prompt",
		"SubagentStart", "SubagentStop", "PostToolUse":
		return true
	}
	return strings.HasPrefix(name, "PreToolUse")
}

// transcriptMayExist is false only when sid's transcript is known to be absent:
// no file at the recorded path, and none under d's config dir — the place a
// swap or a project folder renamed under it would have put it.
func transcriptMayExist(d *state.Dispatch, sid string) bool {
	if d.TranscriptPath != "" {
		if _, err := os.Stat(d.TranscriptPath); err == nil || !os.IsNotExist(err) {
			return true
		}
	}
	return findTranscript(configDirOf(d), sid) != ""
}

// branchMoved reports whether d's branch carries commits made since it was
// dispatched (BaseSHA is the tip it was cut at). A branch that is not there
// carries nothing; one that is there and cannot be counted might carry
// anything, so it counts as moved.
func branchMoved(d *state.Dispatch) bool {
	if d.RepoPath == "" || d.Branch == "" || d.BaseSHA == "" {
		return false
	}
	ref := "refs/heads/" + d.Branch
	if !resolves(d.RepoPath, ref) {
		return false
	}
	n, err := gitOut(d.RepoPath, 10*time.Second, "rev-list", "--count", d.BaseSHA+".."+ref)
	return err != nil || n != "0"
}
