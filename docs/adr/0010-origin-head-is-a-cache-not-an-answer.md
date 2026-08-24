# 10. origin/HEAD is a cache, not an answer

Date: 2026-08-24

## Status

Accepted

## Context

Reported as "the dispatcher attempts to fork a dead branch", with the ask
"we should have root branch on the dispatch form".

The event log had the launch that did it — the audit added in
[0009](0009-a-dispatch-that-did-not-happen-is-a-thing-that-happened.md), which
is the only reason there was anything to read:

```
{"event":"DispatchFailed","feature":"validate json-ld",
 "repo":"soundbooth-website-nextjs",
 "reason":"git branch feature/validate-json-ld from origin/dev:
           fatal: not a valid object name: 'origin/dev'"}
```

### The base was read from a cache nothing refreshes

`baseRef` resolved a dispatch's start point from
`refs/remotes/origin/HEAD`, and took the answer on trust:

```go
if out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short",
    "--quiet", "refs/remotes/origin/HEAD").Output(); err == nil {
    if ref := strings.TrimSpace(string(out)); ref != "" {
        return ref            // handed straight to `git branch`
    }
}
```

`refs/remotes/origin/HEAD` is written **once, by `git clone`**, and no fetch
ever updates it. It is a local guess at the remote's default branch, frozen at
the moment the repo was cloned. And `symbolic-ref` reports the name a symref
points at whether or not anything is there — so nothing in that code had any
occasion to notice the branch was gone.

Measured on the repo that reported it:

```
$ git symbolic-ref refs/remotes/origin/HEAD
refs/remotes/origin/dev
$ git rev-parse --verify origin/dev
(nothing — the branch was archived as origin/dev-archive and deleted)
$ git ls-remote --symref origin HEAD
ref: refs/heads/main	HEAD
```

That repo moved to trunk-based development. `dev` died. Its clone still names
it, and the remote has said `main` for months.

### It has two failure modes, and the quiet one is worse

- **The launch dies.** The old default has been deleted on the remote and
  pruned locally, so origin/HEAD dangles and `git branch --no-track <feature>
  origin/dev` fails. This is the reported bug, and it takes the whole dispatch
  with it: no worktree, no session, nothing.
- **The launch succeeds on a dead branch.** The old default still exists in the
  clone — a plain `git fetch` never deletes a remote-tracking ref — so the
  branch is cut, the worktree appears, the session starts, and the dispatcher
  spends its entire run on top of a branch nobody merges into any more. Its PR
  carries the diff against a retired base. **Nothing anywhere says so**: the
  record kept `BaseSHA`, and a commit does not say which branch it was the tip
  of.

Both were reproduced from scratch (rename the remote's default; delete the old
branch; fetch) before anything was changed.

The conventional-name fallbacks underneath were only ever a second guess at the
same question, and the last of them — the local checkout's `HEAD` — is the
defect [the base was introduced to fix](../../CLAUDE.md): whatever branch the
human happens to have checked out. Across the reporter's 60-odd repos no repo
currently reaches it, but nothing had ever stopped it.

## Decision

**Ask the remote. Resolve everything else before using it. And let the human
say.**

### The default comes from the remote

`git ls-remote --symref origin HEAD` is the only authoritative answer to "what
is this repo's default branch", and it is one network call on a path that
already makes one (the pre-dispatch fetch). Its answer is resolved to
`refs/remotes/origin/<name>` and used.

Nothing rewrites the human's `origin/HEAD`. `git remote set-head origin --auto`
would repair the cache, and repairing it is out of scope for a launch: resolving
a base reads, it does not edit somebody's repo as a side effect.

### Nothing unresolved is ever handed to git

Every remaining candidate — the local `origin/HEAD`, the conventional names,
the checked-out `HEAD` — must resolve to a commit before it is offered as a
base. A ref that does not resolve is not a base; it is a fact about a stale
cache, and the resolution carries on past it. A repo where nothing at all
resolves is an empty repository, and it is refused in our words rather than
git's, at the moment of asking.

The old ordering happened to be right and is kept: the remote's copy of a
branch beats the local one, because a local branch is as stale as the last time
the human pulled and a dispatch starting behind origin opens a PR carrying
commits it did not write.

### ROOT is a choice on both forms

The remote's default is right for nearly every dispatch, and it is not right for
all of them — stacking on another feature, or cutting a fix from a release
branch, is ordinary work. `Root` is that choice: a branch name, or
`RootDefault`.

`RootDefault` names **no branch**. It means "ask origin at launch", exactly as
`ModelDefault` means "pass no flag". A form cannot know a repo's default branch
without going to the network, and the only local answer available to it is
`origin/HEAD` — the cache this whole record is about. A form that filled it in
would be quoting it.

The affordance differs because the forms differ, and the repos decide: they
carry 170-odd branches each.

- The `+` overlay gets a **step** with a filtered list, the same shape as its
  repo step. The list is read from the repo when the repo is picked — once, not
  per keystroke — and never over the network: a form that stalls for a fetch is
  a form nobody uses. `refs/remotes/origin/HEAD` is not in it; it is not a
  branch, and on the repo that reported this it does not resolve.
- The dx form gets a **typed field**, because that form is one screen of
  switches and a branch list is not something you cycle. A name that is not a
  branch in the picked repo is refused *there*, with the near misses named
  (edit distance, because a mistyped branch is usually a transposition and no
  substring test can see one). By the time the launch could refuse it, the form
  and the brief typed into it are gone.

Both forms hand off through one seam (`launchDispatch`), so what a test asserts
is the argument that **leaves** the form and not the value on its screen — the
lesson from the truncated prompt, whose form state was correct the whole time.

### A named root is never quietly dropped

Re-dispatching a finished feature reuses the branch it left behind, and that is
wanted: it is how a dispatcher is sent back to work it already did. But a
branch that exists is not going to be re-cut from anywhere, so a root named for
one is **refused**, not ignored. The alternative is a human picking a base,
being agreed with, and getting another one — which is this bug with the blame
moved. Nothing is said when the root is the default, because then nothing was
asked for.

`Resume` rebuilds a reclaimed worktree on `RootDefault` for the same reason: it
is putting an existing branch back on disk, not cutting one.

### The record says what it was cut from

`Dispatch.Root` is the ref the branch actually started on, short —
`origin/main`. "What is this work on top of" had no answer anywhere, and the
quiet failure mode above is invisible without one. It is empty when the branch
already existed, because then this dispatch cut nothing.

## Consequences

- The reported launch works. Verified against the real repo
  (`baseRef` → `refs/remotes/origin/main`, was `origin/dev`) and end-to-end
  against the real binary on a repo built to the same shape: the dispatch that
  died with `fatal: not a valid object name` now starts, and its record reads
  `root: origin/main`.
- One extra network call per launch (`ls-remote`), bounded by the same 20s
  timeout as the fetch, with `GIT_TERMINAL_PROMPT=0` so a repo wanting
  credentials fails fast rather than holding a launch open. Unreachable is not
  a failure: it falls through to the local candidates, which are now verified.
- Stale remote-tracking refs are **not** pruned. `git fetch --prune` would make
  the offered branch list mean exactly "what origin has", and it deletes refs in
  the human's repo, which can leave commits unreachable. A launch is not the
  place for that. The consequence is that a form can offer a branch that origin
  retired since the last fetch; the launch fetches and refuses it by name.
- A dispatch can now start from a branch only this clone has. That is a real
  use, so it is offered — labelled `local only`, because the PR will be opened
  against a base origin has never seen.
- Records written before this carry no `Root`, which is not the same fact as
  "cut from nothing"; screens must read empty as unknown.
