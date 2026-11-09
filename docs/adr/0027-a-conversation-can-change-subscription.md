# 27. A conversation can change subscription

Date: 2026-11-09

## Status

Accepted. Extends 0022.

## Context

Asked for as "enable a user to swap subscriptions within a dispatch, say for
example you run out of credits".

ADR 0022 put each dispatch on one account for life: `Resume` reopens under the
recorded `ConfigDir` because the transcript `--resume` reads is in that
directory. So a dispatcher whose subscription ran out could only wait for the
window to reset, or be dispatched again from nothing on another login,
throwing away the conversation that was the work so far. The day this was
built the default login answered "You've hit your monthly spend limit … your
weekly limit resets Nov 10" while a second account had room — the exact case.

Measured on claude 2.1.281 before anything was built on it: a session made
under one config directory and resumed under another answers `No conversation
found with session ID: …`. Copied into the second directory under the same
`projects/<folder>/`, the same session id resumes there and carries on. The
folder is named for the working directory, which a swap does not change, so
the copy keeps its name. Beside the transcript Claude Code keeps a folder of
the session's subagent transcripts and tool results (`projects/<folder>/<id>/`)
and its rewind checkpoints (`file-history/<id>/`).

## Decision

**A swap is a copy and a resume.** `dispatch.SwapAccount` copies the
transcript, its folder and its checkpoints into the new account's directory
(each file written whole and renamed into place), ends the old claude, points
the record's `Account`, `ConfigDir` and `TranscriptPath` at the new home,
inherits the worktree's trust into that login's `.claude.json`, and hands the
record to `Resume`. Everything 0022 says about resuming still holds; the
record simply says a different directory now.

**It is a copy, never a move.** The old directory's transcript is what that
login spent and is part of the human's own history there, and it is the way
back if the new account refuses too. Swapping back copies the newer
transcript over the old one, so each login's copy is at most one swap stale.

**It is offered only where ending claude loses nothing.** The old claude must
go — two sessions writing one conversation under two logins fork it — so the
turn must be over: no session, claude exited, needs-input, exited, or done
with the closing message on the record (a done record alone says nothing,
since the tracker marks a merged PR done while its session works on). Mid-turn
and on a menu are refused by name. An adopted session is never restarted: it
is the human's (0026), and `/login` in it is theirs to do.

**`continue` only for a turn an error ended.** Running out arrives as
`StopFailure` with a usage-limit category, which is not one `RetryFailed`
types at; nothing inside the session will start another turn. So a swap whose
last turn failed opens with `continue`, and one that ended on a question opens
with nothing — answering it would be the swap deciding something for the
human.

**Same refusals as a launch.** The target account is put through
`account.Problems`: not logged in, no hooks, never opened. The account it is
already on is refused.

**Where.** `a` on a triage row whose turn is over opens a picker listing every
account with what it has left (the forms' figures, probed as it opens), the
current one marked; enter moves it and the table stays — the row goes to
"resuming its session" and on. `claude-dispatcher swap <id|feature>
<account> [message]` does the same from a shell. It is not on the steward's
allowed list: which subscription pays is spend, and spend is the human's.
Each swap is an `AccountSwapped` event, "from → to", unbilled like the other
audit events.

## Consequences

- The usage lens's transcript fallback reads only `~/.claude`, so a copy into
  another directory is not double-counted there; the old directory keeps the
  tokens it actually spent.
- A swap restarts the claude process, so background tasks the old session had
  running end with it. Out of credits they had stopped anyway.
