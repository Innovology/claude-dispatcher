# Claude Dispatcher — project conventions

## Vocabulary (non-negotiable, in code and UI)
- **Dispatcher** — a single unit of execution you send work to. NEVER "agent",
  "bot", "runner", or "worker" anywhere in the product.
- **Dispatch** — the act of sending work; also the cockpit collectively.
- **Feature** — the human unit of work; history is navigated by feature, not
  by commit hash.

## Agreed decisions (2026-08-06, worktrees added 2026-08-07)
- **Multi-repo, multi-worktree, multi-product** — three independent axes:
  - *Repo* is the organising primitive; discovery via configured roots.
  - *Worktree* is per-dispatch isolation: each dispatch gets its own git
    worktree of its repo under
    `~/.local/state/claude-dispatcher/worktrees/<repo>/<slug>`, so concurrent
    dispatches — and the human — never fight over one checkout. `x` removes a
    clean worktree; a dirty one is kept for inspection. (Supersedes the
    original "multi-repo, not multi-worktree" call, reversed the next day
    after two sessions collided in one working copy and a commit landed on
    the wrong branch.)
    - The feature branch is cut from the repo's **default branch as the
      remote sees it** (`origin/HEAD`, falling back to origin/main, then
      local), after a best-effort fetch — never from the repo's HEAD. Git's
      default would inherit whatever branch the human left checked out, so a
      dispatch would silently start on top of an unmerged feature and carry
      it into its own PR. Created `--no-track`, or `git push` would refuse a
      branch whose upstream has a different name.
    - **One live dispatch per feature name.** The name is the key: the
      worktree path and the cockpit's record map are both keyed by it, so a
      second concurrent dispatch of a live name would put two sessions in one
      checkout. Launch refuses it; re-dispatching a *finished* feature is
      still fine and reuses the worktree left behind.
  - *Product* is the grouping lens: the cockpit list and the dispatch form's
    repo picker group by the `[products]` config, most urgent group first;
    unmapped repos fall under "other". No separate group concept.
- Sessions run as interactive `claude` inside per-dispatch tmux sessions
  (`disp-<slug>`); tmux is a hard dependency and the process supervisor. The
  cockpit is a stateless viewer — "jump in" hands the terminal to tmux.
- Status truth comes from one global Claude Code hook in
  `~/.claude/settings.json` (hooks cannot be injected at launch time). The
  `CLAUDE_DISPATCHER_ID` env var is the join key from session to record.
  Transcript JSONL parsing is best-effort preview only (format is internal).
  The one thing no hook can report is a session dying without getting a
  `SessionEnd` out (SIGKILL, an outside `tmux kill-session`, a tmux server
  that went down with the machine), so `dispatch.ReconcileSessions` sweeps
  working/needs-input/blocked records whose session is gone and marks them
  exited. It never sweeps `launching`: no hook has fired for one, so its
  session may simply not exist *yet* — absence is only evidence where a hook
  proved the session once existed.
- **Coming back from a jump-in rechecks, it does not redraw.** The human has
  just spent minutes driving the session by hand, so `cockpit.recheckCmd`
  drops the gh cache, sweeps session liveness, reconciles PR/deploy and only
  then rebuilds the snapshot — the ordinary poll reload would serve forge
  state cached from before they went in and take the record's status at its
  word. Where the handover exits on the way *out* rather than on the way home
  (`switch-client` inside tmux, the raised console window on Windows —
  `supervisor.AttachSwitches`), the recheck waits for the terminal focus
  event instead. Focus alone never triggers it: a full forge re-read on every
  alt-tab is how the gh quota gets burned.
- Features are named at dispatch time (hybrid model): the name is the key;
  branch `feature/<slug>`, commits, and PRs enrich it automatically. Every
  dispatch works on a feature branch, even in repos that ship from main
  (PR from branch onto main).
- "Done means live": a feature stays open until deployed, unless explicitly
  stated otherwise. Deploys are always GitHub Actions; `internal/track`
  flips features to done when the deploy workflow succeeds after PR merge
  (merge counts as live for repos with no deploy workflow). `d` in the
  cockpit is the manual override. Auto-done only advances while a cockpit
  is open (the tracker runs from the cockpit's poll loop).
- Commit attribution is by provenance (dispatch records its branch SHAs),
  NEVER by Co-Authored-By trailers — the user strips those from commits.
- User is on a Claude subscription (not API billing): portfolio roll-up
  speaks in tokens/effort, never dollars.

## Architecture map
- `main.go` — subcommand dispatch: cockpit (default), `init`, `hook`.
- `internal/state` — dispatch records + event log under
  `~/.local/state/claude-dispatcher/` (override: `CLAUDE_DISPATCHER_STATE`).
- `internal/hookcmd` — receives lifecycle hook events, drives the status
  state machine (launching/working/needs-input/blocked/done/exited).
- `internal/dispatch` — branch + tmux + record creation.
- `internal/ui` — Bubble Tea cockpit; responsive tiling breakpoints at 110
  and 170 columns (more panes on wide screens, never one ballooned view).
- `internal/ship` — shipping stats (Claude-stamped = Co-Authored-By trailer).
- `internal/version` — the build's version (stamped by goreleaser via
  `-X claude-dispatcher/internal/version.Version`), the cached, best-effort
  check for a newer release, and `Detect()`: which package manager installed
  this binary, read from its own resolved path, and the one command that
  upgrades it. `U` in the cockpit runs that command and re-execs. A
  declaratively-installed Nix build is never upgraded imperatively — only an
  entry in the imperative profile's `manifest.json` proves it may be.

## Build
`make build` / `make vet` / `make install` (binary to ~/.local/bin — the init
hook embeds the absolute binary path, so reinstall to the same path).
