# 12. A dispatch that ends is not a dispatch that goes away

Date: 2026-09-16

## Status

Accepted

## Context

Two reports, in one sentence each: "the history needs to be ordered by last
access/last action", and "the triage should hold dispatchments until they are
dismissed — they should not auto-dismiss".

They are the same complaint from both ends. The triage table takes rows off by
itself, and the list they land in is ordered by something that is not about
them.

### The table dismissed them on the human's behalf

`collectFleet` sorts every record into one of four rows. `blocked`, `needs` and
`review` are queue rows; `working` is a run row; everything else — `live`
(shipped) and the empty state (`exited` with no merged PR) — falls through to
the history row. History is not in `fleetAll`, so the moment a dispatcher's
last hook fired, its row left the triage table on the next poll.

From the chair that is a row vanishing. The human is watching one screen; a
dispatcher finishes; the count goes from 4 to 3 and a line disappears. Nothing
says which one, nothing says how it ended, and the one screen they watch has
just quietly disposed of the only notice they were ever going to get. Finding
out costs `h`, a scan of a list that may be hundreds long, and knowing to look.

This is the same defect as the failed launch in ADR 0009 — "the entire account
was one footer notice the next keypress replaces" — with the failure moved to
the other end of the run. A launch that does not happen and a dispatch that
ends are both things that happened, and both were being reported by the row
going away.

### History was ordered by when we last wrote the file

`fleetMoved` takes the freshest of the transcript's mtime and the record's
`UpdatedAt`, and history sorted on it. The `max` is right for a live row and
wrong for a finished one, for a reason that is entirely ours: `state.Save`
stamps `UpdatedAt = time.Now()` on every write, and a finished record goes on
being written for the rest of its life by things that are not the dispatcher
doing anything —

- `dispatch.ReconcileSessions` sweeping a ghost, days after the session died;
- `track.Refresh` catching a merge or a deploy on a branch nobody has touched
  since;
- any PR field coming back different from the forge.

Measured on the reporting store (204 records):

| feature | `UpdatedAt` | transcript mtime |
|---|---|---|
| search updates | 09-15 11:09 | **09-11 15:01** |
| gleap | 09-14 10:26 | **09-07 08:40** |
| JWT validation | 09-10 07:57 | **09-07 08:40** |

and five separate dispatchers — `teamtoday integration`, `script from pr`,
`review this PR`, `audio security`, `remove boxes` — sharing `09-12 09:54:0x`
to the second, because one sweep had written them in a loop. Their real work
ended on four different days. Ordered by that stamp, history is a list of what
we last touched: a dispatcher whose transcript had not moved in a week sat
above the one worked an hour ago, and a block of them sat in whatever order a
loop happened to run.

Both reports are about the same list. "Last access" is the giveaway: history
exists so a dispatcher can be resumed, so the one you were last in belongs at
the top.

## Decision

**A dispatcher that finishes keeps its row until the human dismisses it.**

It is collected as a `done` row — its own rank (3), its own group under a
`finished` divider, glyph `✓`, below everything still going and above the
shelf. It carries the same SIGNAL a history row carries (`deployed`, `merged`,
`marked shipped`, `stopped`) and offers the same acts: `⏎` resumes, `o` opens
the PR. `x` **dismisses** it — a dismissal, never a kill, because the session
has already ended and there is nothing left to kill. The headline says
`N finished · x clears`, and the count is its own: folding it into "running
clean" would hide a finished dispatcher inside the number that means everything
is getting on with it.

Nothing else takes the row off. No snapshot retires it, the way no snapshot
retires a failed launch's note (ADR 0009): what it reports is that the work is
over, and a later load finding the work still over is not new information.

**The dismissal is written to the record** — `DismissedAt`, an annotation like
parking, never a `Status` (ADR 0001). The table is rebuilt from disk on every
poll and every fsnotify event, so a dismissal the model only remembered would
come back on the next load, and come back for *every* finished dispatcher the
moment the cockpit restarted, which on the reporting machine is about 170 rows.

**What is held is `Held()`: finished, stamped with `FinishedAt`, not
dismissed.** `FinishedAt` is set by `state.Stop` at the transition into a
finished status and nowhere else — the `SessionEnd` hook, the ghost sweep, the
failed-launch record, the cockpit's kill and its two ship keys, the tracker's
deploy flip. A record already finished keeps the stamp it has: the tracker
rewriting a merged PR's fields months later is not a second ending.

That stamp is also the whole of the migration. **A record that ended before
this build existed has no `FinishedAt`, so it was never held and is history
from its first load.** No backfill, no marker file, no rewriting 170 records to
say something they already imply — and no possibility of the day this ships
putting a year of finished dispatchers onto the fleet at once. The failure mode
of a finishing site we missed is the old behaviour, not a flood.

`Save` clears both fields whenever the record's status is not a finished one.
That is an invariant rather than a guess — a working record has not finished —
and it is what makes a resumed dispatcher come back clean, from whichever path
resumed it.

**Finished rows are ordered by when the dispatcher was last worked**
(`fleetActed`): the transcript's own mtime, which only the session writes,
falling back to `UpdatedAt` only when there is no transcript to read — a launch
that died before claude opened one. It is the LAST column on those rows as well
as their sort key, because a column reading `1h` on a row sorted as a week old
is the same lie twice. The product panel's `H` tab reads the same clock.

`fleetMoved` is unchanged for live rows and keeps its `max`. A working
dispatcher's hook save is real liveness, and the transcript is the only honest
signal for the ten minutes between hooks; the two agree for a blocked or
turn-done one. The bookkeeping problem is specific to records that have
stopped, which is the only place the rule changes.

## Consequences

- The dispatch form no longer opens by itself while finished dispatchers are
  held, because `fleetAll` is not empty. `d` still opens it. This is the right
  way round: the form appearing over an unread ending is how the ending got
  lost in the first place.
- `fleetBody` draws two group dividers now instead of one. They are display
  lines the cursor cannot land on, so the selection maps across however many
  precede it rather than across a single known index.
- A held row sits out the `s` rotation, for the same reason a parked one does:
  an id ordered while it was still a queue row must not drag its finished self
  back above the live table.
- Dismissing is not undoable with `ctrl+z`. The act keeps its row until the
  record comes back saying it was dismissed, and the row it becomes is one `h`
  away — where `⏎` resumes it.
- Two records' worth of new schema, both `omitempty`, both absent on every
  record written before this. Nothing reads them except `Held()`.

Verified against the real binary in a tmux pane: a finished dispatcher held
under its divider with `2 finished · x clears` on the headline, `x` writing
`dismissed_at` and moving it to history, and history ordered `10m · 1d · 7d`
where the records' own stamps would have read `10m · 1h · 1h` in the other
order.
