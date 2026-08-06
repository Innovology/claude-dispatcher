# Claude Dispatcher — project conventions

## Vocabulary (non-negotiable, in code and UI)
- **Dispatcher** — a single unit of execution you send work to. NEVER "agent",
  "bot", "runner", or "worker" anywhere in the product.
- **Dispatch** — the act of sending work; also the cockpit collectively.
- **Feature** — the human unit of work; history is navigated by feature, not
  by commit hash.

## Agreed decisions (2026-08-06)
- Multi-repo: independent repos are the organising primitive; discovery via
  configured roots. Each dispatch runs in its own git worktree of its repo,
  under `~/.local/state/claude-dispatcher/worktrees/<repo>/<slug>`, so
  concurrent dispatches — and the human — never fight over one checkout.
  `x` removes a clean worktree; a dirty one is kept for inspection.
  (Supersedes the original "not multi-worktree" call, revised later the
  same day after two sessions collided in one working copy.)
- Products are the grouping lens: the cockpit list and the dispatch form's
  repo picker group by the `[products]` config, most urgent group first;
  unmapped repos fall under "other". No separate group concept.
- Sessions run as interactive `claude` inside per-dispatch tmux sessions
  (`disp-<slug>`); tmux is a hard dependency and the process supervisor. The
  cockpit is a stateless viewer — "jump in" hands the terminal to tmux.
- Status truth comes from one global Claude Code hook in
  `~/.claude/settings.json` (hooks cannot be injected at launch time). The
  `CLAUDE_DISPATCHER_ID` env var is the join key from session to record.
  Transcript JSONL parsing is best-effort preview only (format is internal).
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

## Build
`make build` / `make vet` / `make install` (binary to ~/.local/bin — the init
hook embeds the absolute binary path, so reinstall to the same path).
