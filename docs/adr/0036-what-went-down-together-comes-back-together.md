# 36. What went down together comes back together

Date: 2026-11-25

## Status

Accepted.

## Context

Asked for as "can i relaunch multiple cockpit sessions at once?", with a
screenshot of the triage table: ten rows under the `finished` divider, nine
of them reading "stopped" or "merged" and every one of them last seen 15h
ago. That is what a reboot looks like here. The machine goes down and tmux
goes with it. The ghost sweep (ADR 0004) and the hold (ADR 0012) then do their
jobs, and the whole fleet lands under one divider at once.

⏎ on a finished row resumes it (`dispatch.Resume`) and then hands the terminal
to its session, the same handover as a jump-in. For the one dispatcher you
want to look at, that is right. For ten it means ten resumes, ten jump-ins and
ten ways back to the cockpit, one row at a time. A resume opened with nothing
said to it also sits at its prompt, so the work does not start again until
somebody types into each one. Nothing else could resume a dispatcher: there
was no command for it, and the steward cannot.

## Decision

**`space` marks, `R` resumes every mark, and nothing is attached.** `space`
marks the row under the cursor on the triage table or in history, `esc` clears
the marks, and `R` resumes every marked row. With nothing marked, `R` resumes
the row under the cursor, which also gives a single resume that does not take
the terminal. A mark is drawn as ◆ in place of the row's glyph, the same mark
the sessions tab uses. The marks are the screen's, kept by record id. They are
not the record's, because nothing about a dispatcher changes until R acts.

**A row can be marked exactly when its own ⏎ would resume it.** `cqResumesOn`
is the rule ⏎ follows in `cqRun`, and `cqResumable` asks it of the row's own
⏎. So a finished row (held or history) can be marked, and so can a parked one
whose session is gone. A running row cannot, and `space` on one says why
rather than marking something R cannot resume. A mark whose row stops
qualifying, because it was resumed some other way or has left the store, is
dropped by the next snapshot (`manyPrune`, from `cqReconcile`).

**R asks one line first, and an empty line is an answer.** The input lists
what is about to start and marks a launch that never ran, since that one
starts again with its brief instead of reopening a conversation. Whatever is
typed is said to each as its opening message, through the same prompt file a
single resume uses. `continue` is the usual line after a reboot. An empty line
reopens them with nothing said, as the product panel's resume already does.

**The batch is `dispatch.ResumeMany`: Resume once per record, plus two rules
that only a batch needs.** Both the cockpit and the shell command use it.

- *One record per feature.* A shipped feature dispatched again reuses its
  name, so two finished records can share a slug and therefore a worktree.
  Resume's own guard looks for a **live** dispatcher of the slug. The first of
  the two is not live yet when the second asks, because its session has only
  just been made. So the batch keeps the first in table order and refuses the
  rest by name, before anything starts.
- *One session server, and one repository, at a time.* Starting a session on
  a server that is not running yet starts the server, and two clients doing
  that at once race for it. Rebuilding a reclaimed worktree writes to the
  repository's git dir, and two of those at once can meet on its lock. So
  records that share either, transitively, form one lane and run in turn,
  while separate lanes run at the same time. A resume is mostly waiting on
  tmux and on `nix develop`, and ten in a row is the slowest way to do that.
  An empty socket is the default server, which every record from before
  per-repo servers shares, so it is grouped like any other.

**Each outcome shows as it lands, and the marks keep the account.** Outcomes
arrive one at a time over a channel. Each one asks for a load, and load.go
collapses those into one in flight, so a row turns when its own session is up
rather than when the slowest one is, and the footer counts them in. A
dispatcher that came back loses its mark. So does one whose claude turned out
to be still running (`ResumeLive`), which was not sent the line, and the
summary says so. One that did not come back **keeps its mark** and shows the
cause in red on its own detail panel. The footer holds one line, a batch can
fail in more ways than one line can say, and a failure that leaves no mark is
ADR 0009's defect again. The batch resumes each record **as it is on disk when
the batch starts**, never the snapshot's copy. A load can be older than a
resume that has landed since, and Resume decides what to do from the record it
is handed. A record that has gone from disk by then is a failure like any
other, and keeps its mark.

**`claude-dispatcher resume <id|feature>… [--say "…"]` does the same from a
shell.** It runs the same batch and prints one line per record as each lands,
then exits non-zero when any record did not resume. Every name is resolved
before any resume starts, so a typo in the third name cannot leave the first
two running and the rest not. A name is an id, or a feature among every record
(finished ones included, which `find` leaves out). A feature carried by more
than one record is refused with their ids, newest first, rather than guessed
at. It is **not** on the steward's allowed list. Starting work again spends a
subscription, and that call is the human's, as it is for `swap` (ADR 0027).

## Consequences

- Marks do not survive a restart of the cockpit. They are a selection, not a
  fact about any dispatcher, and the rows they were on are still there to mark
  again.
- R refuses while a batch is under way rather than queueing a second one. Two
  batches could pick up the same record, and the per-record rules above are
  rules about a single batch.
- A batch is only as parallel as its lanes allow. On a portfolio where most
  dispatchers live in one repo, most of the batch runs in turn, and the gain
  over pressing ⏎ ten times is the ten jump-ins it skips.
