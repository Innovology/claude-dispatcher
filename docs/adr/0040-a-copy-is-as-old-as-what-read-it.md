# 40. A copy is as old as what read it, and a hold answers to the conversation

Date: 2026-11-26

## Status

Accepted.

## Context

Reported as "session is reported 'stopped' in cockpit while active": the
dispatcher adopting design-system 3.21.2 into player-app (record
`7b388dcc30f3`) read as finished while it was working and watching its pull
request's CI. Neither ⏎ nor space-and-R brought it back.

The record said `exited`, "its tmux session is gone — a newer session has its
name". Its hold (`SessionStartedAt`) was 14:32:21.411916704, and the session at
its address, `disp-ds-3-21-2-adoption` on `-L player-app`, was created at
14:35:00. By ADR 0015's rule (`HoldsSessionBorn`) a session made after the hold
has only taken the name, so every load's sweep retired the record.

The session was the record's own. The event log has the whole story:

- 14:32:21: a handoff launched the record and stamped that hold. The pane
  started in a deleted directory (ADR 0037), claude never ran, and the shell
  stayed.
- 14:34:59.7: ⏎ "start again" relaunched it. Resume killed the idle shell's
  session, made a new one at 14:35:00, and saved the record holding
  14:35:00.1. The `Resumed` event at 14:35:00.169 says "relaunched in
  disp-ds-3-21-2-adoption".
- 14:46:21: claude reported in for the first time (`SessionStart`). That hook
  is the only writer of `session_id`.
- 16:53:03: the record carried the session id and the hold of 14:32:21.411916704
  again, nanosecond for nanosecond the value the 14:32 launch had written.

So a copy of the record read before 14:35:00 was saved whole after the
relaunch's save, and before 14:46:21, or the session id would have gone with
it. Nothing records which writer it was. Every candidate was the same mistake:
writing back a copy whole. The cockpit's keys (kill, ship, mark shipped, park,
unpark, dismiss) each took the record the screen was drawn from, changed it
and saved it whole. ⏎ "start again" keeps its row until a load reads the
relaunch back, so any of them pressed on that row wrote the pre-relaunch
record. The sweep saved its load's copy whole too, and a load takes seconds
warm and a minute cold (ADR 0007).

Then the hold did its damage. From 14:46:21, every hook put the record back to
working, because only `done` and a handoff are guarded against hooks. Every
load after it retired the record again. The row flickered between running and
finished for two hours. It was caught live at 17:09:28, when a hook revived the
record while its hold still said 14:32. Once the session went quiet, waiting
on CI behind a Monitor, no hook came and "finished" stuck.

⏎ could not fix it, because Resume asked the same hold. On the installed build,
which carries #100, Resume found the running conversation and attached, and
left the record as it was. On main, Resume would have read the session as a
stranger's and started a second claude on the conversation under a new name.
That is ADR 0035's twin, reached another way.

## Decision

**Anything that is not a hook writes a record as it stands on disk.**
`state.Edit(id, fn)` takes the hook lock, reads that one record fresh, hands it
to the act and saves it only when the act changed something. The cockpit's
keys go through it. Kill ends the session the record names now. Dismissing a
dispatcher that is running again dismisses nothing and says so, rather than
moving a live row to history on the screen's word (ADR 0013). The sweep applies
its verdict through it too (`retireSwept`), and only to the record the verdict
was about: the same status, session address and hold. A record that has moved
on since the load is left for the next load to judge.

**A hold answers to the conversation.** The hold stands in for a question the
machine can answer outright: is that session running this record's
conversation? Where the hold calls a named session younger, the sweep and
Resume ask Claude Code's own registry (`sessions/<pid>.json`), or a transcript
a claude under the pane holds open. These are the reads adoption uses
(ADR 0026). A session running the record's conversation is the record's
whenever it was made. The hold is what was wrong, and it moves to the session's
birth under the hook lock, so the next load asks nothing. A record with no
conversation, or a released one, is not asked about. A session running another
conversation is still a stranger's, so ADR 0015's case is untouched.

**⏎ undoes the sweep's verdict, and only that.** When Resume finds the
record's claude busy in its own session and the record carries the sweep's
ending (`sweptAway`), the record goes back to working with the reason "its
session is still running" before the session is handed back. Every other ending
is somebody's word: a SessionEnd, a kill, a handoff, a release, a merge. A
claude still at its prompt does not take it back. The sweep's reasons share
one lead (`sweptLead`), so the test and the writer cannot drift.

## Consequences

- On a fleet whose holds are right, the sweep asks nothing new. The question
  is asked only of an eligible record whose session is younger than its hold.
  That costs one process listing and a few file reads, and then the hold is
  right again.
- The reported record was repaired by hand on 2026-11-26: its hold was set to
  14:35:00 under the hook lock. The flip-flop stopped at once. Until a build
  carries this, the same chain can strand another dispatcher.
- A revived record reads "working" until its next hook. Its claude may in fact
  be at its prompt. A running claude is the one fact Resume has, and the next
  event says more.
- `resumeCmd` and `swapCmd` on main still hand Resume the screen's copy. #100
  moves both to `state.Load`. This change makes Resume's own write of the hold
  fresh, but not the copy it starts from.
- #100 (Resume asks the machine) and #103 (a start is confirmed) change the
  same functions: `ReconcileSessions` and Resume's own-session check. They
  compose: #100's `runningConversation` is the same question asked across a
  whole server, and this asks it of one session.
