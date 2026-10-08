# 20. The steward is a switch, not a session you run

Date: 2026-09-24

## Status

Accepted. Amends ADR 0019, whose last consequence ("It stays a human-started
session: nothing starts it on its own") this replaces.

## Context

ADR 0019 shipped the steward as something the human operated. `: steward`
started it and took over their terminal; `stop steward` ended it. Nothing
brought it back once its session was gone, whether by reboot, a WSL distro
shutting down with its last console, or claude exiting inside it. The first
sign of that would have been the notes quietly stopping.

The human's reply was: "should the steward not be completely opaque to the
user? Maybe a on off toggle in triage". The steward exists to take work off
the human. A steward they have to start, watch and restart is more work, not
less.

## Decision

**One key on the triage table.** `t` switches the steward on or off in the
background; nothing takes over the terminal. The headline says `steward on`
(or `steward starting` in the gap before the poll brings it back), and says
nothing while it is off, so a cockpit that never wanted one is never nagged.
The footer names the key by what it will do: `t start steward` or
`t stop steward`. On an empty fleet, triage is the dispatch form, whose first
field is a type-to-filter repo list, so `t` there types. There is nothing to
steward, and the palette still works.

**"On" is recorded intent, not the existence of a session.** The switch is a
file, `steward/enabled`, in the state directory. It belongs there rather than
in `config.toml` because it is a fact about this machine's fleet, and so a
scratch store (`CLAUDE_DISPATCHER_STATE`) can never switch on a steward for
the real one. `Start` sets it and `Stop` clears it, clearing *before* killing
so a poll landing between the two cannot bring it straight back. The CLI's
`steward` and `steward stop` flip the same switch.

**The cockpit makes the fact follow the intent.** `steward.Ensure` runs with
the tracker and the API-error retry, at startup and on every poll. A
switched-on steward whose session is gone is started again. One whose claude
has exited is replaced, since `SessionIdle` proves the pane is back at its
shell. An unknown answer from `SessionIdle` leaves it alone, because replacing
a steward that is mid-thought is worse than one that is a minute late. Like
auto-done and the retry, this happens only while a cockpit is open.

**Its session is for reading, not operating.** What the steward does already
shows where the human looks: `steward · yours: …` and `answered · …` on their
rows. The palette's `steward` still jumps into its session, for its running
account of the fleet, and starts it first if it is off.

## Consequences

- Turning the steward on is one keystroke, and it stays on across reboots for
  as long as a cockpit is running.
- A claude that exits at once every time (a broken login, say) would be
  restarted once a poll, every minute, until the switch is turned off. Each
  restart is cheap, but it will show as a steward that never writes a note. If
  that turns up in practice, the answer is a failure count on the switch.
