# 39. A session id is not a conversation

Date: 2026-11-26

## Status

Accepted.

## Context

Record `4c6d1447ab92`, "adopt DS 3.21.2 on main" in player-app, could not be
resumed and could not be started. Every ⏎ on its row refused with "the
transcript for … is gone": eight `Resumed` events between 14:25 and 14:33,
each one a refusal.

What happened, from the event log:

- 14:03:23, `DispatchLaunched`. The dispatched claude never came up, because
  its pane started in a deleted directory. That cause is fixed on its own
  (ADR 0037), and confirming that a start happened is being fixed separately.
- 14:20:55, a `SessionStart` from the dispatch's worktree with **no dispatcher
  id**. This was a `claude` the human ran by hand in the dead pane. The hook's
  last attribution rule bound a SessionStart to the newest *launching* record
  whose worktree it started in, so this claude's session id and transcript
  path went onto the record.
- 14:24:43, `SessionEnd`. Nothing in between: no prompt and no stop. Claude
  Code writes a transcript when the first message goes in, so none was ever
  written. The project folder for that worktree does not exist at all.

So the record named a conversation with nothing behind it. `Resume` refuses a
missing transcript (`resume.go`), and `SwapAccount` has the same refusal when
it copies one (`swap.go`). Neither looked further. `Resume` already handles a
record with no session id: ADR 0009 runs the launch again from the recorded
prompt. This record was that same case with a session id attached.

The missing file alone cannot be the test, though. A transcript that was
written and has since gone looks exactly the same on the record, and that is
the common case. Measured on the reporting machine: of the sessions with
turns older than 25 days, 105 of 126 no longer have a transcript. Claude Code
clears old ones away itself, and a human can delete one. Running the brief
again over such a conversation would set a fresh claude to redo, on a branch
that already holds it, work the old one did. Of the 90 records in the
reporting store, 24 carry a session id with no transcript behind it. Only one
of them is this record.

The event log can tell the two apart. It is append-only and goes back to
2026-08-11, and every hook writes to it whether or not the event is
attributed to a record. Of the 456 sessions whose start it saw, 199 never had
a turn. In the last 25 days, all 131 sessions with a turn still have a
transcript, and 56 of the 62 turnless ones never got one. The other six have
small transcripts holding only mode and system lines, from local commands.
Two shapes in the log mattered for the rule:

- A `Notification:idle_prompt` can fire with no turn. One session drew it a
  minute after starting and never took a prompt.
- A turn can show without its prompt. One session has two `Stop`s and no
  `UserPromptSubmit`.

The hook's join by working directory was the other half of the defect.
Every launch and every resume command has carried `CLAUDE_DISPATCHER_ID`
inline since the scaffold, on Unix and Windows. So a fresh launch never
needed the join, and the join only ever caught a claude the dispatcher did
not start. A dispatch's pane drops to a shell when its claude exits, and that
shell does not inherit the id, because `VAR=… claude` sets it for claude
alone. Over all 90 records the join had bound exactly one: this one. It could
also do worse. A resumed record is *launching* until its claude reports. If
that claude does not come up and the human starts one by hand, the join puts
the stranger's session id over the dispatch's own, and the real conversation
can then be found only in the log.

## Decision

**A recorded session that never had a turn is started again, not resumed.**
`Resume` relaunches when the record has no session id (ADR 0009), or when its
session never began (`dispatch.neverBegan`). A relaunch runs the original
launch in the dispatch's worktree with the prompt it was sent with. The
turnless session leaves the record at the point the relaunch is certain, so
the new claude's own SessionStart is the one the record carries. If the
relaunch then fails, the row offers to start again, not to resume nothing.

**"Never began" is decided on evidence, and anything short of it refuses.**
All of these must hold, cheapest first:

- There is no transcript for the session, at the recorded path or under the
  record's config dir. A path that cannot be read is not a path that is gone.
- There is no work on the branch since it was dispatched: no commits on the
  record, and none in `BaseSHA..branch` now. This is what a wrong answer would
  cost, so it is asked directly. A branch that is not there carries nothing;
  one that is there and cannot be counted counts as moved.
- The event log, read to its end (`state.EventsMentioning`), holds that
  session's `SessionStart`. Its silence is evidence only because it saw the
  session begin. A log that never did — deleted, a scratch store, hooks not
  installed at the time — has said nothing.
- The log holds no turn under that session, and none under the dispatcher's
  own id in any session. The second part matters: a dispatch whose own claude
  worked under one id and then had a stranger's session put on its record has
  a conversation, just not the one the record names.

A turn is what a turn writes: `UserPromptSubmit`, `Stop`, `StopFailure`, any
`PreToolUse` or `PostToolUse`, `PermissionRequest`,
`Notification:permission_prompt`, `SubagentStart` and `SubagentStop`. It is a
list of what a turn produces, not of what an idle session does not, so
nothing the cockpit writes about itself can count. Those audit lines carry
session ids too: all eight `Resumed` refusals were under the turnless
session's id. An idle prompt is not a turn, and a `Stop` on its own is.

The log is read for one session's lines only. A line is matched as text
before it is decoded, so the read takes 17ms where `LoadEvents` takes 215ms
on the 30MB log. Lines are read whole however long they are, and a read that
stops early reports itself incomplete, because this caller asks the log what
is *not* in it.

**A swap goes the same way.** A turnless session has no conversation to copy
into the new account's directory. `SwapAccount` copies nothing and types no
`continue`, and `Resume` starts the dispatch again under the new login. A
conversation that existed and has gone still refuses there too.

**A claude the dispatcher did not start is not joined to a record.** A hook
event is attributed by the dispatcher id the session carries, or by a session
id a record already holds (which is how an adopted session is found), and by
nothing else. Where a session started says nothing about whose it is. Its
events are still logged. Bringing a session the human started under a record
is what adoption is for (ADR 0026).

**The row reads the record and does not read the log.** `NeverStarted` is asked
on every draw, so it stays a read of the record. A row whose session never
began offers ⏎ resume. Once `Resume` has found out, the notice says the
dispatch was started again because there was nothing to resume, which is how
`ResumeMode` already reports a resume that attached instead.

## Consequences

- Checked read-only against the reporting store: the rule finds the reported
  record and no other. The 23 other records with a session id and no
  transcript all began, and still refuse.
- The reported record already carries the session id. ⏎ on it now starts the
  dispatch again. Records bound by the join before this change are covered the
  same way, because the rule reads the log rather than the hook's decision.
- With the join gone, a launch whose claude never comes up keeps no session
  id. The start-stall check marks it as stopped before its start, and once its
  session is gone the ghost sweep (ADR 0004) retires it as a launch that died
  before claude ran, which ⏎ starts again (ADR 0009). A hand-run claude in its
  pane no longer changes that row. To be tracked, that claude has to be
  adopted.
- PR #100 (ADR 0035) asks the machine whether a conversation is already
  running before it resumes one. A turnless session takes the relaunch path
  instead, and a relaunch starts a new conversation, not a second claude on an
  old one. A live claude in the dispatcher's own pane is still attached to,
  not killed: the session check that comes before both paths decides that.
- A record whose log was lost, or whose session started before the hooks were
  installed, cannot be shown never to have begun, and still refuses. That is
  the side a doubtful case should land on.
