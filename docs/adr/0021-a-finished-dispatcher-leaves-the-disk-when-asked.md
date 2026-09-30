# 21. A finished dispatcher leaves the disk when asked, and takes only what it can prove is ours

Date: 2026-09-29

## Status

Accepted.

## Context

Reported as "we need a way to tidy up and completely remove worktrees and
their node-modules".

Each dispatch gets its own git worktree under
`~/.local/state/claude-dispatcher/worktrees/<repo>/<slug>`. Only the cockpit's
kill ever removed one. A dispatcher that ended any other way (its session
ending, the tracker marking it live, the ghost sweep) left its checkout
behind, together with every dependency its session installed. Measured on the
reporting machine:

- About 130 folders across 19 repos, roughly 100 GB. Sailcoach alone was
  50 GB, and nearly all of it was `node_modules`, several per worktree in the
  monorepos.
- 88 of them belonged to `done` records and 10 to `exited` ones. Almost all of
  those were clean: nothing modified, nothing untracked.
- 39 were named by no record at all. Some were a case mismatch: `Aura` on the
  record and `aura` on a case-insensitive disk. Others were checkouts a
  session had made for itself. One was a **full clone**, with its own `.git`
  directory.
- The dirty ones held real work: staged deletions, modified sources, scraped
  data files.

`git worktree remove` without `--force` already deletes ignored files.
Measured on git 2.39, a worktree whose only extra content was an ignored
`node_modules` went cleanly. So the space was not held by git refusing. It
was held because nothing ever asked git.

## Decision

**A tidy is asked for, never automatic.** `: tidy` in the cockpit reads the
plan in the background and puts it on the confirm bar ("remove 84 finished
worktrees with their node_modules · strip node_modules from 6 with uncommitted
work · 10 kept"). `y` carries it out, also in the background. On the command
line, `claude-dispatcher tidy` lists every item with its reason and touches no
folder; `tidy --yes` does it. The steward's allow-list names its verbs one by
one, so it cannot run this without asking. Removing a finished checkout when
the record finishes would be the tracker deciding what happens to a folder the
human may still be looking in. The ask is cheap; the surprise is not.

**Only folders a record names.** The candidates are the `WorktreePath`s on
dispatch records, grouped by *file identity* (`os.SameFile`), not by string,
so `Aura` and `aura` are one folder. Nothing else in the worktrees directory
is ever listed. A full clone or a hand-made worktree belongs to somebody, and
it is not us.

**Only when nothing still claims it.** Every record naming the folder must be
finished (done or exited) and unparked, and must have no session left.
Finished is not enough on its own: `done` is the tracker's word, written when
the deploy succeeds whatever the session is doing, and an open session has its
working directory in that folder. A supervisor that cannot be asked counts as
"might be open". Before reading, both the command line and the cockpit run the
ghost sweep (`ReconcileSessions`, which every cockpit load already runs), so a
record still saying "working" over a session that is gone does not hold its
folder forever.

**git removes; we do not.** A clean worktree goes through
`git worktree remove` without `--force`, from its own repo, so git's own
checks stand as a second guard: a main working tree, a locked worktree,
submodules and a dirty tree are all refused, in git's words, which become the
reason shown. The **branch stays**. Commits are not lost, and `Resume` already
rebuilds a missing worktree at the same path, which is what makes the
transcript findable again. Removing the checkout is therefore undoable in
everything except the reinstall.

**Uncommitted work is never taken; its node_modules is.** A dirty worktree
keeps everything except the `node_modules` folders that git ignores and
tracks nothing in: every workspace's, not just the root's. An install rebuilds
those. A `node_modules` a repo has force-added files to is left alone.

**A detached HEAD whose commits no ref holds is kept.** The worktree's HEAD is
the only thing keeping those commits alive, and removing it would hand them to
the garbage collector.

**The plan is read again at the moment of acting.** A plan can sit on a
confirm bar while a dispatch of the same feature starts in that very folder.
Each item is re-planned against the records as they are now, and anything that
no longer plans the same way is kept, with the reason. Every removal and strip
is written to the event log (`Tidied`: dispatcher id, folder, what was taken),
so a folder that went missing can be traced to the tidy that took it.

## Consequences

- On the reporting machine the first run lists 84 removals, 6 strips and 10
  kept (4 in use, 6 dirty with no ignored `node_modules` to strip).
- Local feature branches accumulate. Deleting merged ones is a separate
  decision: a branch is cheap, and it is what `Resume` rebuilds from.
- Folders no record names still accumulate. They are listed nowhere by
  design; the human removes them by hand.
- Reading the plan costs a few git calls per worktree, about 14 seconds across
  a hundred. That is why it runs behind the confirm rather than in front of
  it, and why the count is of folders, not of bytes: sizing them took `du`
  three minutes.
