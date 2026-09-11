# A repository is its common dir, and its name is not its folder

## Status

Accepted (2026-09-11)

## Context

`repos.Discover` walked each scan root three levels deep and applied one test:

```go
if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
    out = append(out, Repo{Name: filepath.Base(path), Path: path, ...})
    return filepath.SkipDir
}
```

A directory holding a `.git` is a repository, and its folder is its name. Both
halves are wrong, and a portfolio that uses git worktrees breaks on both.

A `.git` marks a **checkout**, and one repository has as many as it has
worktrees. Reported from a machine whose projects are laid out as
`<project>/.bare` with the checkouts beside it — measured there, discovery
returned **47 "repos" for 26 repositories**:

- `kolchurin.dev` appeared 13 times, once per worktree, as `logo`, `guestbook`,
  `bento-layout`, `preview-deployment`…
- `playerpulse` — an ordinary clone that grew worktrees later, as siblings of
  itself — appeared 8 times.
- Three unrelated repositories were all called `main`
  (`kolchurin.dev/main`, `claude-dispatcher/main`, `better-altegio/main`).

The name is the identity everywhere downstream: `[products]` maps a product to
repo names, `config.ProductFor` matches on it, `collect_products` indexes
`discByName` by it, and `dispatch.Launch` builds
`state/worktrees/<Name>/<slug>` from it. Three repositories sharing a key means
assigning one assigns all three.

It also silently broke the forge. `gh.AssignedIssues` and `gh.RepoPRs` key their
results by the **GitHub** repo name, with a comment in `gh/search.go` reading
*"matching how repos.Discover names a checkout"* — which it had stopped doing.
GitHub says `claude-dispatcher`; discovery said `main`; nothing joined. Six
repositories on the reporting machine are cloned into a folder that is not their
name at all (`ord-ai-n` → `ordain`, `k3s-migration` → `k3s-personal`,
`prototype-player-app` → `Player-App-2`), and every issue and PR belonging to
them had been landing nowhere.

And the depth budget cut the layout entirely. A bare project directory carries
no `.git` of its own, so it is not found by the test above and falls through to
the depth check — where `Build_Sticky/PlayerPulse_org/player-app` sits exactly
on the limit. The project was skipped with all **69 checkouts** inside it. The
reported symptom was a project that simply never appeared in the cockpit.

## Decision

**A repository is its common dir.** Git's own identity for "these checkouts are
one repository" is the git directory they share: `<clone>/.git` for an ordinary
clone, `<project>/.bare` for the bare layout. It is readable without a git
process — a `.git` directory *is* the common dir, and a `.git` **file** names a
worktree's private git dir whose `commondir` file points back at the shared one.
Three small file reads, which matters because a discovery runs on every load and
a portfolio like this has a hundred checkouts in it. Checkouts that resolve to
the same common dir are one `Repo`.

**This is an extension, not a replacement.** The walk is unchanged: a directory
holding a `.git` is a checkout and is not descended into. What is added is the
question asked of each one. An ordinary clone with no worktrees resolves to
itself and produces byte-identical output to before, and a checkout whose
metadata cannot be read falls back to being a repository of its own named for
its folder — which is exactly what every checkout used to be.

**The name comes from the origin remote**, read from `<common>/config`, falling
back to the folder only for a repo with no remote to ask. A repository does not
store its own name, but the remote does, and it is the name GitHub, `gh` and the
`[products]` map all use.

**One checkout is canonical.** Roughly twenty call sites *stand inside* `Repo.Path`
— `gh pr list`, the staleness `git log`, the decisions lens reading `docs/adr`
off disk, `git worktree add` at dispatch, the ROOT field's branch list — so it
has to be a real working tree. A bare repository has none of its own, which is
why this is a choice rather than a lookup: the main worktree when there is one,
else the checkout on `main`, then `master`, then first by path.

**The worktree list comes from git's registry, not from the scan.**
`<common>/worktrees/*/gitdir` is authoritative and complete; the scan is neither.
The scan skips hidden directories, so the common `.worktrees/<name>` layout
would report none of them, and a worktree may sit outside every configured root.
A registration whose directory is gone is one git has not pruned, and is dropped
rather than offered as a place to work.

**Evidence of a repository buys the level that reaches its checkouts.** A
directory holding a valid bare repo is descended into even at the depth limit.
The budget itself does not grow — a container *below* the limit is still out of
reach, and the answer to that is a nearer scan root.

**`p` in the assignment editor says where a repo is.** A row was a folder and is
now a repository: it may be named for none of the directories it occupies and it
may occupy sixty-nine of them. The screen you assign from could no longer answer
"which directory is this?" by being read, so `p` unfolds the selected row to the
absolute path of the checkout it acts in and every other checkout git knows of,
capped with a count of the rest. It toggles per repo and is keyed by name, so
the cursor moving on does not fold it — comparing two repos' locations is the
point of it.

## Consequences

- On the reporting machine, 47 rows become 26, `player-app` appears for the
  first time with its 69 checkouts behind one row, and six repos are renamed to
  what they are actually called.
- **A rename can orphan a `[products]` entry.** An entry naming a folder rather
  than the repo stops matching and its repo falls to "unassigned", where the
  editor reassigns it. Nothing rewrites the user's config: a migration that
  guessed would be editing the one file this tool promises is the source of
  truth. (The reporting machine's config needed none — its single entry names a
  repo whose folder and remote agree.)
- **A repo's worktrees are no longer individually visible.** That follows the
  model this tool already states — repo is the organising primitive, worktree is
  per-dispatch isolation — and `p` is where the detail went.
- Two repositories in different orgs with the same name still collide on one
  key. That is a pre-existing property of `[products]` being keyed by bare name,
  narrowed rather than widened here: the old scheme collided on folder names,
  which is how three repos came to be called `main`.
- Dispatchers still cut their worktrees under
  `~/.local/state/claude-dispatcher/worktrees/<repo>/<slug>`, not beside the
  user's own. Making that follow the project's layout is a separate decision:
  `x` removing a clean worktree would then be deleting inside a project
  directory.
