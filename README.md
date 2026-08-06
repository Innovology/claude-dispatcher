# Claude Dispatcher

A terminal-native dispatch cockpit for running many Claude Code sessions
across many independent git repositories. It sits **above** your repos, not
inside any one of them: launch dispatchers, see at a glance which are
working, which are done, and — critically — which are blocked waiting on you.

## Vocabulary

- **Dispatcher** — a single unit of execution. You dispatch defined work to
  it and it carries it out. Not an agent.
- **Dispatch** — sending work to a dispatcher; also the cockpit itself.
- **Feature** — the human unit of work. History is navigated by feature, not
  by commit hash.

## How it works

- Each dispatcher is an interactive `claude` session inside its own tmux
  session (`disp-<feature-slug>`), started on a `feature/<slug>` branch in
  the target repo. Sessions survive cockpit restarts; "jump in" is a plain
  tmux attach at full fidelity.
- Status comes from a single global Claude Code lifecycle hook (installed by
  `init` into `~/.claude/settings.json` with your consent). The hook maps
  events to states: **working**, **needs you** (turn complete or waiting for
  a prompt), **blocked** (permission approval), **exited**, **done**.
- The cockpit is a stateless viewer over
  `~/.local/state/claude-dispatcher/`, refreshed by fsnotify.
- Wide screens tile into more panes (dispatchers → detail → shipping stats)
  at 110 and 170 columns; narrow terminals collapse back to essentials.

## Requirements

- macOS/Linux, `tmux`, `git`, the `claude` CLI. `gh` optional (roadmap).

## Install

Via Homebrew:

```sh
brew install innovology/tap/claude-dispatcher
claude-dispatcher init        # config + repo scan + hook install (asks first)
claude-dispatcher             # open the cockpit
```

Or from source:

```sh
make install                  # builds to ~/.local/bin/claude-dispatcher
```

Note: the status hook embeds the absolute path of the binary that ran
`init`. If you switch install methods later, re-run `init` so the hook
points at the new binary.

Releases are cut by tagging (`git tag v0.x.y && git push --tags`); the
Release workflow needs a `HOMEBREW_TAP_TOKEN` secret (fine-grained PAT with
contents:write on `Innovology/homebrew-tap`) and skips publishing when the
secret is absent.

Config lives at `~/.config/claude-dispatcher/config.toml`: scan `roots` and
an optional `[products]` map (product → repo names) for the roll-up lens.

## Keys

| key | action |
|---|---|
| `n` | dispatch: pick repo → name feature → write prompt (`ctrl+d` to launch) |
| `enter` / `a` | attach to the selected dispatcher's tmux session |
| `d` | mark shipped (done means live — manual until Actions integration) |
| `x` | kill the tmux session |
| `r` | refresh |
| `q` | quit |

## The shipping loop

- Commits are attributed to dispatchers by **provenance**: each dispatch
  records the SHAs produced on its feature branch (base tip at launch →
  branch tip at each turn). No trailers or markers in your git history.
- Each dispatch is linked to its PR by branch name (`gh pr list --head`).
- **Done means live**: when the PR merges, the tracker watches the repo's
  deploy workflow (auto-detected by name — deploy/release/publish/ship/prod
  — or overridden in `[deploy_workflows]`) and flips the feature to done on
  a successful run. Repos with no deploy workflow count merge as live.
  Auto-done advances while a cockpit is open; `d` remains the manual
  override.
- The shipping strip shows: commits today, via-dispatch %, PRs launched
  today (one `gh search` across all of GitHub), features that went live
  today, and active repos.

## Roadmap (deliberately deferred)

1. Portfolio roll-up: effort and token spend per product (tokens, not
   dollars — subscription billing).
2. Feature history: jump back into a shipped feature and rehydrate its
   sessions (`claude --resume`) and branch diff.
3. Adopting sessions started outside the cockpit (the hook already logs
   them to `events.jsonl`).
4. Diff pane on ultrawide layouts.

## Development

`make check` runs build, vet, lint (golangci-lint), and the race-enabled
test suite — the same gates CI runs on every PR. The CI workflow is named
"CI" deliberately: this tool's own deploy detection treats deploy-ish
workflow names as a live signal.

## Troubleshooting

- **Everything stuck on "launching"** — the hook isn't firing. Re-run
  `claude-dispatcher init`; confirm the entries in `~/.claude/settings.json`
  point at the installed binary path.
- **Statuses lag** — `needs you` vs `blocked` relies on the `Notification`
  hook matchers `idle_prompt` / `permission_prompt`; `Stop` covers turn
  completion regardless.
- Rebuilds must go to the same path (`make install`) because the hook embeds
  the absolute binary path.
