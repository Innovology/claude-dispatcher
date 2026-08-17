# Amber is a claim about the human, not about CI

## Status

Accepted (2026-08-17)

## Context

The triage table had a tier between "wants you" and "running": amber ◆,
"green in ci and still not merged", for a dispatcher whose open PR had all
checks passing. It read as "this is drifting — a merge is sitting there and
nobody is doing it".

But the only rows that could earn the tier were *running* sessions. A row is
a run row because the hook state machine says the session is still working —
doing manual verification of the change it just pushed, or partway through
its next step — and a session that has not asked anyone to merge is not
being waited on. The state the tier meant to name, a green PR whose session
has stopped, is a "review" queue row by construction: hookcmd flips the
record to needs-input, floorState reads the open undeployed PR as "review",
and the dispatcher surfaces in the table's top half asking "approve a
merge". Every dispatcher truly waiting on a human was therefore already
ranked above the tier, and the tier fired exclusively on the false positive.

## Decision

An attention colour is a claim that a human is being waited on, and only the
hook state machine can make that claim — never a forge signal alone. The ◆
drifting rank is removed. A running row keeps "green, unmerged" as SIGNAL
text — the fact is worth a cell — but carries no tone, no glyph louder than
·, and no rank above other running rows. The "needs a look" filter and the
headline's "N need a look" count, which existed for that tier, go with it;
with the tier gone they answered the same question as "wants you" twice.

## Consequences

- A dispatcher doing manual verification on a green PR no longer lights the
  cockpit amber; the moment it actually stops, the same record becomes a
  review queue row and is highlighted as waiting.
- The live ranks are 0 (queue, something wrong), 1 (queue), 2 (running),
  with parked (‖) and history below; `f` cycles all → wants you → running →
  history.
- Any future escalation of a running row must cite evidence the session
  itself is stuck — a forge fact about the PR is not that evidence. The
  design's "thrash" trigger stays unimplemented for the same reason:
  gh.Checks is a point sample and cannot demonstrate a trend.
