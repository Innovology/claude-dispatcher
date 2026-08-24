# 9. A dispatch that did not happen is a thing that happened

Date: 2026-08-24

## Status

Accepted

## Context

Reported as "when the prompt is massive the dispatcher seems to just
disappear", and then, minutes later, "it's not just long prompts — that last
one failed with a one line prompt". Both were true, they had different causes,
and the reason they were indistinguishable is the more important defect.

### There was no audit

`dispatch.Launch` returns before `state.Save` for a refusal (a feature already
live, an empty slug) and for every `ensureWorktree` failure. Nothing else
writes anything until then. So a launch that failed early left:

- no dispatch record — the dispatches directory is untouched;
- no worktree — it is created inside the step that failed;
- no session — it is started after the record;
- no line in `events.jsonl` — that log is written by the lifecycle hook, which
  only ever fires from inside a session that started.

Checked on the reporter's own machine at the time: one record written that day,
one worktree created, no tmux session created after it. The failed attempt from
minutes earlier had left nothing at all behind to look at.

The only account of it was `m.notice`, one line in the cockpit footer, which
the next keypress or the next action replaces. And the placeholder row that
covers the launch window (`pending.go`) was *deleted* on failure, deliberately
— "leaving a row saying 'starting session' under a notice saying it did not
start would be the screen contradicting itself". True, and the wrong fix: the
row was the only thing on screen that knew the dispatch existed. Taking it away
left a table that looked exactly as it had a second earlier, which reads as
"nothing happened", and on an empty fleet the lens falls back to the dispatch
form, so submitting put the blank form back up — the screen the human had just
filled in.

That is why every different failure presented as the same one thing. The
product had no way to say what had gone wrong, so it said nothing.

### The long-prompt cause: the prompt was inside the command

`launchCommand` built one string — env var, flags, the prompt, and the trailing
drop to a login shell — and that string is handed to the supervisor as a single
argument. Every supervisor caps that.

tmux carries a command from client to server in one imsg, and `MAX_IMSGSIZE` is
16KB. Measured against tmux 3.7b on the reporter's machine, by bisection:

    largest new-session command that works: 16313 bytes
    fails at:                               16314   ("command too long")

So a prompt long enough to be worth writing took `tmux new-session` down.
`Launch` then marked the record exited with `StatusReason = "tmux launch
failed"` — four words that replaced the diagnosis tmux had just handed us — and
the cockpit deleted the row.

Windows was worse on both counts. `cmd.exe` caps a command line at 8191
characters, and `winQuote` was flattening every newline in the prompt to a
space on the way in, because a cmd command line cannot contain one. A prompt is
not a single line; dispatchers there were reading paragraphs run together.

The `+` form's prompt input made this harder to see rather than easier: a
`textinput` with `CharLimit = 500` silently drops everything past 500
characters, so that path truncated instead of failing. The dx form has no cap,
which is where the long prompts came from.

### The short-prompt cause: there are several, and they all looked alike

`ensureWorktree` refuses when the worktree it would reuse is not on
`feature/<slug>` any more. The reporter's fleet has two of those already — a
worktree on `fix/products-lens-polish` and one on `fix/usage-model-families`,
both left there by their own sessions renaming the branch mid-work. Re-dispatch
those names and the launch dies before writing anything. So does dispatching a
name whose branch the human has checked out in the repo itself; so does
`liveDispatch`'s refusal. One line of prompt, every time, and the same silence.

## Decision

**Every attempt to dispatch is audited, whether or not it produces a record.**
`DispatchAsked` goes in the event log before anything is created;
`DispatchLaunched` or `DispatchFailed` closes it, carrying the feature, the
repo and the reason verbatim. They are not lifecycle events — nothing derives
status from them and `velDwellState` leaves them unbilled — so they can be
added to a log other readers already walk without moving a single figure.

**A failed launch keeps its row, and the row says why.** SIGNAL reads "did not
start", the detail panel leads with the launch's own words, the glyph is red,
and `x` dismisses it: the one act a row with no record behind it can honour. No
snapshot retires it, because what it reports is that there is nothing for a
snapshot to find — only the human takes it off. A launch that got far enough to
write a record is the exception and hands over to it, since two rows for one
dispatch would be the cockpit contradicting itself.

**A session that would not start keeps its record, carrying the supervisor's
own words** rather than "tmux launch failed" for every cause there is.

**The prompt travels as a file, not inside the command.** It is written to
`state/prompts/<id>.txt` and the session's own shell reads it: `claude …
"$(cat …)"` on Unix, PowerShell's `ReadAllText` inside the console command on
Windows — which also ends the newline flattening. The launch command is a fixed
~300 bytes whatever the prompt says. Resume's opening message goes the same
way, under its own name, so reopening a dispatcher can never overwrite the
record of what it was originally sent.

**`MaxPromptBytes` is 100KB and the refusal is ours.** The prompt still reaches
claude as one argument, and a single argument is capped by the kernel (Linux's
`MAX_ARG_STRLEN` is 128KB; Windows caps a whole command line at 32767). Past
that the exec fails *inside* the session: claude never starts, no hook ever
fires, and the record sits at `launching` until a sweep retires it hours later.
Better to say so at the moment of asking.

The `+` form's `CharLimit` is gone with it. A silent truncation to 500
characters and a loud refusal at 100KB are not two settings of one dial; the
first is the product lying about what it was given.

## Consequences

- Measured, end to end, through the real binary: an 18,980-byte prompt pasted
  into the dispatch form now launches its session. A 49,699-byte prompt
  containing quotes, `$HOME`, backticks and 699 newlines arrives at claude as
  one argument, byte for byte, from a 301-byte launch command.
- Also driven against the real binary: a feature whose branch was checked out
  in the repo itself. The row stays and carries git's own "already checked out
  at …"; the event log has the ask and the reason; the dispatches directory is
  empty, which is exactly the state that used to be invisible.
- The audit is the diagnostic for the failures still to come. The two worktrees
  sitting on renamed branches are real and will refuse a re-dispatch; that is
  now a sentence on the screen and a line in the log rather than a vanishing
  row, and whether to recover such a worktree automatically is a separate
  question this does not answer.
- `state/prompts/` accumulates one small file per dispatch, alongside the
  record it belongs to. Nothing prunes it, for the same reason nothing prunes
  the records.
- Windows moves from `cmd.exe` to PowerShell for the launch line. It lifts the
  8191-character cap and the newline flattening, but if `claude` is installed
  there as a `.cmd` shim the argument still transits cmd on its way in; that
  backend remains a preview and is untested here.
