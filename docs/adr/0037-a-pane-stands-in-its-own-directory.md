# A pane stands in its own directory, and a server stands in home

## Status

Accepted (2026-11-26)

## Context

A design-system dispatcher handed its work on to player-app. The new session
opened on this:

```
shell-init: error retrieving current directory: getcwd: cannot access parent directories: No such file or directory
The current directory no longer exists (it was deleted or moved). Start Claude Code from an existing directory.
Error: nu::shell::error
  × $env.PWD points to a non-existent directory
   ╰── $env.PWD = …/player-app/travelling-team-reminder
```

`travelling-team-reminder` was the worktree of a different dispatch, which had
finished over an hour earlier and been removed. The new dispatch's own worktree,
`adopt-ds-3-21-2`, had been cut correctly. The launch was reported as a success,
because tmux had accepted the command. Re-dispatching it, and relaunching that,
came up the same way.

The chain:

1. **A tmux server stands for its whole life where the client that started it
   stood.** With an environment prefix, `clientIn` runs the client from the
   dispatch's worktree, so that `nix develop` finds the flake there. The first
   dispatch on player-app's server (13:06:53) therefore started the server
   inside `travelling-team-reminder`, and the server copied that `PWD` into its
   global environment.
2. **That dispatch finished, and its worktree was removed.** The server's
   working directory was now `(deleted)`.
3. **A server whose working directory is gone ignores `-c`.** Measured on
   tmux 3.7b, on a private server started in a folder that was then deleted:
   every pane it opened started in the dead folder, whatever `-c` said. That
   held for `new-session` and `new-window`, for the argv form
   (`-- /bin/sh -c …`) and the one-string form. `-e PWD=<dir>`,
   `set-environment -gu PWD` and `set-environment -g PWD <dir>` changed only
   what the pane was *told*. `/proc/<pid>/cwd` still read `… (deleted)`. With
   the server's folder still in place, `-c` worked.
4. **claude will not start where it cannot `getcwd`**, so it printed its
   refusal and exited, and the launch line's exit shell (nu) took over the
   pane and complained about `$PWD` at every prompt.
5. **Nothing reported it.** `new-session` returned 0, the session listed the
   right `session_path`, and the pane was alive, so the launch sweep (which
   retires a launching record only when its session is gone) left the row at
   "starting session".

This was not a single accident. At that moment two of the five servers on the
machine stood in deleted worktrees (player-app, czech_out_auto_bot), and a
third (claude-dispatcher) stood in a branchless conversation's folder, which
goes the same way once that conversation is finished. Every pane opened on a
stranded server starts dead, the human's own splits and windows included.

## Decision

**The pane changes into its directory itself.** `tmux.NewSession` puts
`cd '<dir>' || exit; ` in front of the launch line (`standIn`). The `cd` runs
inside the pane once it exists, where the server's directory has no say. This
covers servers that are already stranded, without restarting them and the
sessions on them. `|| exit` because a directory that is not there must end the
pane rather than start claude wherever it landed. The launch sweep then retires
the record as a session that ended before claude reported in. Resume and the
steward go through the same `NewSession`.

**A server this tool starts stands in the home directory** (`starterIn`).
`new-session` is the only command here that can start a server, and its client
now stands in `$HOME` (or `/` when there is none):

- without a prefix, the client simply runs from home;
- with one, the prefix still runs from the worktree, where it finds its
  environment, and then hands over through a fixed
  `/bin/sh -c 'cd "$1" && shift && exec "$@"' sh <home> tmux …`. The client
  keeps the environment the prefix built.

tmux has no flag for where its server stands, so the client's directory is the
only lever. Changing it before the prefix runs would lose the flake that
`nix develop` looks for where it stands, which is the failure ADR 0014 exists to
prevent.

## Consequences

- A dispatch on a stranded server starts in its own worktree. The pane still
  shows bash's one-line `shell-init: error retrieving current directory`
  first, because `/bin/sh` starts in the server's folder before the `cd` runs.
- New servers no longer strand themselves when a worktree is removed. A server
  that is already stranded stays so until it is restarted. Only the human's own
  panes on it notice, and restarting it ends the sessions on it, so that is the
  human's call.
- What this does not fix is the report of success. A launch whose claude dies
  before any hook fires, while its pane lives on as a shell, still reads
  "starting session". That is its own decision (the launch-confirmation
  dispatch). So is resume refusing a conversation that never began, where a
  claude the human started by hand joined the record by cwd and never wrote a
  transcript.
- Tests, each failing on the old code: a server started in a folder that is
  then deleted still opens a session in the directory asked for; a line whose
  directory is missing runs nothing; and a server `NewSession` starts stands
  in home, with and without a prefix, while the prefix itself ran in the
  worktree (Linux, where `/proc` says where a process stands).

## Notes

The first diagnosis proposed `tmux set-environment -gu PWD` and `-e PWD=` as the
fix. Both were tested here and neither moves the pane. The working directory is
inherited from the server process, not from any environment variable.
