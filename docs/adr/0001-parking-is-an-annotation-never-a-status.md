# Parking is an annotation, never a status

## Status

Accepted (2026-08-17)

## Context

A session sometimes stops on a question the human has no answer for yet — legal
has not come back, the API key is not issued, a design call has not been made.
Until parking existed that ask sat in triage's "want you" counts indefinitely,
indistinguishable from asks that could actually be answered. The fix has to
move such a dispatcher to a *parked* group at the bottom of the fleet, under a
human-typed reason, until the human takes it back up.

The obvious implementation was a new `Status: parked`. But status truth in this
product belongs to the lifecycle hooks (`internal/hookcmd`): every event a
session emits — a Stop, an idle prompt, a permission prompt, a session ending —
writes the status it proves. A parked *status* would be flipped back by the
very next event, or need a guard at every one of the half-dozen sites a status
is applied (hookcmd, reconcile, track, resume, the cockpit's own kill and
mark-done). And a shelf that a lifecycle event can empty is not a shelf: the
whole point is that the human's "later" outlives whatever the machine does in
the meantime — including the machine rebooting and taking tmux, and every
parked session, down with it.

## Decision

Parking is a pair of fields on the dispatch record — `ParkedReason` (the
human's required, typed reason) and `ParkedAt` — orthogonal to `Status`. The
hooks keep reporting machine truth underneath; the fleet groups on the
annotation. A parked record lands in the parked group whatever its status says,
with one exception: "live" outranks the shelf, because shipped work has nothing
left to come back to.

The shelf clears only on the acts that genuinely end it:

- `p` on the parked row — unpark; the row rejoins the live table at whatever
  rank its real status earns.
- `x` — a kill is abandonment, not shelving; the record must not haunt a group
  whose claim is "you will come back to this".
- A prompt reaching the session (`UserPromptSubmit` in hookcmd) — someone just
  answered the question it was parked on. No other lifecycle event touches the
  pair.
- Shipping — a done record goes to history like any other.

In the fleet, parked is its own row kind at its own rank, between running and
history, under the table's one divider line, with the reason in the SIGNAL
cell. Parked rows are excluded from the "want you" / "need a look" counts and
sit out the `s`-rotation ordering, so an id skipped while it was a queue row
can never drag its parked self back above the live table. Only queue rows can
be parked: parking answers "it asked me something I cannot answer right now",
and a running dispatcher has not asked anything.

## Consequences

The status machine is untouched — no new status, no guards, no new hookcmd
transitions beyond the one deliberate clear on `UserPromptSubmit`. The shelf
survives reboots: a parked row whose session died offers resume instead of
attach, by the same mechanics history rows use. The reason is mandatory at the
input — a shelf of unexplained rows would just be a second history. The cost of
the orthogonal pair is that every screen deciding "is this row live" from
status alone will show a parked dispatcher as its underlying status (the
products lens counts it as out, collisions still name it), which is accepted:
those screens report machine truth, and the shelf is a triage-lens concept.
