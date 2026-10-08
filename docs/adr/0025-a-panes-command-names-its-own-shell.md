# A pane's command names its own shell

## Status

Accepted (2026-09-18, extended 2026-10-07)

## Context

Every dispatch on the reporting machine sat at "starting session" and never
started. The row never changed, nothing appeared in the event log after the
launch, and the obvious suspect was the slowest thing in the launch path:

> tmux is taking over a minute to start

It was not slow. It was already dead. The launch had reported success, and the
session it reported was gone within milliseconds.

`tmux.NewSession` passed the launch line to tmux as a **single argument**:

```go
s.clientIn(dir, env, "new-session", "-d", "-s", name, "-c", dir, shellCommand)
```

Given one argument, tmux runs it through its `default-shell`. That option
belongs to the human, and this machine's `tmux.conf` sets it to
[nushell](https://www.nushell.sh):

```
run-shell 'nu_path="$(command -v nu …)"; … tmux set-option -g default-shell "$sh" …'
```

The launch line is POSIX:

```sh
CLAUDE_DISPATCHER_ID=… claude --permission-mode auto "$(cat …)"; exec ${SHELL:-/bin/sh}
```

nu does not parse that:

```
Error: nu::parser::parse_mismatch
 1 | CLAUDE_DISPATCHER_ID=x true "$(cat /etc/hostname)"; exec ${SHELL:-/bin/sh}
   :                             ^^^^^^^^^^^|^^^^^^^^^^
   :                                        `-- expected operator
```

So the pane died at once, the server exited with its last session, and
`new-session` had **already returned 0** — the launch was reported as a
success. Reproduced on tmux 3.7b: the session is gone inside a second, with
and without `nix develop` in front of it, and with a config-free tmux whose
only setting is that one.

Two things made it unreadable from the cockpit. The failure looked like an
environment problem, because `nix develop` was the visible novelty in that
launch path and the record said "launching" the whole time. And a `launching`
record is never swept on absence (ADR 0004): no hook has fired for one, so its
session may not exist *yet*, and absence proves nothing.

### What the fix left open

Bypassing `default-shell` for the launch line is correct and must stay. But the
line ends by dropping to a shell, so the pane stays open for inspection, and
that shell was `${SHELL:-/bin/sh}` — the login shell. On the reporting machine
`$SHELL` is bash while every tmux pane is nu, so after a dispatcher finished,
jumping in put the human in a shell they do not use. The setting they had
deliberately made was ignored in exactly the panes this tool creates.

Reading `default-shell` out of `tmux.conf` ourselves is not an option: this
machine's value is computed by a `run-shell` job at config load, so the file
contains a shell loop and no path. Only a running server can answer, and at the
moment the launch line is built, the repo's server does not exist yet.

## Decision

**The launch line names the shell that parses it, and nothing else decides.**
`NewSession` passes `/bin/sh -c <line>` as separate arguments, which tmux (3.0
and later) execs directly. `default-shell` is not consulted for it, ever.

**The pane's exit shell is layered, first answer that works.** The tail of the
launch line is:

```sh
; cdsh='<configured>'
; [ -n "$cdsh" ] || cdsh=$(tmux display -p '#{default-shell}' 2>/dev/null)
; [ -x "$cdsh" ] || cdsh=${SHELL:-/bin/sh}
; exec "$cdsh"
```

1. `shell` in `config.toml` — the human saying it outright.
2. the pane's **own** server, asked from **inside the pane** at the moment
   claude exits. By then the server exists and its config has run, which is the
   only time and place the computed value can be read.
3. `$SHELL`, then `/bin/sh`.

`-x` rather than `-n` on the last test, so a shell named in config that is not
installed falls through instead of killing the pane the instant claude exits.

The chosen shell goes on the record (`PaneShell`), beside `TmuxSocket` and
`EnvCommand`, so a resumed dispatcher comes back the way it went out rather
than the way config has been edited since.

**A shell we chose is a shell the idle check knows.** `SessionIdle` decides
whether claude has ended by asking whether the pane is a shell with nothing
under it, against a list of names. An unrecognised pane reads as busy for ever:
the session is never reclaimed, `Resume` will not reopen it, and a second
dispatch of that feature is refused. A list cannot be complete — a shell can be
a wrapper script called anything — so the names are learned as well as listed:
the configured shell at startup, and a server's `default-shell` the first time
one of its panes shows a command we cannot place. That probe runs at most once
per server per run, and only when a pane is unrecognised, because
`default-shell` is a global option that does not move and polling it per sweep
would break the "never poll below the poll" rule for an answer that cannot
change.

## Consequences

- A dispatch pane runs its launch under `/bin/sh` whatever the human's shell
  is, and lands in **their** shell when claude exits.
- `shell` is a Unix setting. On Windows a console window has no shell to drop
  to, it has `pause`; `launchCommand` there takes the value and ignores it
  rather than inventing a meaning for it.
- An exotic shell no longer strands a session as permanently busy.
- Tests: a tmux server whose `default-shell` refuses every command still gets a
  live session (the fix, failing on the old code); a pane parked at a shell
  under an unknown name reads as idle; and end to end on the test machine, a
  finished pane sits at the server's own `default-shell` and reads idle.

## Notes

The original report named the wrong cause, and so did the first hour of
looking at it. What identified it was the tmux server's own log:

```
cmdq_fire_command <global>: (3104) set-option -g default-shell /etc/profiles/per-user/_liminor/bin/nu
```
