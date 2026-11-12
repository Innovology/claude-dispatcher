# Branchless work is a conversation across the product

## Status

Accepted (2026-11-06)

## Context

Asked for as:

> there needs to be a way to dispatch a session without creating a dedicated
> branch for it

and, while it was being built:

> branchless work should be project wide. so if i wanted to chat about the
> entire project or a specific repo in it, the agent would be able to navigate
> the conversation easily

and, once a read-only version was up for review:

> a conversation might end up going into a worktree/branch or creating one.
> the point of the feature is to allow us to start a conversation about the
> project but then evolve into either a worker or an orchestrator.

Every dispatch cut `<prefix><slug>` in a worktree of its own, because every
dispatch was assumed to end in a pull request, and the auto contract said so:
"Commit as you go, open the PR". Plenty of work does not end in one. Read off
the reporting store, three dispatches in a row went out asking for an answer:

- `review-163`: "quickly review it first and only provide the succinct report"
- `byga-integration-sitrep`: "what's the status of byga integration? what are
  the tests we have run and is there anything missing"
- `find-mannequin`: "… it got lost during the player-app's migration to this
  design system repo. find it"

Each got a branch named after it and a contract asking for a pull request the
brief did not want. Branches cost something in repos that already carry
170-odd each. The only other way to get a session with no branch of its own
was to start one by hand in a shared checkout and adopt it (ADR 0026). Every
adopted record on that store sits in a `main` or `dev` checkout.

The first build answered the first sentence alone: a worktree of the repo,
checked out detached at the root, with no ref created. The second sentence
changed the unit: a conversation is about a product as often as a repo, and
one that starts on a repo has to be able to follow the talk to another. A
detached checkout of one repo cannot do that. A detached checkout of every
repo in the product would be a worktree per repo per question, made to read
files the human already has on disk.

## Decision

**A dispatch can cut no branch, and then it is a conversation across its
product.** BRANCH on both dispatch forms is `own branch` (the default,
unchanged) or `no branch`. With none:

- **Nothing is cut.** No branch and no worktree, in any repo. The session
  reads each repo's canonical checkout, the one its products-lens row reads,
  given to claude with `--add-dir`.
- **The scope is the product.** No branch on a repo starts the conversation
  there and gives it every repo in that repo's product (`dispatch.ScopeOf`,
  the focus first). The product as a whole is its own row on WHERE, after its
  repos, and is branchless by construction. A product of one repo has no such
  row, and a repo in no product is a scope of one.
- **It runs in a folder of its own,** `branchless/<slug>-<id>` under the
  state dir. That folder's `CLAUDE.md` is the brief: every repo, its checkout, what
  its work merges into, and where the conversation starts. It is a brief and
  not a first message because claude reloads CLAUDE.md itself and keeps it
  through compaction, and a conversation that moves between repos is exactly
  the one that needs to look this up an hour in. The transcript is keyed by
  the folder, so Resume finds this conversation and not whichever one last
  ran in a shared checkout. The folder is trusted the way the steward's is
  (`TrustOwnDirFor`, in the account it runs under). The folder is the
  record's own, not the slug's. A slug keeps five words, and two
  conversations named as questions ("what is the status of …") share one: keyed
  by the slug, the second rewrote a finished first one's brief. The launch keeps
  a copy of the brief beside its prompt, and Resume puts it back if the folder
  has gone.
- **It starts as a conversation and can turn into work.** The contract says
  where it is: in the human's own checkouts, which it reads and never changes.
  When the talk turns into work, it goes onto a branch of its own or is sent
  out, and its CLAUDE.md names the two verbs, with the command that reaches
  this build:
  - **A worker:** `claude-dispatcher worktree --repo <repo> <name>`
    (`dispatch.TakeBranch`). Given an existing branch, it goes into that
    branch's worktree, or cuts one for it. Given anything else, it cuts the
    repo's prefix and the name's slug from the root, where the repo's layout
    puts a dispatch's worktree. The record becomes a worker's: branch, repo and
    base set, so the PR, provenance and done-means-live all come back, while
    its folder stays where the transcript is keyed. One branch per
    conversation, and never the canonical checkout's own.
  - **An orchestrator:** `claude-dispatcher dispatch --repo <repo> --title …`.
    Each worker is an ordinary dispatcher, on the conversation's subscription,
    recorded as sent by it (`DispatchedBy`), steered with `status` and `reply`.

  Both run from the conversation's own shell and find its record by the
  `CLAUDE_DISPATCHER_ID` its launch line set. A session cannot move its own
  launch directory, and claude's `EnterWorktree` only enters worktrees of the
  repository it was launched in, which a conversation's folder is not. Auto's
  "commit as you go, open the PR" leaves the opening contract, since there is
  no branch to commit to yet; the brief says it for when there is. The
  isolation decision stands: work happens in a worktree, never in the shared
  checkout.
- **It borrows from its focus.** One that starts on a repo takes that repo's
  tmux server, shell and toolchain (`repos.EnvIn` names the checkout's flake,
  since the folder has none). One about the whole product runs in the plain
  environment, on a server named for the product. It is not left unnamed:
  with no `-L`, tmux answers on whichever server `$TMUX` points at, and run
  end to end the conversation landed on the cockpit's own server.

`--add-dir` goes **after** the prompt. It is variadic (`--add-dir
<directories...>`), and measured on claude 2.1.317, `claude -p --add-dir <dir>
"say hi"` fails with "Input must be provided…": the prompt was read as a
directory. With the prompt first, both directories given were readable, one
with a space in its path, and a directory not given was denied.

## Consequences

- The record says `Branchless`, names the product, and carries `AddDirs`,
  which Resume passes again. `RepoName` is the focus, or empty for the whole
  product (`WholeProduct`). It is filed under the product on its record,
  because the config can only map a repo to a product.
- While it is still a conversation (`Conversation()`: branchless, no branch
  taken) there is no provenance: no base SHA, no commits, no diff, no effort
  figure, no pull request. An empty branch asks the forge nothing. `gh` reads an empty
  `--head` as no filter, so before that guard the tracker would have linked
  the repo's newest merge to the record and marked it done.
- The triage row says `no branch` where the branch would be, and `all repos`
  where a whole-product record names no repo. `y` is offered as `mark done`
  with nothing shipped, because a conversation was never going to ship, and
  "marked shipped" would claim something went live. For the same reason, a
  conversation marked done is not a ship anywhere else: history says
  "marked done", velocity does not count it as live, and the floor leaves it
  off.
- Kill and tidy leave the folder alone: it is not a checkout
  (`OwnsWorktree` is false), and resuming needs it.
- The steward leaves these alone. Every stop is the human's turn to talk, so
  `status --next` never wakes it for one, and its brief says so.
- The product panel's resume overlay starts a never-run conversation again
  through Resume rather than dispatching it afresh with a branch.
- Each repo's worktree folder is given with `--add-dir` beside its checkout, so
  a worktree the conversation cuts can be worked in without asking. A folder
  inside another one given is left out, as on a project layout. The checkouts
  are writable too, and "change nothing in them" is a convention, as the rest
  of the contract is.
- Kill and tidy remove the worktree a conversation cut (`RemovableCheckout`),
  never one it only went into, and never its folder.
- A worker sent from inside a session is launched with nothing of that
  session in its environment (`ScrubSessionEnv`). A pane takes its environment
  from the client that asks for it, and Claude Code puts its session id,
  messaging socket and token, `CLAUDECODE` and the dispatcher id in its tools'
  environment. Checked end to end: the worker's claude carried only its own id.
- A conversation that took a branch is a worker. Its row shows the branch, `y`
  is "mark shipped" once it has commits, and a done one is the ship it is.
