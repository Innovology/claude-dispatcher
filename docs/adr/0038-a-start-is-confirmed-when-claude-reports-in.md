# 38. A start is confirmed when claude reports in

Date: 2026-11-26

## Status

Accepted.

## Context

A dispatch was reported as launched for a session whose claude never came up,
and nothing afterwards said otherwise.

On 2026-11-26 a design-system dispatcher handed its work to player-app ("adopt
DS 3.21.2 on main", record `4c6d1447ab92`, handed off from `7e0e022eaff0`).
`DispatchLaunched` was logged at 14:03:23, as soon as `tmux new-session`
returned 0. claude exited at once: the pane had started in a deleted directory,
because tmux 3.7b ignores `-c` once the server's own working directory has gone
(fixed separately, ADR 0037). claude printed one line, "The current directory
no longer exists (it was deleted or moved). Start Claude Code from an existing
directory.", and exited 1. The launch line's exit shell, nu, took over the pane
and complained about `$PWD` at every prompt.

The session stayed alive, with an idle shell in it. The launch sweep
(`ReconcileSessions`) retires a launching record only when its session has
gone, so it never looked at this one again. The record sat at launching, and
its row said "starting session" for good. The session that handed the work off
had been told the launch worked, and told the human that the handoff had
worked.

Two separate things were wrong.

- **A launch counted as started when the pane existed.** `Launch` returns once
  the supervisor has accepted the session. Its success means a pane exists. It
  does not mean claude is running in that pane. The launch line ends in an exit
  shell on purpose (ADR 0025), so the pane outlives claude: a claude that dies
  at once leaves a live session behind, exactly like one that is starting. ADR
  0009 made the sweep retire a launch whose session had gone. A session that
  stays is the other half, and nothing covered it.
- **A caller repeated that success.** Whatever told someone the launch had
  worked took `Launch` returning as its evidence. That covers the handoff
  command (ADR 0034, PR #99), the CLI `dispatch` (ADR 0033, PR #89) and `swap`
  on main. A dispatcher reading "handed off" ends its turn on it. In #99 the
  handoff also ends the dispatcher's own run on it.

The only account of why claude died was its own screen. No hook fires before
`SessionStart`. The exit shell then prints its own complaints underneath, and
read from the end, the last error in that pane is nu's, not claude's.

## Decision

**A launching record whose live session has fallen to its shell is a start
that failed.** `ReconcileSessions`, which the cockpit runs on every load, asks
one more question of a launching record whose session is alive and which the
supervisor accepted at least `StartSettle` (3s) ago: are all of its panes idle
(`SessionIdle` known and idle)? A pane becomes its exit shell only once claude
has gone, so an idle pane is a claude that ended before any hook fired. Such a
record is retired to exited. Its reason is read from the pane, and the whole
screen is kept as `StartOutput`, which `l` opens. For example:

> claude exited before it reported in (status 1): The current directory no
> longer exists (it was deleted or moved). Start Claude Code from an existing
> directory.

Nobody watched it end, so the row holds (ADR 0012). It has no conversation, so
⏎ starts it again (ADR 0009), and Resume first kills the idle session it left.
The sweep leaves the session itself alone: a pane at a shell is the inspection
the exit shell exists for.

**Only an idle shell is evidence.** A busy pane is left launching. It is a
claude still starting, or one stopped on a startup prompt: player-app's next
dispatch that afternoon took eleven minutes to report in. An unknown answer
counts for nothing either way; the Windows console backend can never give
anything else. The settle covers the one moment a live launch looks idle:
before the launch line's `/bin/sh` has forked anything, it is a shell with
nothing under it. The write is made under the hook lock against a fresh read,
and only if the record is still the same launch. A `SessionStart` that lands
while the pane is being read wins.

**The pane says when claude exits, and with what status.** The launch line's
tail now prints `[claude-dispatcher] claude exited with status N` before it
hands the pane to the exit shell, reading `$?` while it is still claude's.
That line is the boundary the reason is read at. What comes before the last
mark is what claude printed, and it is read through `internal/cause` like any
failed command's output. Lines with no letter or digit in them, such as the
edges of a box claude drew, are dropped first. What comes after the mark is
the exit shell's, and it is kept in the file but never quoted as the cause. The
same line also tells a human who attaches that claude has gone and how. A pane
from an older build has no mark, and its last words are quoted as "its pane
last read", not as claude's.

**Whatever reports a launch to a caller waits for the verdict, and says
which.** `dispatch.AwaitStart` waits up to `StartWait` and returns one of three
outcomes:

- *confirmed*: a hook moved the record off launching, which only a running
  claude can do;
- *failed*: the record ended, because the session went, claude died at its
  shell, or it reported in and ended within the wait;
- *unconfirmed*: neither had happened when the wait ran out.

`StartWait` is `LaunchStallAfter` plus ten seconds. A claude still silent after
it is one the cockpit already shows as stopped on a startup prompt. Each look
is `ReconcileSessions` over the one record, so the caller and the cockpit reach
the same verdict the same way. A dead launch is retired with its pane's words
even when no cockpit is open. `StartReport` is the clause every caller prints,
so the commands all say the same thing:

- `swap` (main) prints `· claude reported in`. A failure becomes an error
  naming the cause. An unconfirmed start says so and names the session.
- `handoff` (#99) ends the handing-off dispatcher's run only on *confirmed*.
  On a failure, the work stays with the dispatcher that has it and the command
  fails with the cause. On *unconfirmed*, it says that it is not confirmed,
  and it does not say "handed off".
- `dispatch` (#89) prints the outcome beside the id, and fails with the cause
  on a failure.

The cockpit does not wait. It has the sweep, and its rows already show
"starting session", then the startup prompt, then the retirement.

## Alternatives considered

- **Record claude's exit from inside the pane, to a file or through a hook
  command, and treat that as the evidence.** This works with no cockpit and no
  waiting caller. It is also exact even after the human has typed into the
  exit shell. But every launch path already has a sweeper right after it: the
  cockpit's load, or the CLI's own wait. And a file is a new piece of state.
  It would have to be cleared on every resume and relaunch, or a reopened
  dispatcher would read its last run's exit as its own death, which is a new
  way to retire a live dispatcher by mistake. What a file adds over the
  marker, the exit status, the marker carries too, in the place the sweep
  already reads. So the evidence stays the idle shell the brief asked for, and
  the launch line contributes only the marker.
- **Kill the dead launch's session when retiring it.** It would tidy the
  server, but it would also destroy the one screen that shows what happened.
  Resume already reclaims an idle session of its own before relaunching.
- **Wait inside `Launch` itself.** The cockpit launches through `Launch`, and
  it must not block for up to half a minute on a key. Its sweep and its
  placeholder rows (ADR 0007) already cover that wait.

## Consequences

- A start that failed is said where the work was asked for: on the cockpit's
  row within one load, and to a CLI caller within `StartWait`, in claude's own
  words.
- A handoff or dispatch command can be trusted. "Handed off" now means claude
  is running in the new session.
- A cockpit load asks `SessionIdle` of each launching record past the settle:
  one `list-panes` and one `ps`. Launching records are few and short-lived,
  and a running fleet costs nothing more.
- `swap` takes a few seconds longer: the time claude takes to start.
- The marker line appears in every Unix pane whose claude exits, including a
  normal `/exit`.
- How a session the human started by hand came to be joined to this record,
  through the cwd at its `SessionStart`, is a separate defect handled on its
  own. Nothing here depends on that join, either way.
