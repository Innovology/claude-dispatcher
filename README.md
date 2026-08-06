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

```sh
make install                  # builds to ~/.local/bin/claude-dispatcher
claude-dispatcher init        # config + repo scan + hook install (asks first)
claude-dispatcher             # open the cockpit
```

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

## Roadmap (deliberately deferred from the MVP)

1. Automatic done-signal from GitHub Actions deploys ("done means live").
2. PR tracking via `gh`; deploys on the shipping strip.
3. Portfolio roll-up: effort and token spend per product (tokens, not
   dollars — subscription billing).
4. Feature history: jump back into a shipped feature and rehydrate its
   sessions (`claude --resume`) and branch diff.
5. Adopting sessions started outside the cockpit (the hook already logs
   them to `events.jsonl`).
6. Diff pane on ultrawide layouts.

## Troubleshooting

- **Everything stuck on "launching"** — the hook isn't firing. Re-run
  `claude-dispatcher init`; confirm the entries in `~/.claude/settings.json`
  point at the installed binary path.
- **Statuses lag** — `needs you` vs `blocked` relies on the `Notification`
  hook matchers `idle_prompt` / `permission_prompt`; `Stop` covers turn
  completion regardless.
- Rebuilds must go to the same path (`make install`) because the hook embeds
  the absolute binary path.
