# 29. A steward is a conversation per product, and it keeps moving while you are away

Date: 2026-11-10

## Status

Accepted. Supersedes ADR 0020 (the steward is a switch) and amends ADR 0019:
there is no longer one steward for the machine, nothing starts one on its
own, and its policy grows rulings, saved questions and a recap.

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

## Considered and not done

- **Auto-resuming an interrupted steward.** It would keep the conversation
  through a reboot, but the human asked for the choice: an interrupted
  steward is either picked back up or replaced with a fresh one for new work,
  and only they know which. The cost is that a reboot while they are away
  stops the steward until they reopen it.
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
- Existing installs keep a `steward/` folder and possibly a `disp_steward`
  session from the old switch; nothing reads either now.
