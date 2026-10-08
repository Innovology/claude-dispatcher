# A session you already trust can be adopted

## Status

Accepted (2026-10-08)

## Context

Reported by someone moving onto this tool:

> i have long-running sessions of orchestrator agents, which hold a lot of
> the context i and the ai need to move fast. if i start using the dispatcher,
> i seem to lose the access to those sessions. yes i can find them in the Y of
> the products tab — but that doesn't give me the same level of control — ie if
> there's something for me to call on in those sessions i won't see it in the
> tool.

ADR 0015 made those sessions visible: the cockpit enumerates tmux servers,
attributes each session to a repo, and lists it. Deliberately as three facts
and a key to jump in, because they are not dispatchers and must never be shown
as ones.

That holds for feature, branch and effort, which do not exist for a session
nobody dispatched. It does not hold for "this session is waiting on you",
which is a fact about a session. And the cockpit is already told it: the
lifecycle hook is installed machine-wide, so every Claude Code session on the
box reports. Measured on the reporting machine: of the last 3,000 events in
the log, 2,732 came from sessions this cockpit never started. Every one was
read, found to match no record, and dropped.

So the human was asked to choose between the tool that watches the fleet and
the sessions that know the work.

## Decision

**A session can be adopted: the cockpit wraps a record around it and nothing
else changes.** No branch is cut, no worktree added, no claude launched, no
prompt sent. The record is a join key and somewhere to keep what the hooks
say. `space` marks sessions in the tab, `A` adopts — one asks for a name,
several are taken on their defaults, because naming six sessions one at a time
is not a migration.

**The name defaults to the branch the session is on**, which on a layout of
one folder per branch is what the human already calls that work.

**The conversation is identified from the process, not the log.** The hooks
identify themselves by Claude Code session id, so adoption must know which
conversation a tmux session is running. claude holds its transcript open,
named for the session id, so the open descriptors of the claude under that
pane name it exactly. The event log can only say which conversation last
reported from a *directory*, and the human this feature is for runs two
orchestrators in one checkout. The log is the fallback for a machine with no
`/proc`, and it refuses rather than guesses when more than one conversation
has reported from the directory.

**The record admits it does not own the directory.** `AdoptedAt` is set, and
`OwnsWorktree()` is false for the life of the record. Both places that delete
a directory — the kill key's `CleanupWorktree` and the disk reclaim — ask that
question first. An adopted dispatcher runs in the human's own checkout, which
existed before the record and must outlive it.

**`x` releases rather than kills.** A session this cockpit did not start is
not its to end (ADR 0015). Release stamps `ReleasedAt`, clears the session id
so the record stops answering hooks it no longer claims, and finishes the
record as read — so the session returns to the sessions tab and the record
stays in history as the account of the time it was managed.

**Provenance starts at adoption.** `BaseSHA` is the commit the directory is on
when adopted, so every effort and shipping figure counts what the dispatcher
does from there. A session adopted mid-feature is not credited with the
feature.

## Consequences

- An orchestrator with hours of context becomes an ordinary row: status from
  its own hooks, the question it asked quoted on the row, `r` to reply, park,
  history, and the merge keys.
- Mode and model stay empty on an adopted record. They are facts about how a
  session was launched, and this one was launched by something else.
- A resumed or restarted session is a new conversation with a new id, so an
  adopted record whose session restarts outside the cockpit stops matching its
  hooks. The ghost sweep retires it; adopting again is one keypress.
- The sessions tab stays what it was for everything unadopted. Second class is
  the right class for a session nobody has asked the cockpit to manage.
