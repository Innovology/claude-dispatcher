# 28. A project's colour is typed in, because there is no flag for it

Date: 2026-11-09

## Status

Accepted.

## Context

Asked for as "allow projects to set color '/color' of the claude session". A
human jumping between a dozen dispatchers' sessions sees the same grey prompt
bar in every one; Claude Code's `/color` colours it, per session, by hand.

Measured on claude 2.1.281 before anything was built on it:

- `/color` takes `red|blue|green|yellow|purple|orange|pink|cyan|default`, and
  writes `{"type":"agent-color","agentColor":…}` into the transcript, which
  `--resume` restores. A resumed session, and one whose transcript was copied
  to another account (ADR 0027), keeps its colour without being told again.
- There is no launch flag. `--agent-color` exists, hidden, described as
  "Teammate UI color"; a session started with `--agent-color red` kept the
  default grey bar (`38;5;244`).
- `/color` is an immediate command: typed while a turn was running a bash
  `sleep`, it applied at once and the turn carried on undisturbed.

## Decision

**`[colors]` names a colour per repo or per product**, a repo's own entry
winning, resolved onto the repo like `[root_branches]` and onto the record at
launch (`Color`). A value `/color` would refuse is no colour, never a session
told something it answers with an error.

**It is typed in, as soon as the session has reported in.** That is the record
carrying a session id — `SessionStart`, the REPL up — so the keys reach claude
and not a startup dialog. Because `/color` is immediate there is no reason to
wait for the turn to end the way the adopt brief does (ADR 0026); a first turn
can run an hour. The cockpit checks the records each load already holds, so
the colour goes in on the load the `SessionStart` write triggers — seconds
after the launch, verified against a real dispatch — and the poll delivers
anything a load missed.

**The usual rules for typing into a pane.** Never on a menu (typed keys pick
its options), never where claude has exited or the pane cannot be seen into
(the line would land in a shell), never into a finished session, never into an
adopted one (it had its own look before we came). Claimed under the hook lock
against a fresh read and stamped (`ColorAt`) before the keys go, so it is sent
once however many loads race.

## Consequences

- Colour is set once per record. Changing `[colors]` later affects new
  dispatches, not ones already coloured; `/color` in the session changes one
  by hand.
- Delivery needs a cockpit open, like the adopt brief and the retries.
- A hook binary older than the cockpit's drops fields it does not know when it
  saves a record — measured here, a v3.13.0 hook stripping `tmux_socket`
  under a newer cockpit. Keep the two the same build (`init` embeds the path;
  the upgrade replaces it).
