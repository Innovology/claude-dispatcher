# 19. The steward pushes what the brief settles, and leaves the rest with a note

Date: 2026-09-24

## Status

Accepted

## Context

ADR 0018 made every stop say what it needs and let the human answer from the
table. It did not take the human out of the loop, and the human asked for
exactly that: "I don't quite know if it's all going to plan. I wonder if
dispatcher should have its own claude code session that it opens to monitor,
observe, kick along, pause, and act as the lead/steward for dispatcher."

The transcripts behind ADR 0018 split the human's answers in two. Most were
reflex — `continue`, `yes`, `do it all`, `word`, `carry on` — to a dispatcher
offering the next part of the work its brief had already asked for. The rest
were judgement, and almost all of it was about something leaving the branch:
"hang on… so it doesn't meet the spec?", "do an adversarial review and then
merge it", "one deploy at a time". The first kind is a push anyone who has read
the brief could give. The second is the human's.

A push is judgement too — "is this question one the brief already answers?" —
and the cockpit never guesses. So the push cannot be a rule in the cockpit. It
has to be something that reads.

## Decision

**The steward is a Claude Code session of the dispatcher's own.**
`claude-dispatcher steward` (or `: steward` in the cockpit, which also jumps
into it) starts `claude` in auto mode in tmux session `disp_steward` — the
underscore keeps it out of the namespace every dispatcher's `disp-<slug>` lives
in, since slugs are `[a-z0-9-]` — in a folder under the dispatcher's state. Its
standing brief is that folder's `CLAUDE.md`, so it survives compaction and
restarts, and it is rewritten on every start so an upgrade changes how the
steward works. It is not a dispatcher: no record, no branch, no worktree, and
no `CLAUDE_DISPATCHER_ID`, so its own hooks are attributed to nothing and it
never appears on the table it reads.

**It can read and answer; it cannot build or ship.** The folder's project
settings allow its fleet verbs (`status`, `reply`, `note`, `park`, `unpark`, by
the binary's absolute path) and the read-only forge and git reads it gathers
facts with, so it never stops on a permission prompt nobody is watching. They
deny `Edit`, `Write` and `NotebookEdit`: a lead that writes code is a second
dispatcher nobody dispatched. The folder is marked trusted with `TrustOwnDir`,
the one place trust is written rather than inherited, and only for a folder the
dispatcher made. If that fails, the session still starts and says so, because
the trust dialog is one keypress on attach.

**It sleeps on the fleet, not on a clock.** `status --next` blocks until some
dispatcher is waiting and its current wait is unhandled, prints those, and
exits. The steward runs it as a Claude Code background task and ends its turn,
so an idle fleet costs it nothing and the exit wakes it. This is the machinery
the human pointed at ("claude has the concept of longer running tasks") doing
the waiting, not a timer. `--timeout` (30 minutes) exits empty, which is the
steward's cue to look over the running rows and write a short account of the
fleet in its own session: the human reads it by jumping in.

**"Handled" is on the record, so nothing is missed and nothing is answered
twice.** `WaitingSince` is stamped the moment a session stops for someone
(every `Stop` and `StopFailure` is a new wait; the idle prompt that trails one
is not) and cleared while it works. A wait is handled when it has a steward
note written during it (`note`), or a line typed into it (`Answer`, stamped by
`reply` and by the cockpit's `r`, under the hook lock, at the send). Nothing is
kept between two `--next` runs, so nothing between them can be missed. The
answer stamp came out of the first live run. A reply is typed, and until the
session's `UserPromptSubmit` hook lands the wait still looks open, so a steward
polling for open waits woke for it again. The live steward refused to reply
twice, on judgement. Correctness should not rest on that.

**The policy, in the brief.** The steward replies, with a one-line instruction
starting `steward:`, when the brief or the dispatcher's own in-brief
recommendation already answers the stop:

- an offer to do the next part of the work;
- an options list with a reversible, in-brief pick;
- a report while the brief is plainly unfinished;
- ending the turn to "check back once CI finishes";
- a transient failure.

It never replies to the following. Instead it writes a one-line note that
names the decision and the facts that bear on it, gathered first:

- a merge, release or deploy;
- anything that reaches people or production;
- deletion, force-push, infrastructure, secrets, permissions or spend;
- product intent the brief does not settle;
- new scope;
- missing access;
- a permission prompt (a menu it cannot answer);
- a usage or auth failure;
- a dispatcher it has already pushed twice on the same kind of stop.

When unsure, it notes: a needless note costs a glance, and a wrong push can
cost an incident. Park is for waits on the world.

**The human sees all of it on their table.** A noted wait leads its row with
`steward · yours: merge #12 — CI green, no review, touches billing`. An
answered one reads `answered · <the line>` until the session picks it up, so a
reply that never took stays visible. The headline says `steward watching` while
its session is up.

## Verified live

A real steward was run against a scratch fleet: two waiting dispatchers in
`cat` panes, plus a real `claude` session.

- **Asked "want me to write the unit tests next?" on a brief that asked for
  tests.** It replied: "steward: yes — write the unit tests for the retry/backoff
  (attempts, delays, jitter bounds, give-up path), commit, open the PR, and fix
  your own CI until green."
- **Asked "want me to merge it? main auto-deploys to production".** It did not
  reply. It noted: "yours: merge #12 — main auto-deploys to production;
  dispatcher reports CI green, idempotent on event id; review state unverified
  (no remote reachable from here)". It said plainly what it could not check.
- **It then slept on `--next`.** A new stop on the first dispatcher, asking to
  also migrate the downloader, woke it. It noted rather than replied: "yours:
  scope — asks to also migrate the downloader onto the retry helper, not in the
  brief; brief itself done".

There was no trust dialog and no permission prompt at any point.

## Considered and not done

- **A LAND switch, or letting the steward merge.** Merges were the human's
  judgement calls in ADR 0018's evidence, and a merge to an auto-deploying
  branch is the least reversible thing a dispatcher does. The steward makes the
  merge a glance — the facts on the row — and leaves the decision. Revisit with
  evidence of how often its merge notes are waved through unread.
- **The steward as a dispatcher record.** It would appear on its own table, get
  a branch and a worktree it has no use for, and be swept, parked and dismissed
  by rules written for work.
- **A cockpit loop that pushes by rule.** It would be the cockpit guessing, and
  the thing that distinguishes a push from a decision is reading the brief.

## Consequences

- The human is interrupted by the questions that are theirs, with the facts
  already gathered, and can read the steward's account of the fleet in its
  session.
- The steward costs tokens only when something stops.
- A chosen `CLAUDE_DISPATCHER_STATE` rides the steward's command, because tmux
  starts sessions with its server's environment. Dispatch sessions still do
  not carry it, so their hooks write to the default store: this only matters
  when the store is overridden, and it predates this.
- It stays a human-started session: nothing starts it on its own.
