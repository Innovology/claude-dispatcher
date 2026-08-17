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
      still fine and reuses the worktree left behind. "Live" is two facts
      (`liveDispatch`), and the second is the one that bites: a live tmux
      session is **not** a live dispatcher, because `launchCommand` ends
      `; exec ${SHELL}` on purpose and every finished dispatch keeps its
      session as an idle login shell. `supervisor.SessionIdle` tells them
      apart; its *unknown* (a backend that cannot see into a session) is
      neither, and keeps the refusal rather than guessing.
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
- **The forge bill counts repositories, not pull requests — and stops when
  GitHub says stop.** Measured, one idle cockpit spent 5,409 GraphQL requests
  an hour against a limit of 5,000: it exhausted the quota by itself and took
  the dispatchers' `gh` calls and the human's down with it. Three rules keep
  it there. *Ask a repo once* — `gh.RepoPRs` gets every open PR's check
  rollup and review posture in one `pr list`, where the cockpit used to ask
  `pr checks` and `pr view` per PR. *Do not re-read history* — a PR the open
  list does not contain has merged or closed and cannot change, so it is held
  for `gh.SettledTTL`, as is the branch of an exited dispatcher that has no
  session left to raise one. *Never poll below the poll* — every cache TTL is
  at least `cockpit.refreshEvery`, or the rebuilds between polls (one per
  dispatch-record write) pay for the same answer again; a test holds the two
  the right way round. And the refusal is itself a signal: the first
  rate-limit error parks every read in `internal/gh` until the window resets,
  spawning nothing, because a collector that degrades to "no signal" cannot
  otherwise tell a quiet portfolio from a locked-out client. The cockpit says
  so — "—" in every check column is a claim about the repositories when it is
  a fact about us.
- **`U` is the upgrade key and nothing else's; undo is ctrl+z.** Shift is not
  a namespace. `handleKey` resolves `U` globally, before any lens is asked, so
  a lens that wants its own capital U never sees the key — the assignment
  editor's "start over" was exactly that, dead since the upgrade key landed,
  and is ctrl+u now. And undo, the key you hit fastest and without looking,
  must not be one slipped shift from upgrading the machine in place, so it is
  a chord. That also frees `u` for the editor's unassign, which the global
  undo used to steal whenever a triage act had left something undoable.
- **Amber is a claim about the human, not about CI.** The triage table used to
  mark a *running* dispatcher amber (◆, "green in ci and still not merged")
  the moment its PR's checks went green — while the session was busy doing
  manual verification and had asked nobody to merge anything. The tier could
  only ever fire on that false positive: the state it meant to name — green PR,
  session stopped — is a "review" queue row by construction (floorState), so
  every dispatcher truly waiting on a human was already in the table's top
  half. The ◆ rank is gone; a running row keeps "green, unmerged" as SIGNAL
  text and nothing louder, and the merge ask is claimed on the queue row the
  record becomes when the session actually stops. The "needs a look" filter,
  which matched exactly that tier plus the queue it duplicated, left with it.
- **Decisions are read where they were written, never invented.** The
  DECISIONS lens has two sources: an adr-tools folder, and a heading that
  names a set of decisions in the repo's own markdown (`CLAUDE.md`,
  `DECISIONS.md`, `ARCHITECTURE.md`, `README.md`) — this section is one, and
  the lens reads it. Nothing writes a record: not the cockpit, not a
  dispatcher. A commit or a PR title is not promoted to a decision, because
  inventing records is worse than an empty pane. It shipped reading only
  `doc/adr/`, which no repo in the fleet keeps, so the lens was empty for
  every repo while decisions sat in plain sight one file away.
- **Parking is an annotation, never a status.** A session reaches a point
  where it asks something the human cannot answer right now; `p` on that queue
  row shelves it under a required, human-typed reason
  (`ParkedReason`/`ParkedAt` on the record) and it drops to a *parked* group
  at the bottom of the fleet — its own divider, its own rank between running
  and history, out of the "want you" counts. It is not a `Status` because
  status belongs to the hooks: a parked status would be flipped back by the
  next lifecycle event, or need a guard everywhere one is applied. The shelf
  therefore survives everything the machine does — including a reboot killing
  tmux; a parked row whose session died offers resume instead of attach — and
  clears on exactly three human acts: `p` again (unpark), a kill (`x` is
  abandonment, not shelving), and a prompt reaching the session
  (`UserPromptSubmit` in hookcmd), because someone just answered the question
  it was parked on. Shipping retires it like any other row: "live" outranks
  the shelf. Parked rows sit out the `s`-rotation ordering so they can never
  be dragged above the live table. The full record is
  `docs/adr/0001-parking-is-an-annotation-never-a-status.md` — this repo's
  first ADR, in the folder the DECISIONS lens reads.
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
- **The dispatch form's MODE is Claude Code's permission mode, not a
  sentence about one.** A dispatch is launched with `--permission-mode` —
  `auto` (takes its own edits and safe commands), `manual` (asks before each
  step) or `plan` — recorded on the dispatch, and passed again on `Resume`,
  so a reopened dispatcher comes back the way it went out. Auto is the
  default: a dispatcher is by definition work sent somewhere else to happen,
  with nobody sitting on its permission prompt. The form's closing prompt
  sentence stays, because the flag says what claude may do without asking and
  the sentence says how far to take the work — "may edit without asking" is
  not "commit, push and open the PR". This shipped as a two-position AUTO
  switch with nothing behind it: whatever it said, the session opened in
  whatever the human's own Claude Code defaults to and stopped on its first
  permission prompt unattended. A claude too old to know a mode's current
  spelling is given the older one (`acceptEdits`/`default`) and one with no
  such flag gets none, because a rejected flag is not a degraded session —
  it is a launch that never happens.
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
- `internal/dispatch` — branch + tmux + record creation, and `Resume`: a
  finished dispatcher's session reopened with `claude --resume <session id>`
  in its own worktree (rebuilt if it was reclaimed). A session ending never
  loses a dispatcher — triage's `h` and the product panel's `H` tab list every
  finished one and resume it.
- `internal/cockpit` — Bubble Tea cockpit; responsive tiling breakpoints at 110
  and 170 columns (more panes on wide screens, never one ballooned view).
  `boot.go`/`boot_view.go` are the opening screen: a console-boot sequence over
  the first `loadSnapshot`, which reports each stage as it runs. Every line is
  a real stage and every figure is what it found — the list is a description of
  that function, not decoration over it, so a stage added there gets a step here
  (a test asserts the two sets match). Any key skips it; the load continues.
  Panels get a height as well as a width: the product panel's history tab is as
  long as the product's past, and it is the lines *under* the list — the session
  id and the key that reopens it — that a fixed-height column drops first, so it
  windows around the cursor and counts what it hides rather than stopping mute.
  `prodDay` normalises to **local** before truncating: forge timestamps are UTC
  and the clock's are local, and a day compared across the two is never equal.
- `internal/ship` — shipping stats (Claude-stamped = Co-Authored-By trailer).
- `internal/effort` — the hand-coding equivalent: how long a senior developer
  would have taken to write a diff by hand. The ONLY figure in the product
  that is a model rather than a measurement, so every screen showing it says
  so — an `≈` on the figure, and the rate (`effort.LinesPerHour`, one named
  constant) printed beside the velocity total. Read once per load off the
  provenance diff collectFloor already ran, published on `snapshot.effortBy`
  keyed by feature, and shared by triage, history and velocity so no two
  screens can quote different hours for one branch. A feature missing from that
  map is one whose diff could not be read, which is NOT the same as one that
  wrote nothing: totals skip it and velocity says how many of its live features
  it could price. History rows take the figure from that map and never run a
  diff of their own — `fleetPastRow` stays the cheapest row there is, so a
  machine with hundreds of finished dispatchers still polls in constant work.
- `internal/version` — the build's version (stamped by goreleaser via
  `-X claude-dispatcher/internal/version.Version`), the cached, best-effort
  check for a newer release, and `Detect()`: which package manager installed
  this binary, read from its own resolved path, and the one command that
  upgrades it. `U` in the cockpit runs that command and re-execs. A
  declaratively-installed Nix build is never upgraded imperatively — only an
  entry in the imperative profile's `manifest.json` proves it may be.
  - **A cached answer belongs to the build that recorded it.** `U` upgrades in
    place and re-execs, so the build that comes back would otherwise read a
    cache naming the release it was upgraded *from* — "latest v3.1.3" read by
    v3.2.3 compares as "you are current" and hides everything published since,
    for the rest of the TTL. The cache carries the `Build` that wrote it and a
    different one re-asks; that is also why a failed check only carries forward
    an answer this build recorded.
  - **`U` with nothing on offer checks rather than reciting the cache**
    (`version.Recheck`). The ambient answer is hours old by design, and a human
    presses `U` precisely when they think it is behind. Finding a release goes
    straight to the confirm — they pressed it to upgrade, not to be told.

## Build
`make build` / `make vet` / `make install` (binary to ~/.local/bin — the init
hook embeds the absolute binary path, so reinstall to the same path).
