# A dispatch runs in its repo's environment, not the cockpit's

## Status

Accepted (2026-09-11)

## Context

Reported as a question rather than a bug: two repos in one product need
different versions of the same binary — what does the dispatcher do about it?

Nothing, was the answer. The isolation this product had built was **git**
isolation. `ensureWorktree` gives every dispatch its own checkout under
`~/.local/state/claude-dispatcher/worktrees/<repo>/<slug>`, which stops two
sessions fighting over one working copy and stops a commit landing on the wrong
branch. It says nothing about what `go`, `node` or `psql` resolve to inside it.

The launch was a fixed string with nothing between it and the machine:

```go
exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, shellCommand)
```

No `-L`, no `-e`, no `cmd.Env`, and no per-repo hook anywhere in `internal/config`.

### What actually decides a session's PATH

Measured on tmux 3.7b, because the answer is not what the client/server split
suggests. A server was started with one PATH and a **later** client, on that
same already-running server, was asked to create a session:

```
server started by a client with  PATH=/plain-server-path
new session asked for by a client with  PATH=/nix/store/FAKE-flake-bin
  → that session's pane:  PATH=/nix/store/FAKE-flake-bin
  → the first session's pane:  PATH=/plain-server-path
```

**A pane inherits the environment of the client that asked for it** — not the
server's, and not the environment the server was born in.

PATH is also the one variable that does not stick to the session. Everything
else captured at creation persists; PATH is re-taken from whichever client
spawns each new window:

```
session created by a client with  NIXMARK=flake  PATH=/nix/store/FAKE-flake-bin
new-window asked for later by a plain client:
  window 1 (original):  NIXMARK=flake  PATH=/nix/store/FAKE-flake-bin
  window 2 (the new one):  NIXMARK=flake  PATH=/plain-path
```

So the cockpit — one process, spanning every repo in the portfolio — was
deciding what every dispatcher could see, and would go on deciding it for every
pane the human opened after jumping in. A Go service, a Node app and a flake
repo all launched into whatever shell the cockpit itself had been started from.

### Why not the obvious fix

The obvious wrapping is the session's command:

```
tmux new-session … 'nix develop --command sh -c "claude …; exec $SHELL"'
```

That breaks `tmux.SessionIdle`. It reads `#{pane_current_command}` and only
falls through to the process-table probe when that names a **shell**
(`shellCommands`). With `nix` as the pane's foreground process the pane is taken
at its word as busy — for ever, because the launcher never exits. `Resume` would
then return `ResumeLive` and attach the human to a dead session's wrapper
instead of killing it and resuming, and `liveDispatch` would refuse to
re-dispatch any name whose record was still in an unfinished status.

That is ADR 0008 with the sign flipped: there, every live session read as idle;
here, every finished one would read as live.

## Decision

**A repository brings its own environment, and the prefix wraps the client.**

```
nix develop --no-write-lock-file --command  tmux -L player-app  new-session -d …
└──────────────── Repo.Env ───────────────┘      └─ Repo.Socket ─┘
```

Two facts on `repos.Repo`, both resolved rather than typed:

- **`Socket`** — the `-L` name, defaulting to the repo's own name, which is the
  name git knows it by rather than its folder (ADR 0012). Every repo is its own
  server with nobody configuring anything. `[sockets]` names one for a human who
  already keeps per-project servers: a socket name is chosen *outside* the
  repository, so no amount of reading the repo finds it.
- **`Env`** — the command the tmux client runs under. A checkout with a
  `flake.nix`, **on a machine with nix**, is launched under `nix develop`. The
  second half is not optional: plenty of repos carry a flake as an optional
  convenience, and on a host without nix, launching under one turns every
  dispatch into a launch that dies. Same rule the mode and model flags follow —
  what we cannot vouch for, we do not pass.

Wrapping the client rather than the command is what keeps the pane running the
plain `<shell> -c "… claude …"` that `SessionIdle` depends on. Both spellings
put the repo's binaries on PATH; only this one leaves the idle probe true.

The prefix is applied to exactly two calls — `NewSession` and `AttachCmd`, the
two that spawn a pane. On `tmux.Server` it would put a `nix develop` evaluation
behind every `has-session` and `list-sessions`, which the cockpit issues on
every poll.

**The name alone stops being an address.** `disp-login` on two sockets is two
sessions, and asking the wrong server about one gets a confident "no such
session". `supervisor.Session` carries the pair, and it goes on the record
(`TmuxSocket`, `EnvCommand`) beside `Mode`, `Model` and `Root`. Resume reopens
from the record rather than resolving again: config changes, and a session put
back on a different server is a different session.

**`--no-write-lock-file`**, because a repo with a flake and no `flake.lock`
would otherwise have one written by whichever dispatch started first — into that
dispatch's worktree, where it lands in the feature's diff and in the provenance
every effort and shipping figure is read from. A dispatch may read a repo's
inputs; it does not get to decide them.

**The socket is not the grouping.** A product is a display lens over records,
and every record already carries its product — so a product's sessions list
across as many servers as its repos have. Sockets do the grouping work in
exactly one place: sessions with no record, the ones a human started by hand,
where socket → repo → product is the only chain there is.

## Consequences

- **`AttachSwitches` changes meaning**, from "am I inside tmux" to "is this
  session on *my* server". `switch-client` cannot cross servers, so a dispatch
  on another socket is a nested attach — which is the honest answer anyway: that
  attach runs in the cockpit's own pane and exits when the human detaches, which
  is the shape `attachReturnedMsg` already knows how to wait for. The focus-event
  path stays for the same-server switch and for Windows.
- **The sweep asks each server once** instead of the fleet once — bounded by
  repos, not by dispatches. The rule it exists for is untouched: a record its
  server's listing does not name is still probed directly, because a listing
  that failed comes back empty and an empty listing must never retire a fleet
  (ADR 0004). Verified that the probe is safe per-socket: `tmux -S <dead>
  has-session` and `list-sessions` both error out and do **not** start a server;
  only `new-session` does.
- **Every repo is its own server by default**, so `tmux ls` no longer lists
  dispatch sessions. That is a real change for anyone who reached them that way;
  the cockpit is the interface, and `[sockets]` can put a repo back on the
  default server by name.
- **A cold `nix develop` is minutes.** It runs off the UI goroutine and the
  pending row (ADR 0009) is exactly what covers it, so "starting session" simply
  lingers where it used to flash.
- **A flake with no devShell fails the launch.** `nix develop` refuses to open
  it, in the client, before any session exists — so `newSession` errors and
  `failLaunch` keeps the record carrying nix's own words, which is ADR 0009's
  contract working. `[session_env]` set to `""` turns the sniff off for that
  repo. A silent fallback to a bare launch was rejected: a dispatcher that
  quietly cannot see its toolchain is the failure this ADR is about.
- **Windows accepts both and ignores both.** The console backend has one session
  manager rather than a server per socket, and it starts the session process
  itself, so there is no client in between for a prefix to wrap.
