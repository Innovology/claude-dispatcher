# 13. The record is the truth; the screen must not wait for it

Date: 2026-09-17

## Status

Accepted

## Context

One sentence: "dismiss should move it to history".

The event log dates it. At 14:20:33 the human pressed `x` on a finished
dispatcher and the dismissal was written correctly — `dismissed_at` is on the
record, the only one in a store of 212. At 14:21:09, thirty-six seconds later,
they dispatched this complaint. Nothing about the record was wrong. What was
wrong was that for the whole of that window the cockpit went on showing them
the opposite.

### A dismissal is written to disk, and the screen waits for a snapshot

ADR 0012 made the dismissal an annotation on the record and said why: kept in
the model it would come back on the next load, and come back for every finished
dispatcher the moment the cockpit restarted, which is the whole fleet. That is
still right, and this decision does not touch it.

But the record is not the screen. `dismissCmd` writes the record and returns an
`actionMsg`, whose only effect on the table is `requestLoad(loadPlain)` — and
a load is not quick. It reads every dispatch record, resolves every repo, asks
the forge, runs a numstat per in-flight branch: ADR 0007 measured 4.5s warm and
61s cold on a 94-record portfolio, and loads are deliberately serialised one at
a time, so a dismissal pressed while one is out waits for that load and then for
its own.

Reproduced against the real binary on a copy of the reporting store (212
records, no live sessions, so the cheap end of the range): the dismissed row
stayed under the "finished" divider for ten to fifteen seconds after the key.

Every screen the human can reach in that window says the dismissal did not
happen:

- the row is still on the triage table, under the divider it was just taken
  off, still offering `x dismiss`;
- the headline still counts it — "N finished · x clears";
- and `h` does not have it. History is built from the same stale fleet, so the
  one place the flash names by hand — "dismissed X · h for history" — is the
  one place it provably is not. The human is told where it went, goes there,
  and it is not there.

The key pressed immediately after `x` is also swallowed (`cqFlash` eats the
keyboard for 850ms), so the first `h` typically does nothing at all. That is
deliberate — a flash is a promise something happened, and nothing gets through
it — and it is left alone here. It only decided which of the two screens the
human was looking at when they wrote the sentence.

### An ending you asked for came back as an unread ending

The same window has a second half, and 0012 created it.

The triage table now holds a finished dispatcher until `x` takes it off, and
holding is right for an ending nobody watched: the session stopped on its own,
the tracker flipped a merged PR to deployed overnight. It is wrong for an
ending the human caused. `x` on a running row (kill), `y` on a review row
(approve merge) and `y` on a finished turn (mark shipped) all end a dispatcher
by the human's own hand, with a confirmation on the footer saying so — and all
three stamped `FinishedAt` without `DismissedAt`, so the dispatcher was *held*.

In-session that is invisible, because those three acts are non-keep: the row is
suppressed the moment the flash ends. On the next cockpit start it is not. The
dispatcher comes back as an unread ✓ row under the finished divider, asking to
be dismissed — a merge the human approved hours earlier, reported to them as
news.

### Suppression was waiting for something that no longer happens

`cqSuppressed` hides a row between an act and the record catching up with it,
and `cqReconcile` retired an entry when its id *left the fleet*. That was true
when a finished record was dropped from the collector's rows. Since 0012 it
never happens: a finished dispatcher keeps a row for good — held, then history.

So a killed dispatcher stayed suppressed for the rest of the session, and
`fleetPast` applies the suppressed set too: it was missing from the triage table
and from history at the same time. The only thing that cleared it was
restarting the cockpit — which is exactly when the unread ✓ row appeared.

## Decision

**The record decides; the screen agrees now.**

`fleetNow` applies this session's own dismissals over the collector's rows, and
it is the only thing `fleetAll` and `fleetPast` read. A held row the human has
dismissed reads as history at once: kind `past`, re-ranked so it takes its
place in history's own order rather than wherever it sat on the live table, and
without the `x` that has just been pressed on it. Every other field still comes
from the record — the map holds what the human did, never what a dispatcher is.

Three rules keep it honest:

- **It is set on the way back from the write, never on the way in.**
  `dismissCmd` returns a `dismissedMsg` carrying the id it wrote for; a
  dismissal that found no record is an ordinary `actionMsg` and nothing moves.
  A row moved for a write that then failed would be the screen making a promise
  of its own, which is the thing this cockpit refuses everywhere else.
- **The snapshot that reads it back retires it.** A stale load still saying
  "held" leaves the entry alone — that is ADR 0007's rule, applied to an act
  instead of to a placeholder. A load that says `past` has made it true, and a
  load that says the row is alive again (a resume clears the ending, see
  `state.Save`) retires it too, because a stale dismissal must never hide a
  running dispatcher.
- **The load still runs.** This is the screen catching up with the record, not
  a second source of truth: the reload `actionMsg` would have asked for is
  still asked for.

**An ending the human asked for is already read.** `killCmd`, `shipCmd` and
`markDoneCmd` dismiss the record as they stop it, so the dispatcher goes to
history instead of coming back unread. The hold is for endings nobody watched —
`hookcmd`'s `SessionEnd`, the tracker's deploy flip, the ghost sweep, a launch
that would not start — and every one of those still holds.

**Suppression is retired by the record catching up, not by the row leaving.**
`cqReconcile` keeps an entry only while the row is still live (`queue`, `run`,
`parked`); any finished kind means the act has landed, so the row belongs in
history and is shown there. `cqUndo` goes with it: undo un-hides a row, so
there is nothing for it to do once the row is not hidden.

## Consequences

- Dismissing moves the row within a second of the key, on any size of
  portfolio, and `h` has it immediately. Verified against the real binary: row
  gone in under a second, in history two seconds after the key, and the
  snapshot that lands fifteen seconds later changes nothing.
- A dispatcher you kill or merge yourself never reappears. It is in history
  from the moment the act lands, findable in the same session — which it was
  not before, at all.
- The finished group only ever holds endings the human has not seen. That is
  what makes its count worth reading.
- The cursor stays at its index when a row is dismissed, so the next finished
  row comes up under it — the same behaviour parking has. Clearing the last one
  brings a live row under the cursor, where `x` means kill; the footer changes
  with it, and the flash blocks the 850ms after every act.
- The model now carries two sets keyed on "until the record catches up"
  (`cqSuppressed`, `fleetDismissed`). Both are retired by the same snapshot
  pass and bounded by it.
- Nothing here migrates: a record dismissed by an older build is already in
  history, and one killed by an older build is still held, so it will appear
  once, be dismissed once, and be over.
