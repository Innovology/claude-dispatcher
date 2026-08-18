# A fan-out is hook truth, reported like ci, swept with the turn

## Status

Accepted (2026-08-18)

## Context

The dispatch form's FAN OUT switch (ADR 0002) invites a session to spread
across subagents, and until now that invitation was the last the cockpit
heard of it. A dispatcher running a twelve-agent sweep and one grinding
through a single file showed the identical row; the only way to know a
fan-out was happening was to jump into the session and watch. The ask was
explicit: if subagents are spun out, the cockpit should see them and report
on them against the dispatcher's headline, the way it reports ci.

Two sources could answer "what did the session fan out". The transcript
JSONL records subagent activity, but transcript parsing is best-effort
preview only in this product — the format is Claude Code's internal
business, and a count read from it would be a guess wearing a number. The
other source is the same one every status fact already rides: Claude Code's
global hooks. `SubagentStart` and `SubagentStop` fire per agent and carry
`agent_id` and `agent_type` (verified against the installed claude,
v2.1.220, before building on it); they inherit `CLAUDE_DISPATCHER_ID`, so
the existing join from event to record works unchanged.

The hooks name starts and stops, which leaves the question every
event-sourced counter faces: what happens to an entry whose stop never
arrives — a session killed mid-fan-out, a hook that lost the race with a
dying tmux. A count that can only go up is a ghost in miniature (ADR 0004):
a row claiming "3 live" forever, with nothing behind it.

## Decision

**Subagents are recorded from the hooks, as an annotation — never a
status.** `SubagentStart`/`SubagentStop` append to and settle a
`Subagents` list on the record (id, type, started, stopped). Like parking
(ADR 0001), the fan-out changes nothing about whether the session is
working or waiting: a `SubagentStart` arriving on a blocked record leaves
it blocked. Status stays the hooks' state machine's; the fan-out is a fact
carried alongside it.

**The turn is the story, and the turn's end is the sweep.** A new session
clears the list — no subagent survives the session that spun it out. A new
prompt drops the finished entries and keeps the live ones, so each turn
reports its own fan-out while a background agent may cross the boundary. A
`Stop` with no background tasks marks every still-live entry stopped: the
turn is over and nothing is in flight, so an entry still claiming to run is
a missed stop event, not a running agent. With background tasks in the
payload the sweep does not run, because those legitimately outlive the
turn. The list is capped (256); at the cap the oldest stopped entry makes
room, so the live picture stays exact and only deep history is shed.

**Reported like ci, in the same places.** The running row's SIGNAL cell
says `fan-out · 3 live` beside `ci · 2 of 5 green` — a count the hooks
measured, no louder than the checks clause it sits with. The detail panel's
meta line counts the turn (`3 subagents live, 9 done`; `fanned out 12
subagents` once they are home) and one further line names the types
(`Explore ×2, Plan live · code-reviewer done`) — seeing them, where the
clause only counts them. The FAN OUT switch itself finally surfaces too, as
a `fan-out` clause beside the mode: the invitation is config, the counts
are measurement, and the panel keeps them distinct.

## Consequences

- `claude-dispatcher init` installs two more hook entries; existing
  installs must re-run it to start seeing fan-outs. A claude too old to
  know the event names never fires them, and the row simply says nothing —
  absence of the annotation is not a claim that no fan-out happened.
- Every subagent event rewrites the record, which the cockpit's fsnotify
  reload turns into a snapshot rebuild. The gh cache discipline (ask a repo
  once, never poll below the poll) already exists to make rebuilds cheap;
  the cap bounds the record's size.
- History keeps the last turn's fan-out: an exited dispatcher's row can say
  `fanned out 12 subagents`, read straight off the record at map-lookup
  cost, so `fleetPastRow` stays the cheapest row there is.
- Workflow-tool agents fire the same hooks, so ultracode runs report
  without any extra plumbing. Background tasks remain a separate fact
  (`WaitingOnTasks`), untouched by this.
