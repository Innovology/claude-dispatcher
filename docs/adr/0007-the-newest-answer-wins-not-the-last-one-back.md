# The newest answer wins, not the last one back

## Status

Accepted (2026-08-23)

## Context

Reported as: dispatch something and it disappears — most of the time —
sometimes coming back, sometimes not.

Everything on screen is drawn from package vars that `applySnapshot` swaps
wholesale, and a snapshot is built off the UI goroutine. Nothing sat between
the two. An fsnotify event on the state dir, the 60s poll, a finished action
and the return from a jump-in each fired a load of their own the moment they
arrived, however many were already out, and whichever returned last won the
screen.

A load is not quick. Measured on the reporter's own portfolio — 94 records —
it takes **4.5s warm and 61s on the first, cold pass**, and every hook event
from every session writes a record, which is an fsnotify event, which is
another load. So when a dispatch lands, a load that read the dispatch
records *before* that record existed is routinely still in flight. It
returns afterwards and publishes a fleet with no such dispatcher in it.

`pending.go` exists to cover exactly this window: the ask goes on the table
as a placeholder the instant the human presses the key, and is retired once
the record can speak for itself. Its retirement rule was meant to be "a
snapshot has since read the records". What it tested was "a snapshot has
since landed", which the stale load satisfies while knowing nothing about
the record. So the placeholder went, the record's row was not in the fleet
that had just been published, and the dispatch vanished. It came back on the
next load that happened to be fresh — or not until the poll a minute later —
and on an otherwise empty fleet the triage lens fell back to the dispatch
form, so what the human saw was the form they had just submitted, blank.

Reproduced against the real binary, driving a cockpit through a dispatch in
tmux and capturing the pane once a second: the row is there at t+1s, gone at
t+2s and t+3s (replaced by "queue clear" and an empty form), back at t+4s.
That was with an empty state dir, where a load takes about two seconds; the
window is as long as the load, so on a real portfolio it is the 5–60s one.

## Decision

**One load at a time, and a snapshot is only published if nothing newer
already has been.** Every reload request goes through `model.requestLoad`
(`internal/cockpit/load.go`), which either starts a load or remembers that
one was wanted; the landing snapshot starts whatever was asked for while it
was out. Requests collapse — ten records changing during one load is one
reload, not ten. Each load carries the number it was started with, and a
snapshot older than the one already applied is dropped rather than
published.

A load out for longer than `loadStuckAfter` (5 minutes, far past the slowest
real one) stops blocking others, because one-at-a-time is a rule about a
load that is going to land and a wedged subprocess must not freeze the
cockpit's data for the session. That is why the sequence guard is kept as
well as the queue, rather than instead of it.

**A settled placeholder is retired only by a snapshot that read the records
after the launch reported success.** The snapshot stamps that instant
(`recordsAt`, taken at `state.LoadAll`, not at the end of the load) and
`prunePending` compares it against when the launch settled. "Since" now
means since the record was written, which is what the rule always meant.

## Consequences

- The cockpit does strictly less work: a burst of hook events across a busy
  fleet is one reload rather than one per event, which also stops several
  loads racing for the same `gh` reads.
- A reload asked for during a load lands one load later than it used to.
  Where that matters — the jump-in's recheck — the request survives as a
  recheck rather than being downgraded to a plain reload (`loadNext` keeps
  the higher of the two).
- The placeholder row now genuinely lasts until the record can replace it,
  including through a launch that takes as long as a `git fetch` timeout.
- Two tests fail on the old rule and pass on the new one: the placeholder
  alone, and the whole path through `Update` with a stale snapshot landing
  on it. `load_test.go` covers the queue, the ordering guard and the
  stuck-load escape.
