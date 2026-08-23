# A shell in the pane is not an idle session

## Status

Accepted (2026-08-23)

## Context

`tmux.SessionIdle` answers one question — has the claude process in this
session ended? — and two callers act on the answer: `Resume` kills the
session when it is idle, and `liveDispatch` treats an idle session as no
longer a live dispatcher, which is what allows a second dispatch under the
same feature name.

It asked tmux for `#{pane_current_command}` and called the session idle if
that was a shell. It cannot work. A dispatch session runs
`<shell> -c "… claude …; exec ${SHELL}"`, and a non-interactive shell has no
job control, so claude never gets a process group of its own: the pane's
foreground process group leader stays the shell, and tmux answers "zsh"
whether claude is running or not.

Measured on a live dispatcher while it was working: pane_pid 51122 reporting
`zsh`, with `claude` running as pid 51136, its child.

So the probe said "idle, known" for every session this tool starts, and:

- pressing ⏎ to reopen a dispatcher whose claude was still up **killed that
  claude** and started a second one on the same transcript;
- `liveDispatch` skipped every live dispatcher, so "one live dispatch per
  feature name" refused nothing. Two records on the reporter's machine —
  `dev-1812` at 00:04:53 and `dev 1812` at 00:06:21 — share a slug, a branch
  and a worktree because of it. The second ended 26 seconds in.

## Decision

**Ask what is running under the pane's shell.** A pane tmux can name a real
command for is still taken at its word — that is the same question, already
answered, and it costs no process-table read — but a shell-looking pane is
idle only when nothing is running under it. That is exactly what
`exec ${SHELL}` leaves behind when claude exits: the same pid, now the login
shell, childless. The read is one `ps -ax -o pid=,ppid=`; a process table
that cannot be read is "unknown", never "idle". (`pgrep -P` is shorter and
not dependable — under a sandboxed process table it reports no children for
a parent that plainly has one, which is the exact wrong answer here.)

**`liveDispatch` reads the record's status as well as the session.** A
finished dispatch keeps its session, and on a real machine keeps its claude
too: the REPL sits at its prompt for days after the PR merged, so a correct
session probe alone would now refuse every feature name that had ever
completed — and history rows offer resume, not kill, so the refusal would
name no way out. The hooks say whether a dispatcher is still going
(launching, working, needs-input, blocked); the session probe catches the
record they could not correct, when a SIGKILL or a reboot left one claiming
to work.

## Consequences

- Resuming a dispatcher whose claude is still up attaches to it instead of
  killing it. A prompt typed into the resume overlay is not delivered in
  that case — sending keys into a session that may be mid-turn, or sitting
  on a permission prompt, answers the wrong question — and the notice says
  so: "say it there, it was not sent".
- Dispatching a feature name whose dispatcher is still going is refused
  again, as `CLAUDE.md` has always said it would be. Re-dispatching a
  finished one is unaffected.
- The probe costs one `ps` per session read, and only for panes that look
  idle.
