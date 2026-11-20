# 29. A steward is a conversation per product, and it keeps moving while you are away

Date: 2026-11-10

## Status

Accepted. Supersedes ADR 0020 (the steward is a switch) and amends ADR 0019:
there is no longer one steward for the machine, nothing starts one on its
own (an interrupted one is reopened by the poll only where `steward_reopen`
says so), and its policy grows rulings, saved questions and a recap.

## Context

Asked for as "have this steward per project session be interactive", pointing
at `coordinator-10.11`: a session the human started by hand in
`player-app/main`, told to coordinate the merge queue, and then talked to
all afternoon.

The question was whether the steward already did this. It did not, and the
record of its only real night says how far off it was. Run from 21:08 on
2026-10-15 to 11:16 the next morning, the machine-wide steward:

- woke 141 times on `status --next`, wrote **139 notes and no replies**, and
  pushed no dispatcher anywhere;
- wrote **72 of those notes on one record**: a dispatcher watching CI ended a
  turn on every tick, each closing on "want me to merge #743?", and each tick
  cleared the last note and started a new wait;
- wrote its account of the fleet ("Still waiting on you: …") every half hour
  into a session the human never opened;
- deferred, all night, to `orchestrator-2026-10-15`, the coordinator the
  human *was* talking to, for every GO.

The human switched it off the next morning and never on again. Three weeks
later they started `coordinator-10.11` by hand. Its transcript shows what the
steward lacked:

1. **The human talks to it.** It was handed rulings ("you own the queue",
   "#931 holds until I approve its goldens"), asked questions (#873 vs #927),
   and asked for the table of what is in flight and the plan.
2. **It talks to the dispatchers, both ways, mid-turn**, through Claude
   Code's session messaging; they answered with status and ETAs and asked it
   for GO. The steward's `reply` types at a prompt and reaches only a
   dispatcher that has stopped.
3. **It acts under authority it was given**: it merged greens that were on no
   hold, pinned to the head it had checked.
4. **It knows the repo's rules**, because it runs in the repo and loads its
   memory (who owns the queue, the box token, the OOM limits).
5. **It is quiet**: "tell me ONLY merges and failures".

The human then added the case this is really for: Remote Control on, a casual
check-in from the phone over a weekend while pre-approved work runs, and a
question they cannot answer in time. The work that does not depend on the
answer should keep going, and the questions should be waiting for them, all
at once, when they come back.

## Decision

**One steward per product, and each is a conversation that is kept.** A
product is the unit the human thinks in and the unit work crosses repos in
(`playerpulse-player` is three repos, and #933 spans all of them); a repo's
own queue still fits inside its product's steward. Each steward has a record
under `state/stewards/<product>/<id>/` beside its brief and settings. It is
started with `--session-id`, so `--resume` reopens that same conversation in
its own folder, and `--name steward-<product>`, so a dispatcher can message
it. At most one per product is current; starting or reopening another ends
it, since two leads coordinating one product would fight.

**Nothing starts a steward on its own.** A reboot leaves the current one
*interrupted*, and the human reopens it where it left off or starts a new one
for a new chunk of work. Both happen from the cockpit: `t` on triage opens the
steward picker on the selected row's product, listing each product's stewards
(live, interrupted, ended) and a "new steward" line. `enter` jumps in, reopens
or starts; `x` ends one, which stays reopenable; a new steward over a live one
is refused until it is ended, on purpose. `claude-dispatcher steward [list |
new <product> | open <id> | end <product|id>]` is the same from a shell. The
switch, its `enabled` file and `Ensure` are gone.

**Reopening an interrupted steward is a setting, and it is off.** Waiting for
the human ends the away case at the first reboot: Remote Control reaches only
a session that is running, so until they are back at a cockpit, pre-approved
merges stop and questions go unsaved. Asked on the PR, the human chose a
setting: `steward_reopen` in config.toml, on the settings screen as a choice
like the theme. `"never"` (unset, and any word but the other one) is the
behaviour above. `"interrupted"` has the cockpit's poll reopen the current
steward whose session went away without anyone ending it — the same
conversation, `--resume`d in its own folder, its first message saying it was
reopened by the cockpit and that the waits it watched may have moved. It
runs only while a cockpit is open, since the poll is the cockpit's.
Its limits are what make it safe to leave on:

- A steward the human **ended** never comes back. `end` writes the ending
  before the session goes, and every start, reopen and end holds one lock
  (`state.LockNamed("steward.lock")`), so a poll can never read one halfway
  through being ended and take it for one that went away.
- **`ReopenBackoff`**, 1m, 5m and 15m from the last opening, says when each
  attempt in a row may come, and its length is the cap. The first step stops a
  steward opened a moment ago, whose pane has not yet started claude, from
  being taken for a dead one; a reboot's steward was opened hours earlier and
  comes back on the next poll. After the third the steward is the human's,
  and the picker says so ("reopened 3 times, did not stay up"), because one
  that dies on every resume is not fixed by a fourth and each reopen can start
  a turn. An attempt is written before its launch, so a launch tmux refuses
  waits its step too. A steward found still up 15 minutes after a reopen has
  its count forgotten, and the human's own reopen clears it.
- A product the config no longer names is not reopened.
- `/exit` typed in the steward's session is claude exiting, so it reads as
  interrupted like a crash does; `x` in the picker is how a steward is put
  down.

**Its brief is written per product**: the product's repos and where each
keeps its own rules (CLAUDE.md and Claude Code's project memory, which win),
that the human talks to it here or over Remote Control, that it is quiet
otherwise, and that a working dispatcher is reached with a message by the
`peer` name `status` gives it (read from Claude Code's session registry),
while a stopped one is answered with `reply`. `status --product` and
`status --next --product` narrow each steward to its own dispatchers, grouped
by the cockpit's own rule.

**What it pushes toward is the dispatcher's goal.** DONE WHEN was a sentence
folded into the prompt and kept nowhere else. It is read back off the prompt
at launch into the record's `Goal` and handed to the steward as `goal`.

**The same question asked again keeps its note.** `note` records the question
it was about (`NotedAsk`); a later wait that closes on the same question keeps
the note, re-stamped, and the steward is not woken to write it again. Any other
wait drops it. A reply does not carry: one typed and asked again settled
nothing.

**Rulings are written down, dated, and end.** `ruling add --product P
[--allow merge] [--repo R] --until <when> <text>` records one; 24 hours
unless said. It is the one verb the steward's settings put under `ask`, so the
human confirms every ruling it records and the steward cannot widen its own
hands. Most rulings are guidance. One that allows merging is the only way the
steward merges: `merge <repo> <pr> --head <full sha>` refuses without a ruling
that allows it there, merges in the repo's own checkout pinned to the head
whose checks the steward read, and logs a `StewardMerged` event naming the
ruling. Deploys, messages and deletions stay the human's whatever a ruling
says. The picker shows each product's standing rulings and when a merge one
ends.

**Questions are saved, never waited on, and asked all at once on recap.**
Every question for the human goes through `ask` (two to four options when it
is a choice, and the context someone gone for hours needs); the same open
question asked again is the one already saved. The steward keeps everything
that does not depend on the answer moving and tells the dispatcher it blocks
to set that part aside. It never asks through a menu unless the human wrote to
it that turn, because a menu holds the session until it is answered, and from
a phone in a pocket that can be hours. A question unanswered for
`steward_away_after` (15m) is **postponed**: the human is taken to be away,
`status --next` wakes the steward at that moment (`now_away`), and it stops
holding work for the answer. When the human types `recap_keyword` (`recap`),
it runs `questions --json`, asks every choice through menus (up to four a
call) and the rest in prose with what has happened since, says what happened
while they were away, and records each answer with `answer`. Both settings are
on the settings screen; the headline counts the questions saved for the human,
and the picker says which product has them and the word that asks for them.

## Verified live

A real steward was started for `claude-dispatcher` from the built binary, on a
scratch store and a private tmux server:

- `claude --session-id … --name steward-claude-dispatcher --permission-mode
  auto` started with no trust dialog and no permission prompt; the prompt bar
  carried the name; its first act was `status --product claude-dispatcher
  --next` as a background task, and it ended its turn.
- **Away.** Two questions were saved as if asked 14m50s earlier — one choice,
  one open. Ten seconds later `--next` woke it with both in `now_away`. It
  checked the facts first (it found the PR one question named did not exist),
  held nothing for them, and went back to sleep. It also announced the
  postponement in its session, which nobody was reading; the brief now says to
  stay quiet there, since the recap is where the human hears of it.
- **Recap.** `recap` typed into its pane brought up Claude Code's question
  menu for the choice, with its context, its recommended option first and,
  since the question had gone stale, a "drop it" option of its own. The
  answer was recorded with `answer`. The open question followed in prose,
  with what each answer would set going and the repo's own rule (Alex merges
  into main) applied. Then came "while you were away", and it went back to
  sleep on `--next`.
- **Reopen.** `steward end` then `steward open <id>` resumed the same session
  id in its own folder with the earlier conversation intact.
- **Reopening on its own** (`steward_reopen`), from the built binary on a
  scratch store and a private tmux server, with a stand-in `claude` on PATH
  that logged its arguments. Two products' stewards were started and their
  sessions killed behind their backs. A cockpit with the setting at `never`
  left both interrupted. Restarted at `interrupted`, its first poll resumed
  each with `--resume <its own session id>` and the reopened-by-the-cockpit
  opening, recorded `auto_reopens: 1` on each, and the footer read "reopened
  beta's and alpha's stewards (they were interrupted)". Then beta was ended
  with `steward end` and alpha killed again: four polls passed alpha by, it
  came back on the poll at its five-minute step to the second
  (`auto_reopens: 2`), and beta never came back.

## Considered and not done

- **Reopening an interrupted steward by default.** It keeps the conversation
  through a reboot, but the brief asked for the choice: an interrupted steward
  is either picked back up or replaced with a fresh one for new work, and only
  the human knows which. So it is `steward_reopen = "interrupted"`, opted into,
  and the cost of leaving it off is the one above: a reboot while they are
  away stops the steward until they reopen it.
- **Reopening from anywhere but the cockpit's poll** — a systemd unit, a
  login item, a tmux hook. Each would cover a reboot with no cockpit open, and
  each is a second place, outside this tool, that decides when a steward runs.
- **Running the steward in a repo's checkout**, as the coordinator did, so it
  inherits the repo's memory. A product spans repos, and the folder must be
  ours to write the brief into; the brief points at each repo's rules instead.
- **A cockpit screen for answering saved questions.** The recap in the
  steward's session is where the human already is when they come back, over
  Remote Control or attached, and a menu there is Claude Code's own.
- **Rulings that grant deploys.** For these repos a merge already deploys
  (GitHub Actions on merge), and a separate deploy hand has no case yet.

## Consequences

- Each product's steward costs tokens only when something in that product
  stops, a question crosses the away window, or the human talks to it.
- A steward's conversation survives being ended: reopening it resumes with
  everything the human told it, and its rulings and saved questions are the
  product's, so a fresh steward picks them up too.
- Dispatchers launched before this carry no `Goal`; the steward falls back to
  their prompt.
- An existing install's machine-wide steward is retired: the cockpit ends a
  running `disp_steward` session and removes its switch at startup and on
  every poll, and a bare `steward stop` does the same. Left running, its old
  brief would go on answering every product's waits beside the new stewards.
- With `steward_reopen` on, a reboot costs each interrupted steward one
  resumed turn on the cockpit's first poll; one that keeps dying costs at most
  two more, five and then fifteen minutes later, before it is left for the
  human.

## After review

An independent review of the branch found these, each fixed on its own:

- an unquoted `--until 2026-11-16 09:00` stood until the end of the 16th, and
  flags after a ruling's text were swallowed into it;
- a question that went past the away window between two waits was never in
  `now_away` — each announcement is stamped on the question (`AwayAt`) now;
- a note on a menu recorded the last turn's question and could carry onto it;
- a carried note never woke the steward again, so a later ruling never
  reached it — the brief sends it back over noted waits after a ruling;
- product names that slug alike (or non-Latin ones) shared a session and a
  rulings file — a lossy slug carries a short hash of the name, and a product
  the config does not name is refused;
- `steward end <name>` took a hex-looking product name as an id prefix;
- the steward's settings did not deny `gh pr merge` or `git push`.
