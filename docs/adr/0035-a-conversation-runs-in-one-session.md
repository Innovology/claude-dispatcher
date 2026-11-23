# 35. A conversation runs in one session, and the machine says where

Date: 2026-11-23

## Status

Accepted.

## Context

Reported after the machine was hard rebooted: "now that i come into the
dispatcher and start the 'finished' sessions i have a mismatch between what
the sessions think and what i see". On player-app's
one-button-production-publish, the session ⏎ opened said another session was
already running the CI for its pull request. Attached by hand with
`tmux -L player-app attach -t disp-one-button-production-publish`, that other
session was there, doing the same work. The guess was a socket mismatch.

The socket was right. The player-app server held two sessions for the one
record, and each was running the same conversation:

    disp-one-button-production-publish    created 12:14:55  claude --resume 7c4eb271…
    disp-one-button-production-publish-2  created 12:15:18  claude --resume 7c4eb271…

Both were in the same worktree. Claude Code's own registry
(`~/.claude/sessions/<pid>.json`) named the same session id for both pids.
The record named the `-2`, so the row and ⏎ followed that one. Both claudes
carried the record's `CLAUDE_DISPATCHER_ID`, so both reported to it, and both
wrote to the one transcript. They also found each other through Claude Code's
peer messaging. The `-2` one's last message is the defect in its own words:
"the other session … worked out a CI queue order with the other PR sessions
… I stood down without asking you."

Resume decided whether a dispatcher is still running by checking the session
the record names: alive, held by this record, and busy. That is correct only
if the record it is handed is the current one, and it was not:

- The cockpit handed Resume `recordByID`, the copy its last snapshot read.
- A load is slow: 4.5s warm and 61s cold on 94 records (ADR 0007), and every
  load just after a reboot is cold.
- The first ⏎ resumed under `nix develop`, about twenty seconds here, then
  saved the new session name. A load that had read the records before that
  save landed after it, and the row read "finished · ⏎ resume" again.
- The second ⏎ resumed the copy from before the first resume. To that copy
  the session the first resume started had been created after its hold
  (`HoldsSessionBorn`). So it was "a younger one that has taken the name",
  which Resume leaves alone while it takes a new name (ADR 0026's rule for a
  human's wrapper reusing a name). The new name was `-2`.

There was also nothing between the two ⏎s to stop the second one. The row
offers ⏎ resume for as long as the record on disk says finished, which is the
whole of the `nix develop`. Nothing in the event log recorded a resume either:
telling one resume from two took the tmux creation times.

## Decision

**A conversation runs in one session, and the machine says which.** Before
Resume starts a claude it lists every session on the record's server and the
processes under their panes. That is one `list-panes -a` and one read of the
process table (`supervisor.ServerPIDs`). It then asks which of those
processes is a claude running the record's conversation, the way adoption
already identifies a session (BindSession). The first source is Claude Code's
registry, checked against the pid's start time where `/proc` has it. The
registry is also looked for under the record's account dir, because on macOS
a process's environment cannot be read to find its `CLAUDE_CONFIG_DIR`. The
second source is the transcript a process holds open
(`dispatch.ConversationSessions`). Found, Resume starts nothing and returns
`ResumeLive` there, and the human is sent to the conversation that is
already running. On the reported machine the scan takes 0.11s for the server.

**The record is pointed at where its conversation runs**, if that session has
a name a launch would have given it (`disp-<slug>`, or that with UniqueName's
`-N`). Otherwise the ghost sweep retires a running dispatcher, and every ⏎
resumes it again. Only the address changes, on the record as it is on disk
and under the hook lock (`state.Load`). A stale copy is never saved back over
what the hooks have written since. The hold is stamped no later than the
session's creation, so the session reads as this record's. A session a human
started on the same server, for example under a wrapper's
`<repo>__<branch>`, is attached to and left theirs, because a record that
claimed it would end it on `x`.

**One resume of a record at a time.** The scan can only see a claude that
exists. During the first resume's `nix develop` none does yet, so Resume
holds a per-record lock (`state.TryLock`, a flock under `state/locks/`) for
its whole run. A second resume of the same record from anywhere is refused
with `ErrResuming` while the lock is held. That covers this cockpit, a second
cockpit, or a `swap` from the command line. It is refused rather than queued,
because a resume that waited its turn would only start the second claude a
minute later. The lock is best-effort the way `state.Lock` is: one that
cannot be made is no holder.

**The cockpit resumes the record on disk, and asks once.** `resumeCmd` reads
the record with `state.Load`, as `swapCmd` already did for the same reason. ⏎
on a row whose resume is still on its way starts nothing and says "still
resuming …" (`startResume`). That applies on the triage table and in the
product panel's resume overlay. The row is let go of when the answer comes
back, success or failure.

**A twin is named, not ended.** When ⏎ finds a conversation running and
attaches to it, any other session running the same conversation is named in
the notice: "its conversation is also running in disp-x — end the one you
do not want". Which of two live claudes holds the work the human wants is
not something the cockpit can tell, so it ends neither.

**Every resume is audited.** A `Resumed` event records the record, the
conversation, the directory, and what the resume did and where: "started in
disp-x", "live in disp-x", "relaunched in disp-x", or "failed: " and the
cause. Like the dispatch audit (ADR 0009), it is not a lifecycle event.

A swap that finds the conversation still running after asking the old claude
to end refuses. It does not report a move it did not make.

## Consequences

- Resuming a record whose conversation is running anywhere on its server
  attaches to it. Nothing the record says can start a second claude on it.
  The tests reproduce the report: with the scan off, Resume starts one.
- The scan covers the record's own server. A conversation resumed by hand on
  another server, or outside tmux, is not seen by it. The record's server is
  where every session the dispatcher starts lives, and asking every server on
  the machine on each ⏎ would buy coverage of a human's own act at the cost
  of everyone's keypress.
- The Windows console backend has no process tree to enumerate, so the scan
  finds nothing there. The lock and the cockpit's in-flight guard still
  apply.
- A twin made before this, like the reported one, stays until the human ends
  one of the pair. The cockpit names it whenever ⏎ attaches to either one.
- Lock files accumulate under `state/locks/`, one empty file per record ever
  resumed. They are left in place, because removing a file another process
  holds open lets two holders each lock a different file of the same name.
