# 18. A stop says what it needs, and only the human's stops reach the human

Date: 2026-09-24

## Status

Accepted

## Context

Reported as: "I keep having to open and check my dispatchers to see where they
are at and ensure they are not stalled … I don't mind being involved in real
issues, but 70% of the time I seem to just be pushing them along." Then, while
this was being built: "claude has the concept of longer running tasks and
sub-agents already", and "I don't quite know if it's all going to plan … I
wonder if dispatcher should have its own claude code session … to monitor,
observe, kick along, pause, and act as the lead/steward".

### What the human was actually typing

Every dispatch record carries its transcript path, so the question has an
answer. Across 231 records there are 949 human prompts after a session's first.
For each, the dispatcher's last words before it, and the time between:

| the turn ended on | share | median wait for the human |
|---|---|---|
| a report | 51% | 7.3 min |
| a question or an offer ("want me to…?") | 28% | 6.8 min |
| asking to merge or ship | 18% | 11.1 min |
| an API error | 2.6% | **23 min** |
| a usage limit | 0.3% | 15 min |

The commonest follow-ups verbatim: `continue` (26), `merge it` (11), `ship it`
(8), `try again` (5), `yes` (5), `deployed?`, `landed?`, `are you stalled?`,
`status report please`. After the 24 API errors the human typed `continue` 20
times. 82 turns (9%) ended with the dispatcher saying it would check back
"once CI finishes" or report "when it lands".

### Why each of those cost a jump-in

Four causes, each checked in the code rather than inferred.

1. **The row did not say what it wanted.** A waiting row's SIGNAL was
   `cqWant(kind)` — "it finished a turn" on every row alike. The detail lead
   was the transcript preview, and `transcript.renderContent` keeps each text
   block's **first line**: the turn's headline ("PR #700 is open. Watching
   CI."). The ask closes the message ("Want me to merge it?", "Say the word and
   I'll…"), so the one sentence that needed an answer was the one sentence cut.
2. **Answering meant attaching.** `r reply` was left off triage on purpose
   ("this screen has one text affordance and it belongs to the dispatch
   form"), so every one-word answer was a jump-in and a jump back. And the
   `replyCmd` that did exist could not have worked: `SendKeys` targeted
   `-t =name`, and tmux 3.7b answers a pane command given a session target with
   `can't find pane` — measured. `replyCmd` discarded the error and announced
   "replied · session resumed" regardless.
3. **An API error was invisible.** Claude Code fires `StopFailure` *instead of*
   `Stop` when an API error ends a turn (read from the installed 2.1.281: "Fires
   instead of Stop when an API error … ended the turn. Fire-and-forget — hook
   output and exit codes are ignored"), and `init` never installed it. Matched
   against the event log, those turns got an `idle_prompt` a minute later at
   best and nothing at all at worst — three sessions sat on "working" for 20
   minutes to hours over a dead prompt. That is the 23-minute median.
4. **Auto dispatches were told how to start, not how to wait or when to stop.**
   The auto sentence was "Commit as you go, open the PR, and fix your own CI
   failures without stopping to ask." — it names the first stopping point and
   says nothing after it, so the session does what Claude Code does by default
   there: stop and offer. And only the triage form added it at all: the `+`
   overlay and every backlog launch went out in auto mode with no sentence.

## Decision

**A stop says what it needs.** The `Stop` hook's `last_assistant_message` —
the whole message, verified in the installed claude's hook schema — rides the
record as `Said` (tail-capped at 4000 runes, because the ask is at the end;
cleared by the next prompt, because by then it has been answered). `ask.Of`
quotes the sentence it closed on: the last question, or the last sentence that
hands the move over ("say the word", "let me know", "your call"), from the
closing three paragraphs, list items split and emphasis stripped. Measured
against the 949 real endings it finds an ask in 401 and nothing in the reports,
which is the right answer for a report. The row's SIGNAL and the detail lead
quote it; review rows too, since "approve a merge" is our reading of an open PR
and the session often closed on something else.

**Answer it where it is read.** `r` on a waiting row opens one line over the
table, showing the ask; enter types it into the session. Not on a permission
prompt — that is a menu, and typed characters would pick its options — and not
where `SessionIdle` proves claude has exited, where the text would land in the
shell. `SendKeys` targets the pane (`=name:`), types literally (`-l --`, so
"Enter" or "C-c" or a leading dash are typed, not pressed or parsed), presses
Enter as its own call, and a failed send is reported.

**An API error is heard, and the machine retries what the machine can.**
`StopFailure` is installed by `init`. It writes a `Failure` annotation — Claude
Code's own category and detail, verbatim — and needs-input, which the
`idle_prompt` that follows does not overwrite. The transient categories
(`overloaded`, `server_error`, `unknown`, `max_output_tokens`) are retried by
the cockpit's poll with `continue` on a 1m/5m/15m backoff, three steps, then
left to the human. It types only where the session is live, claude is provably
still at its prompt, and the row is not parked, and it claims the retry under
the hook lock against a fresh read so a human who answered in between wins. The
count survives the `UserPromptSubmit` its own retry causes and is reset only by
a `Stop`, the one proof the retry worked. Every retry is an event (`Retried`)
in the audit log. While a retry is coming the row rides with the running rows,
saying so (`api error · overloaded · retry 1 of 3 in 40s`), and counts toward
nobody's "wants you"; a retry more than two polls overdue stops being promised
and the row is the human's. `rate_limit`, credentials and billing are the
human's from the start, and the row names the error. Velocity bills the
interval after `StopFailure` as waiting, not as the prompt's working.

**The session keeps itself going with its own machinery; the contract asks it
to.** The mode's working contract is composed at `Launch`, beside FAN OUT's
sentence, for every way in, adding only sentences the prompt lacks. Auto's:
commit, open the PR, fix CI without asking; **wait on anything slow — CI, a
deploy, a long job — with a background task or Monitor rather than ending the
turn to check back later**; take the next step the brief implies instead of
offering it; stop only for a decision that is genuinely the human's — outside
the brief, destructive or irreversible, or needing access the session lacks —
and end on that one question. Manual and plan keep their one-pass sentences and
also end on the question. The DONE WHEN hint no longer promises auto "one pass,
then waits".

**The fleet is scriptable, for a steward.** `claude-dispatcher status
[--json]` prints the triage table's reading (wants-you / running / finished /
parked, most urgent first, with each ask); `reply`, `park`, `unpark` are its
hand-acts, with the same refusals and the same writes.

## Considered and not done

- **The cockpit keeping sessions busy.** Claude Code already keeps a turn alive
  across a wait (background tasks, `Monitor`) and spreads work across subagents
  (FAN OUT, ADR 0005); the `Stop` hook already reads in-flight background tasks
  as working (`WaitingOnTasks`). A second mechanism outside the session would
  compete with them. The contract asks the session to use its own; the cockpit
  acts only where nothing inside the session can — a turn `StopFailure` has
  already ended.
- **Blocking `Stop` to force continuation.** A `Stop` hook can return
  "block" and keep the turn going. It would have to decide, per stop, whether
  the session stopped for the human or for nothing — a judgement, made
  unseen, in the one process that must never disturb a session. That is a
  steward's call, made in the open.
- **An age threshold for "stalled".** ADR 0003's rule stands: no evidence says
  a thirty-minute quiet is worse than a five-minute one. The stall this found
  had a hook that reports it; the rest is what a working session looks like.
- **A LAND switch** (merge when green, see it deploy). Merge asks are the
  largest pushed category and the longest wait — but the replies to them were
  not all `merge it`: "hang on… so it doesn't meet the spec?", "do an
  adversarial review and then merge it", "one deploy at a time". A switch that
  merges to a branch that auto-deploys is a blanket answer to a question the
  human sometimes answers no. It is the first decision to hand the steward.

## The steward, next

A long-lived `claude` session of the dispatcher's own (`disp-steward`),
self-paced on a schedule, reading `status --json`: answering what is clearly
in scope with `reply`, shelving with `park` what is waiting on the world, and
escalating the real calls — a merge that needs a look, a question outside the
brief — to the human with the reason. The deterministic recovery (the retry)
stays in the cockpit, so the steward spends its judgement only where judgement
is needed. It builds on this record's pieces and needs nothing else from the
cockpit.

## Consequences

- A waiting row reads as its question, and most answers need no jump-in.
- An API-errored dispatcher is on the table in seconds, retried where retrying
  is what the human would do, and named where it is not.
- Retries only happen while a cockpit is open, like auto-done. On Windows
  `SessionIdle` is always unknown, so the retry never types there; the grace
  hands the row to the human after about three minutes.
- Existing installs re-run `init` to get `StopFailure`. `Said` fills from the
  next `Stop` under the new binary; until then the row falls back to the
  transcript preview.
- The cockpit test suite stopped reaching the real GitHub: `TestRefreshCmds`
  ran a whole load against the real `gh`, and one rate-limited answer parked
  `gh` process-wide, failing every later fake-`gh` test — on `main` as well.
