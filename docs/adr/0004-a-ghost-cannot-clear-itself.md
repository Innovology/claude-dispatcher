# A ghost cannot clear itself: liveness is swept on every load

## Status

Accepted (2026-08-17)

## Context

Status comes from Claude Code hooks, and they are truth right up to the
moment a session dies without getting a `SessionEnd` out: a SIGKILL, a
`tmux kill-session` from another terminal, a machine that took the tmux
server down with it. `dispatch.ReconcileSessions` exists for exactly that —
it retires a working/needs-input/blocked record whose session is gone.

It was wired into one caller: `cockpit.recheckCmd`, the reload that runs
when the human comes back from a jump-in. Nothing else swept. Not the
opening load, not the 60s poll, not the fsnotify reload.

So a record whose session had been taken away went on claiming *working* for
as long as the cockpit was open, and its triage row went on offering ⏎
attach. Attach checks the session, finds nothing, sets a notice and changes
nothing — and the one thing that would have retired the record was coming
back from an attach that could never happen. The state that made the row
un-attachable was the state only a successful attach could clear.

It was reported from WSL, and WSL is why it was reported there: the distro
shuts down when its last console closes, taking every tmux session with it,
so a fresh cockpit opened on a fleet of dispatchers that no longer existed.
On macOS a tmux server survives for weeks, so the same ghosts are rare
enough to look like a WSL bug. They are not — a suspended laptop, a reboot
or a stray `tmux kill-server` produces the identical row anywhere. The
human's reading was "once you have dispatched something you can no longer
attach"; the giveaway was that a newly dispatched one attached fine, and the
old row disappeared moments after the new one's jump-in returned — that
return was the sweep finally running.

## Decision

Session liveness is reconciled on **every** snapshot load, in the
`LIVE SESSIONS` boot stage that already asks the supervisor what is running.
The stage retires what it finds and reports it ("0 sessions live of 1 · 1
ghost retired"), and the sweep writes through the records the load then
builds its snapshot from, so the collectors see the corrected status.

The sweep reads the whole fleet from ONE listing (`supervisor.Sessions`)
rather than a probe per record, because a load happens on every poll and
every state-file change. The listing screens; it never convicts. A record
the listing does not mention is asked about directly before anything is
written, since a failed listing comes back empty and an empty listing must
never be read as "the whole fleet died" — the same reason an unreachable
supervisor sweeps nothing. That costs one subprocess per record about to be
retired, and a retired record is skipped by every pass after it.

What counts as evidence has not changed. Only the three statuses a hook
proved a session behind are swept, and `launching` is still never swept: no
hook has fired for one, so its session may simply not exist *yet*.

⏎ on a ghost that slipped through between loads no longer merely refuses. It
runs the same sweep on that one record and says where the dispatcher went —
"retired · h for history, where ⏎ resumes it" — because the human pressing
the key is how a ghost gets discovered, and a refusal leaves the same dead
key on the same lying row. The decision stays with the sweep: on a record
still `launching` it retires nothing and the notice says it is starting.

## Consequences

- `recheckCmd` still sweeps before `track.Refresh`, for that path's ordering
  — it is no longer the only way in.
- The cockpit costs one `tmux list-sessions` per load. It replaces the one
  the opening screen already ran to count live sessions: `ReconcileSessions`
  returns that count as a byproduct, so nothing asks twice.
- A dispatcher whose machine ate its session lands in history (`h`) within
  one load, where ⏎ resumes it — a real recovery, where the ghost row's ⏎
  was a key that could never work.
- Killing tmux out from under a running cockpit is now visible within a
  poll rather than never.
