# Claude Dispatcher

**A terminal cockpit for running a factory of Claude Code sessions across all your repos.** Dispatch work to many repositories at once, then see — at a glance, on one screen — what's working, what shipped, and the one thing that's actually blocked waiting on you.

It sits **above** your repos, not inside any one of them. Every dispatcher is a real `claude` session in its own tmux; the cockpit is a fast, keyboard-driven viewer over all of them.

![Triage: the whole fleet ranked, with a detail panel for the selected row](docs/triage.svg)

---

## Why

You can run ten Claude Code sessions. You can't watch ten terminals. The cockpit collapses the whole fleet into one triage surface: it surfaces the blocked and the finished, keeps the busy ones out of your way, and hands the terminal straight to tmux when you want to jump in.

- **One screen, the whole factory** — dispatchers, PRs, deploys, backlog, usage and velocity across every repo and product.
- **Real data, live** — dispatch records + `git` + `gh` + Linear + Azure Boards, refreshed on an fsnotify watch and a poll. Nothing is mocked.
- **Keyboard-first** — six lenses on the number keys, one key per action, a `:` command palette, and `?` for the map.
- **Done means live** — a feature stays open until it's actually deployed, not merely merged.
- **Tokens, not dollars** — built for a Claude subscription; usage speaks in tokens and effort.

## Six lenses

Switch with the number keys. Each lens is a different question about the same factory.

| | lens | the question it answers |
|---|---|---|
| `1` | **triage** | what is blocked, claims done, or waiting on me right now? |
| `2` | **products** | how is each product (many repos) doing? `enter` opens one |
| `3` | **backlog** | GitHub Issues · Linear · Azure Boards, in one list |
| `4` | **usage** | 5-hour and weekly consumption vs my learned limits |
| `5` | **decisions** | ADRs, and the decision sections written in each repo's own markdown |
| `6` | **velocity** | DORA + what actually reached production |

**Triage is the whole fleet, ranked.** One table of everything in flight — the dispatchers that want you above the ones getting on with it — and a detail panel for the row under the cursor: what it wants, where it stands, and the keys that answer it. The cursor holds its row across refreshes, so nothing moves under your hands.

**`f` narrows to what is running.** Everything working away unattended, with how long since each one last said anything. Nothing there needs you — which is why it sorts below what does. `f` cycles the other filters too.

![The fleet filtered to what is running](docs/working.svg)

**`h` is what has already finished.** Shipped, killed, or simply exited — every dispatcher whose session is over, newest first, on the table the live fleet leaves. `enter` resumes one: the worktree comes back if it was reclaimed and the conversation picks up where it stopped, so a session ending is never the end of it.

**With the fleet clear, the same screen becomes the prompt.** Give it a title — that is the branch — then the brief, which wraps for as long as it needs to be, and what "done" means. `tab` moves between the fields; `ctrl+d` dispatches.

![Dispatching straight from an empty fleet](docs/dispatch.svg)

**A product** is many repos — the portfolio roll-up, what is stale, and where the factory is stuck:

![The products lens](docs/products.svg)

**Press `a` to say which repos make up which product.** Mark repos with `space`, `enter` moves them into the selected product, `n` names a new one. It writes straight to `[products]` in your config, so the grouping every other lens uses is one screen away rather than a file you have to remember the syntax for.

![Assigning repos to products](docs/assign.svg)

**Velocity** — DORA delivery metrics beside what actually shipped, because a thousand commits that never merge isn't velocity:

![The velocity lens](docs/velocity.svg)

**One backlog** across GitHub Issues, Linear and Azure Boards — pick, then dispatch:

![The backlog lens](docs/backlog.svg)

## Usage limits, learned

There is no API for a Claude subscription's limits — so the cockpit **learns** them. It measures your 5-hour rolling and weekly consumption from the session transcripts, and treats the usage at each real rate-limit (429) as that window's cap: *assume that's the limit, until we sail past it and it's fine — then raise it.* The estimate persists and sharpens over time.

![The usage lens: 5-hour and weekly windows against learned caps](docs/usage.svg)

## Actions are real

Every act the table offers is wired to the live session — the key hints show only what the selected dispatcher can actually do:

- **`enter`** attach the tmux session at full fidelity (`Ctrl-\` to come back). Coming back **rechecks** rather than redraws: you have just spent minutes driving that session by hand, so the forge is read again instead of replayed from cache, and any session that died without getting a `SessionEnd` out stops being reported as working
- **`y`** on a PR waiting to merge: `gh pr merge --squash --auto`, then mark live. Elsewhere it marks the record shipped, and it is hidden entirely when the dispatcher has produced no commits
- **`x`** kill the session · **`s`** skip to the back of the table · **`u`** undo
- **`d`** open the prompt · **`f`** filter the table · **`enter` on a backlog ticket** dispatches it
- **`h`** the finished dispatchers, and **`enter`** on one resumes its session — `claude --resume` on the same transcript, in the same worktree, so it comes back knowing what it already did

## Install

Via Homebrew:

```sh
brew install innovology/tap/claude-dispatcher
claude-dispatcher init     # config + repo scan + status hook (asks first)
claude-dispatcher            # open the cockpit
```

Or from source (needs Go):

```sh
make install               # builds to ~/.local/bin/claude-dispatcher
```

Via Nix — try it, or install it into your profile:

```sh
nix run github:Innovology/claude-dispatcher            # no install
nix profile install github:Innovology/claude-dispatcher
```

Declaratively, the way you would pin any other flake-packaged tool. Add the
input:

```nix
inputs.claude-dispatcher.url = "github:Innovology/claude-dispatcher";
```

then take the package in a NixOS module:

```nix
{ pkgs, inputs, ... }:
{
  environment.systemPackages = [
    inputs.claude-dispatcher.packages.${pkgs.stdenv.hostPlatform.system}.default
  ];
}
```

or in home-manager (`home.packages`), or apply `overlays.default` and use
`pkgs.claude-dispatcher`. `inputs.nixpkgs.follows` is safe but not required:
`go.mod` asks for Go 1.26.5, so a nixpkgs older than that would fail to build.

The binary is wrapped with `git` and `tmux` on its PATH — as a fallback, so
your own copies still win — which leaves `claude` and `gh` yours to provide.
`init` records the profile path it was invoked through (`~/.nix-profile/bin/…`,
`/run/current-system/sw/bin/…`), not the `/nix/store` path behind it, so the
hook survives upgrades and you do not have to re-run it after every rebuild.

## Upgrading

When a newer release exists, the cockpit says so in the bottom-right corner.
Press **`U`**: it names the exact command, asks, hands the terminal to your
package manager so you can watch it work, then restarts itself in place —
same terminal, same tmux pane. Your dispatchers are tmux sessions and are not
touched by any of this.

The corner is fed by a cached check (a few hours old at most, so the cockpit
does not chatter at GitHub). Pressing `U` when it shows nothing does not repeat
that cache — it goes and looks, and if a release did go out in the meantime it
takes you straight to the same confirm. So `U` is always worth a press, and
"`v3.2.3` is the latest" is only ever said about a check that just ran.

Which command it runs is read from the running binary's own path, not guessed
from your OS:

| installed via | `U` runs |
|---|---|
| Homebrew cask (the tap) | `brew upgrade --cask claude-dispatcher` |
| Homebrew formula | `brew upgrade claude-dispatcher` |
| `nix profile install` | `nix profile upgrade claude-dispatcher` |
| scoop | `scoop update claude-dispatcher` |
| winget | `winget upgrade --id Innovology.claude-dispatcher` |

A **declarative** Nix install — home-manager, a NixOS module, a flake input —
is deliberately left alone: the version lives in a file you own, and an
imperative upgrade would either fail or install a second copy shadowing the
declared one. The footer shows the version gap and says `nix-managed` instead
of offering the key. Same for a binary we cannot place (`make install`, or a
copy you moved yourself) — it points at the releases page.

`claude-dispatcher version` prints both lines, which is the quickest way to
see why `U` is or is not on offer:

```
claude-dispatcher v3.1.2
homebrew cask · brew upgrade --cask claude-dispatcher
```

On Windows there is no exec-in-place, so `U` installs and tells you to start
the cockpit again rather than pretending to restart.

`nix develop` gives the full toolchain — Go, `golangci-lint`, `goreleaser`,
`tmux` — for working on the dispatcher itself; `nix flake check` runs the test
suite and the `gofmt` gate.

`~/.local/bin` must be on your PATH ahead of Homebrew's, or a `brew`-installed copy keeps winning:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
```

> The status hook embeds the absolute path of the binary that ran `init`. If you switch install methods, re-run `init` so the hook points at the new binary.

**Requirements:** macOS or Linux · `tmux` · `git` · the `claude` CLI · `gh` (for PR/deploy signals). Linear and Azure Boards are optional and off until configured.

## Windows

**Recommended: run under WSL2.** The full experience — real `tmux`, in-place attach, reply-without-attaching, the whole cockpit — depends on `tmux` as the process supervisor, and `tmux` is a Linux tool. Install a WSL2 distro (Ubuntu is fine), then use the **Linux** build exactly as documented above (`brew` via Linuxbrew, or `make install`). Inside the distro, `brew install innovology/tap/claude-dispatcher` is the same command as on a Mac and installs the same cask — Homebrew installs casks on Linux, and the tap's cask carries the Linux build alongside the macOS one, so `U` upgrades it in place there too. This is the recommended way to run the dispatcher on a Windows machine.

**Native preview (winget / scoop).** A native Windows build is published for early access once the publishers below are configured. It is honestly a preview: without `tmux` there is no shared session multiplexer, so **each dispatch opens in its own console window**, and **in-place attach (window focus) and reply (console input injection) are best-effort and still being hardened** — the `tmux`-grade session model for Windows is still being built. For the real cockpit experience today, use WSL2, and watch the cockpit as the native session model lands.

Once configured, install natively with:

```powershell
winget install Innovology.claude-dispatcher
# or
scoop bucket add innovology https://github.com/Innovology/scoop-bucket
scoop install claude-dispatcher
```

## Quickstart

```sh
claude-dispatcher init     # first run: writes config, scans for repos, installs the hook
claude-dispatcher            # open the cockpit
```

Press `1`–`6` to move between lenses. On triage, `j`/`k` move down the fleet, act on the selected row or `s` to skip it; `f` filters, `h` shows what has already finished, `d` opens the prompt. Elsewhere `j`/`k` move and `enter` opens. `:` is the command palette, `?` lists every key, and `,` opens settings.

## Settings

Press `,` in the cockpit (or `:settings`) to edit — written straight to `~/.config/claude-dispatcher/config.toml`:

- **scan roots** — the directories scanned (3 levels deep) for git repos
- **Linear API key** — turns on the Linear backlog source (or set `LINEAR_API_KEY`)
- **Azure org / project** — turns on Azure Boards (needs the `az` CLI; or set `AZURE_DEVOPS_ORG` / `AZURE_DEVOPS_PROJECT`)
- **weekly token budget** — optional; when set, the usage lens gauges against it, otherwise it shows learned caps and raw tokens

Environment variables override file values, so a secret can stay out of the file.

**Products** are edited on the products lens (`2`, then `a`), which writes the table below for you. You can still group them by hand in `~/.config/claude-dispatcher/config.toml`, using each repo's directory name:

```toml
[products]
acme    = ["acme-api", "acme-web", "acme-hq"]
bluefin = ["bluefin-core", "bluefin-web"]
```

Anything unmapped is grouped under `unassigned`, which is what a fresh install shows.

## How it works

- Each dispatcher is an interactive `claude` session inside its own tmux session (`disp-<slug>`), started on a `feature/<slug>` branch. Sessions survive cockpit restarts; the cockpit is a stateless viewer over `~/.local/state/claude-dispatcher/`.
- Status comes from **one** global Claude Code lifecycle hook (installed by `init` into `~/.claude/settings.json`). It maps events to states: working, needs you, blocked, done, exited.
- Commits are attributed to dispatchers by **provenance** — each dispatch records the SHAs its feature branch produced (base tip at launch → branch tip). No trailers in your git history.
- **Nothing disappears:** a session that ends — shipped, killed or simply exited — moves to history rather than off the screen. `h` on triage and the product panel's `H` tab both list them, and `enter` resumes one: its worktree is put back if it was reclaimed and `claude --resume` picks the same conversation up where it stopped.
- **Done means live:** when a PR merges, the tracker watches the repo's deploy workflow (auto-detected by name, or set in `[deploy_workflows]`) and flips the feature to done on a green run. Repos with no deploy workflow count merge as live.
- The layout is **responsive**: wide terminals tile into three panes, narrower ones collapse to essentials.
- **It opens on a boot screen.** The first load reads every dispatch record, asks tmux which sessions are still running, scans your roots and talks to the forge — seconds of work on a real portfolio. The opening screen shows each stage ticking off with what it found and how long it took, so the wait is legible instead of blank. Any key skips straight to the cockpit; the load carries on behind it.

## Keys

| key | action |
|---|---|
| `1`–`6` | switch lens (triage · products · backlog · usage · decisions · velocity) |
| `+` | dispatch new work — repo → feature → prompt |
| `j` / `k` · `g` / `G` | move · first row · last row |
| `f` | cycle the triage filter (all · wants you · needs a look · running · history) |
| `h` | history — every dispatcher whose session is over; `enter` resumes one |
| `enter` | attach the selected dispatcher's tmux session · open what is selected |
| `y` · `x` · `s` | ship (squash-merge) · kill · skip to the back |
| `d` · `u` | dispatch · put back the last thing you cleared |
| `,` · `:` · `?` | settings · command palette · all keys |
| `U` | upgrade to the published build and come straight back |
| `q` | quit |


## Development

`make check` runs build, vet, lint (golangci-lint), and the race-enabled test suite — the gates CI runs on every PR.

## Releasing

Merging a PR into `main` cuts the next patch release automatically (goreleaser builds macOS/Linux tarballs, Windows `.zip`s, and the Homebrew cask). Label the PR `release:minor` / `release:major` (or `skip-release`) to control the bump — the PR template reminds you. A `release: minor` line or `[skip release]` in the merge commit still works as a fallback; doc-only merges skip on their own. Releases need a `HOMEBREW_TAP_TOKEN` secret (a fine-grained PAT with contents:write on `Innovology/homebrew-tap`):

```sh
gh secret set HOMEBREW_TAP_TOKEN -R Innovology/claude-dispatcher
```

Without it the Release workflow succeeds but tags and publishes nothing, so a merge never half-releases.

### Windows publishing (optional)

The `scoop` and `winget` publishers are wired into `.goreleaser.yml` but stay a no-op until you set their tokens — the existing macOS/Linux + Homebrew release keeps working unchanged with only `HOMEBREW_TAP_TOKEN`. To turn on native Windows publishing, set up each publish repo exactly like the Homebrew tap:

- **scoop** — create an `Innovology/scoop-bucket` repo, then add a fine-grained PAT with contents:write on it:

  ```sh
  gh secret set SCOOP_BUCKET_TOKEN -R Innovology/claude-dispatcher
  ```

- **winget** (optional) — fork `microsoft/winget-pkgs` to `Innovology/winget-pkgs`, then add a PAT with contents:write on the fork (goreleaser opens the manifest PR against it under publisher `Innovology`):

  ```sh
  gh secret set WINGET_TOKEN -R Innovology/claude-dispatcher
  ```

Each publisher's `skip_upload` is templated on its token, so an unset token means the manifest is generated but never pushed — a release can never half-publish.

## Troubleshooting

- **Everything stuck on "launching"** — the hook isn't firing. Re-run `claude-dispatcher init`; confirm `~/.claude/settings.json` points at the installed binary.
- **`needs you` vs `blocked` lag** — those rely on the `Notification` hook matchers (`idle_prompt` / `permission_prompt`); `Stop` covers turn completion regardless.
- **Backlog empty** — set a Linear key / Azure org in settings, and check `gh auth status` for GitHub Issues.
- Rebuilds must go to the same path (`make install`) because the hook embeds the absolute binary path.
