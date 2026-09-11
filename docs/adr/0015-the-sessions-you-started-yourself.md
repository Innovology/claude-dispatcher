# The sessions you started yourself

## Status

Accepted (2026-09-11)

## Context

ADR 0014 gave every repository its own tmux server, because a pane inherits the
environment of the client that asked for it and the cockpit was that client for
the whole portfolio. It left one thing unfinished, and it is the thing that was
asked for first: the cockpit could only see sessions it had a record for.

A human who works this way has servers of their own on the same machine — one
per project, started by a shell wrapper long before any dispatcher existed.
Measured on the reporting machine, two of them:

```
server "claude-dispatcher"  claude-dispatcher__nix-pkgs  …/claude-dispatcher/nix-pkgs
server "player-app"         player-app__main             …/PlayerPulse_org/player-app/main
```

Those are real work in repos the cockpit knows about, and it could not name one
of them. Meanwhile the cockpit's own `Sessions()` only ever asked about sockets
it had recorded, so a server it had never dispatched into did not exist to it.

## Decision

**Find servers by looking where tmux puts them.** `${TMUX_TMPDIR:-/tmp}/tmux-$UID`
is tmux's own rule, and it has to be ours: the sockets we are looking for were
made by somebody else's tmux, so there is nowhere else to learn about them.

A socket **file** is not a server. It outlives the process that made it — on an
ext4 `/tmp` a reboot's leftovers sit there for months — so every name found is a
candidate and only asking settles it. Asking is safe: a dead socket makes
`list-sessions` error out, and it does **not** start a server. Only
`new-session` does that, which is what makes enumeration possible at all.

**A record claims a session by its whole address.** The same name on two servers
is two sessions, so claiming a name everywhere would hide a human's own session
because a dispatcher elsewhere happened to share it. Records of every status
claim, not just live ones: a finished dispatcher's session outlives its claude
by design and is still the dispatcher's.

**Attribution is the socket, then the path.** A server named for a repo is that
repo's — the rule the launcher itself follows, so it is exact rather than
inferred. Otherwise it is where the session is actually working, matched
against the repo's checkouts, longest match first. The second is the case that
matters: a server a human named themselves matches no repo at all, and only the
directory says whose it is. Measured — `pp_calendar_isolated` is a checkout of
`pp-calendar-sync`, and nothing but the path connects them.

Matching is by path **segment**, never by string prefix: `/src/app` must not
swallow `/src/app-2`, and both layouts on the reporting machine produce exactly
that shape — `playerpulse` has seven worktrees named `playerpulse-o6`,
`playerpulse-joins`, `playerpulse-fixture` and so on, sitting beside it.

**They are not dispatchers, and are never shown as ones.** A session started by
hand has no feature, no status, no base SHA and no effort figure. The row is the
three facts that exist — what it is called, which repo it works in, which server
it is on — plus `enter` to hand over the terminal. There is no kill key: this
cockpit did not start them and does not account for them.

A session belonging to no known repo is **dropped**. A repo in no product goes
to the `unassigned` bucket the portfolio already keeps for unassigned repos —
which is the difference between using a bucket and inventing one.

## Consequences

- **The Y tab** on the product panel, beside O/R/T/S/H. `enter` attaches,
  carrying the repo's environment, so panes opened after arriving see what that
  repo's sessions are supposed to see.
- **The boot line reports it.** `LIVE SESSIONS` already asks the supervisor what
  is running, so the enumeration belongs to that stage rather than to a new one,
  and the line now ends "· 2 sessions of your own". No new boot step, so the
  sequence still describes `loadSnapshot` exactly.
- **One `list-sessions` per server per load**, bounded by servers on the
  machine, not by dispatches. The dead sockets a machine accumulates each cost a
  failed connect; nothing prunes them, because deleting somebody's socket file
  is not ours to do.
- **Filing under `unassigned` was a correction found by running it.** The first
  version dropped a session whose repo had no product, and on the reporting
  machine — `[products]` empty, as it is on every machine's first run — that
  made the entire tab invisible with no way to tell why.
- **Windows returns no servers.** Its backend has one session manager rather
  than a server per socket, and it records a pid rather than a directory, so
  there is nothing to enumerate and nothing to attribute a session by.
