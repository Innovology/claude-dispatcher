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
    - **A repository is its common dir, and its name is not its folder.**
      Discovery called any directory holding a `.git` a repo and named it for
      that directory. A `.git` marks a *checkout*, and one repo has as many as
      it has worktrees: measured on a worktree-heavy machine, **47 "repos" for
      26 repositories** — `kolchurin.dev` thirteen times, `playerpulse` eight,
      and three unrelated repositories all called `main`. Name is the identity
      everywhere (`[products]`, `ProductFor`, `discByName`, the dispatch
      worktree path), so three repos shared one key; and `gh` keys its search
      results by the *GitHub* name, so six repos cloned into a folder that is
      not their name (`ord-ai-n` → `ordain`, `prototype-player-app` →
      `Player-App-2`) had every issue and PR landing nowhere. The bare layout
      was invisible on top of that: `<project>/.bare` carries no `.git` of its
      own, so the project dir fell through to the depth check and was cut with
      all 69 checkouts inside it. So identity is the **git dir every checkout
      shares** — `<clone>/.git`, `<project>/.bare` — read from files, never a
      git process, since a discovery runs on every load; the **name comes from
      the origin remote**, folder only for a repo with no remote; one checkout
      is **canonical**, because twenty call sites stand *inside* `Repo.Path`
      and a bare repo has no working tree of its own; and the worktree list
      comes from **git's registry**, not the scan, which skips hidden dirs and
      so would report nothing for the common `.worktrees/<name>` layout. It is
      an extension, not a replacement: the walk is unchanged, a plain clone
      resolves to itself byte-for-byte as before, and unreadable metadata falls
      back to being a repo of one named for its folder. Evidence of a repo buys
      the one level that reaches its checkouts; the budget itself does not grow,
      and a container below the limit still wants a nearer root. Nothing
      rewrites the human's `[products]`: a rename can orphan an entry, and it
      falls to "unassigned" where the editor reassigns it, because a migration
      that guessed would be editing the file this tool calls the source of
      truth. `p` on a row in the assignment editor is where the detail went —
      the absolute path it acts in and every checkout git knows of, the acting
      one marked. **And the automatic choice is a guess worth correcting**:
      three spellings of a trunk is narrow, and a repo that merges into `dev`
      matches none of them, so its row would be read from a branch nobody
      ships. The fold therefore takes the keyboard (`j`/`k` move its checkouts,
      `enter` pins one into `[checkouts]`, `esc` lets go without closing, `p`
      folds); `enter` on the one already pinned clears it, because nothing else
      removes a pin and "choose automatically again" has to be reachable; and a
      pin naming a path git does not list as a worktree of that repo is ignored,
      since a pin chooses between the checkouts that exist rather than inventing
      one. A pinned checkout is **not** a root branch — it decides what the row
      reads, while which branch a feature is cut from stays with the remote's
      default and the human's `Root` (see the root-branch decision below). Full
      record: `docs/adr/0017-a-repository-is-its-common-dir.md`.
  - *Worktree* is per-dispatch isolation: each dispatch gets its own git
    worktree of its repo under
    `~/.local/state/claude-dispatcher/worktrees/<repo>/<slug>`, so concurrent
    dispatches — and the human — never fight over one checkout. `x` removes a
    clean worktree; a dirty one is kept for inspection. (Supersedes the
    original "multi-repo, not multi-worktree" call, reversed the next day
    after two sessions collided in one working copy and a commit landed on
    the wrong branch.)
    - The feature branch is cut from the repo's **default branch as the
      remote sees it**, after a best-effort fetch — never from the repo's
      HEAD. Git's default would inherit whatever branch the human left
      checked out, so a dispatch would silently start on top of an unmerged
      feature and carry it into its own PR. Created `--no-track`, or
      `git push` would refuse a branch whose upstream has a different name.
      Which branch that is comes from the **remote**, not from
      `origin/HEAD` — see the root-branch decision below.
    - **One live dispatch per feature name.** The name is the key: the
      worktree path and the cockpit's record map are both keyed by it, so a
      second concurrent dispatch of a live name would put two sessions in one
      checkout. Launch refuses it; re-dispatching a *finished* feature is
      still fine and reuses the worktree left behind. "Live" is three facts
      (`liveDispatch`), and none of them is the tmux session on its own: it
      outlives its claude by design, because `launchCommand` ends
      `; exec ${SHELL}`, and on a real machine the claude REPL outlives the
      work too, sitting at its prompt for days after the merge. The record's
      status is the hooks' account of whether the dispatcher is still going;
      the live session catches a record they could not correct (a SIGKILL, a
      reboot); `supervisor.SessionIdle` catches the session that outlived its
      claude, and its *unknown* (a backend that cannot see into a session) is
      neither, keeping the refusal rather than guessing.
  - *Product* is the grouping lens: the cockpit list and the dispatch form's
    repo picker group by the `[products]` config, most urgent group first;
    unmapped repos fall under "other". No separate group concept.
- Sessions run as interactive `claude` inside per-dispatch tmux sessions
  (`disp-<slug>`) on their repo's own tmux server; tmux is a hard dependency
  and the process supervisor. A session is addressed by the pair
  (`supervisor.Session`: socket + name), never by name alone. The cockpit is a
  stateless viewer — "jump in" hands the terminal to tmux.
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
- **A ghost cannot clear itself, so every load looks.** That sweep ran in one
  place — `recheckCmd`, the reload after a jump-in — so a record whose session
  had been taken away claimed *working* for as long as the cockpit stayed open
  and its row went on offering ⏎ attach, which found no session, said so and
  changed nothing: the only thing that would have retired it was coming back
  from an attach that could never happen. It is swept on every `loadSnapshot`
  now, in the boot stage that already asks the supervisor what is running (and
  reports what it retired), off ONE `supervisor.Sessions` listing rather than a
  probe per record, because a load happens on every poll and every state-file
  change. The listing only screens: a record it does not name is asked about
  directly before anything is written, since a failed listing comes back empty
  and an empty listing must never retire a fleet. ⏎ on a ghost runs the same
  sweep on that record and points at history, where ⏎ resumes — the human
  pressing the key is how a ghost gets found. Reported from WSL, where the
  distro shuts down with its last console and takes tmux with it; a suspended
  laptop or a reboot makes the same row anywhere. The full record is
  `docs/adr/0004-a-ghost-cannot-clear-itself.md`.
- **The newest answer wins, not the last one back.** A dispatch went on the
  table and vanished seconds later — sometimes returning, sometimes not, and
  on an empty fleet leaving the blank dispatch form, which reads as "that did
  not work". Loads were racing: an fsnotify event, the poll, a finished action
  and a jump-in each started a full snapshot load of their own however many
  were already out, and `applySnapshot` published whichever came back last. A
  load is not quick — measured on a 94-record portfolio, 4.5s warm and 61s
  cold — so when a dispatch lands, one that read the records *before* it
  existed is routinely still in flight, and it returns carrying a fleet with no
  such dispatcher in it. The placeholder that covers that window
  (`pending.go`) then retired on it, because it retired on any snapshot at all
  once the launch had reported: the rule meant "a snapshot has since read the
  records" and tested "a snapshot has since landed". So: one load at a time
  (`model.requestLoad`, `load.go`), requests collapsed into the next one rather
  than started beside it — with a 5-minute escape so a wedged load cannot
  freeze the data for good; every load numbered, and a snapshot older than the
  one already on screen dropped rather than published; and a settled
  placeholder retired only by a snapshot whose `recordsAt` (stamped at
  `state.LoadAll`, not at the end of the load) is after the launch settled.
  Reproduced and fixed against the real binary, capturing the pane once a
  second through a dispatch. Full record:
  `docs/adr/0007-the-newest-answer-wins-not-the-last-one-back.md`.
- **A shell in the pane is not an idle session.** `tmux.SessionIdle` decides
  whether claude has ended in a session, and `Resume` *kills* the session on
  that answer while `liveDispatch` lets a second dispatch take the name. It
  read `#{pane_current_command}`, which cannot answer it: a session runs
  `<shell> -c "… claude …; exec ${SHELL}"`, and a non-interactive shell has no
  job control, so claude shares the shell's process group and tmux reports the
  leader — "zsh" — throughout. Measured on a working dispatcher: pane_pid
  51122 reporting `zsh` with claude alive as its child. So every live session
  read as idle: ⏎ on a running dispatcher killed the claude it was reopening,
  and two dispatches of one name landed in one worktree (`dev-1812` and `dev
  1812`, 88 seconds apart, same branch, the second dead in 26 seconds). It
  asks what is running *under* the pane's shell now (one `ps -ax -o
  pid=,ppid=`; an unreadable table is "unknown", never "idle"), and a pane tmux
  can name a real command for is still taken at its word. `liveDispatch` reads
  the record's status alongside it, because a finished dispatch keeps its
  claude too — the REPL sits at its prompt for days after the merge — and the
  session probe alone would refuse every name that had ever completed, with no
  kill key on a history row to clear it. Full record:
  `docs/adr/0008-a-shell-in-the-pane-is-not-an-idle-session.md`.
- **A dispatch runs in its repo's environment, not the cockpit's.** Two repos
  in one product can need different versions of the same binary, and the
  isolation this product had was git isolation: a worktree per dispatch stops
  two sessions sharing a checkout and says nothing about what `go` or `psql`
  resolve to inside it. What decides that is not what the client/server split
  suggests — **a pane inherits the environment of the CLIENT that asked for
  it**, not the server's and not the environment the server was born in
  (measured on tmux 3.7b: a server started with `PATH=/plain-server-path`,
  asked by a later client carrying `PATH=/nix/store/FAKE-flake-bin`, spawns the
  pane with the client's). PATH is also the one variable that does not stick to
  a session: everything else captured at creation persists, PATH is re-taken
  from whichever client spawns each new window. So the cockpit — one process
  spanning the whole portfolio — was deciding what every dispatcher could see,
  and would go on deciding it for every pane the human opened after jumping in.
  A repository brings its own now: its **server** (`Repo.Socket`, the `-L`
  name, defaulting to the repo's own name — so each repo is its own server with
  nothing configured; `[sockets]` names one for a human who already keeps
  per-project servers, since a socket name is chosen outside the repository and
  no reading of it finds one) and its **environment** (`Repo.Env`, the command
  the client runs under — a checkout with a `flake.nix` *on a machine with nix*
  is launched under `nix develop`, the second half no more optional than a
  model alias we cannot vouch for). The prefix wraps the **client**, never the
  session's command: both put the repo's binaries on PATH and only this one
  leaves the pane running the plain `<shell> -c "… claude …"` that
  `SessionIdle` reads, since a pane whose command is not a shell is taken at
  its word as busy for ever — ADR 0008 with the sign flipped. The wrapped
  client runs **from the session's directory** (the worktree at launch, tmux's
  `session_path` at attach), because `nix develop` finds its flake where it
  stands: run from the cockpit's cwd it failed from `~` and, worse, lent a
  dispatch whichever flake repo the cockpit was opened in. It is applied to
  the two calls that spawn a pane and nowhere else, because on the Server it
  would put a nix evaluation behind every poll. The name alone stops being an
  address: `supervisor.Session` carries `(socket, name)` and both go on the
  record (`TmuxSocket`, `EnvCommand`) beside Mode and Root, so resume reopens
  where it ran rather than where config now says. `AttachSwitches` becomes "is
  this session on *my* server" — switch-client cannot cross servers, and the
  nested attach that results exits on the way home, which is the shape the
  caller already waits for. The sweep asks each server once rather than the
  fleet once, and still probes any record its server did not name. And the
  socket is **not** the grouping: a product is a lens over records, which carry
  their product already, so a product's sessions list across as many servers as
  it has repos. Full record:
  `docs/adr/0014-a-dispatch-runs-in-its-repos-environment.md`.
- **The sessions you started yourself.** A repo dispatches onto its own server
  (above), and a human who works that way has servers of their own on the same
  machine — one per project, started by hand long before any dispatcher. That is
  real work in these repos and the cockpit could not name one of them: it only
  asked about sockets it had a record for. Servers are found by looking where
  **tmux** puts them (`${TMUX_TMPDIR:-/tmp}/tmux-$UID`), because the sockets in
  question were made by somebody else's tmux and there is nowhere else to learn
  of them. A socket *file* is not a server — it outlives its process, and a
  reboot's leftovers sit on an ext4 `/tmp` for months — so every name found is a
  candidate and only asking settles it; asking is safe because `list-sessions`
  on a dead socket errors rather than starting one, which is what makes
  enumeration possible at all. A record claims a session by its **whole
  address** and at **any status** (a finished dispatcher's session outlives its
  claude by design and is still its own). What is left is attributed by the
  socket when it names a repo — the launcher's own rule, so exact rather than
  inferred — and otherwise by the directory it runs in, matched against the
  repo's checkouts by path *segment* and longest-first, since `/src/app` must
  not swallow `/src/app-2` and one repo here has seven worktrees named exactly
  that way. The directory is the case that matters: a server a human named
  themselves matches no repo, and nothing but the path says whose it is. They
  are **not dispatchers and never shown as ones** — no feature, no status, no
  effort figure exists for them, so the row is the three facts that do and
  `enter` to jump in, with no kill key for something this cockpit did not start.
  A session in no known repo is dropped; a repo in no product goes to the
  `unassigned` bucket the portfolio already keeps, which is the difference
  between using a bucket and inventing one — and dropping those instead made the
  whole tab invisible on a machine with no products assigned, which is every
  machine on its first run. Full record:
  `docs/adr/0015-the-sessions-you-started-yourself.md`.
- **A colour is a role, and the switch is news.** The cockpit paints only
  foregrounds onto the terminal's own ground, and it painted the dark design's
  hexes whatever that ground was: on a light terminal (NixOS/niri, ghostty
  following the desktop) the pale greys were pale grey on white, and a selected
  row was a slab of `#18222f`. Every `c…` constant is a **role** now, and a
  **theme** (`theme.go`) is the table that says what hex it is; `fg`/`paint`
  resolve at render time and `setTheme` empties the role-keyed style caches.
  `dark` is the design verbatim. `light` turns it over rather than inverting
  it: the emphasis ramp keeps its order, each hue takes its 700-ish shade so
  amber still means "wants you", and rule and selection come apart again. A
  test holds every theme to every role and to WCAG AA on its own ground. Config
  `theme` is the mode — `"system"` (or no line) follows the switch live, a
  theme's name holds it — cycled on enter in settings, applied without a reload.
  Following it takes **two reporters, because neither is enough**: the OS is
  polled every 2s (`internal/appearance`: the freedesktop portal via
  `busctl`/`gdbus`, `AppleInterfaceStyle`, `AppsUseLightTheme`; "no preference"
  is unknown, and a machine with nothing to ask stops polling), and the
  terminal is asked with mode 2031 / `CSI ? 996 n` for `CSI ? 997 ; 1|2 n`,
  which tmux 3.6+ relays and ghostty sends — instant where spoken. Bubble Tea
  v1 delivers that as its unexported unknown-CSI slice, matched by shape then
  exact content; the enabling write is safe beside the renderer because both go
  through one `*os.File` write lock, and is re-sent after a jump-in. **The
  newest change wins**: the OS answer counts only when it differs from its own
  last answer, so a terminal that said "dark" is not overruled every poll. The
  first frame is decided before the program starts (OS, else the terminal's
  background once), so boot never flashes the wrong theme. A third-party theme
  is one more table; which one `system` picks per side is one lookup
  (`themeForAppearance`) waiting for a key. Full record:
  `docs/adr/0016-a-colour-is-a-role-and-the-switch-is-news.md`.
- **A dispatch that did not happen is a thing that happened.** Reported as
  "when the prompt is massive the dispatcher seems to just disappear", then
  "it's not just long prompts — that last one failed with a one line prompt".
  Two causes, and the reason they were indistinguishable is the worse defect:
  `Launch` returns before `state.Save` for a refusal or any `ensureWorktree`
  failure, so there was **no record, no worktree, no session and no log line** —
  the entire account was one footer notice the next keypress replaces, and
  `dropPending` *deleted* the placeholder row on purpose, leaving a table that
  looked exactly as it had a second earlier. On an empty fleet the lens then
  falls back to the dispatch form, so submitting put the blank form back up.
  So: **every attempt is audited** (`DispatchAsked`/`DispatchLaunched`/
  `DispatchFailed` in the event log, with the feature, the repo and the reason
  verbatim — not lifecycle events, nothing derives status from them and
  `velDwellState` leaves them unbilled); **a failed launch keeps its row**,
  SIGNAL "did not start", the launch's own words in the detail lead, `x` to
  dismiss, and no snapshot retires it because what it reports is that there is
  nothing for a snapshot to find; and **a session that would not start keeps
  its record**, carrying the supervisor's words instead of the "tmux launch
  failed" that stood for every cause there is. The long-prompt cause was the
  prompt travelling *inside* the launch command, which is one argument to the
  supervisor and therefore capped: tmux carries a client command in a single
  imsg (`MAX_IMSGSIZE`, 16KB) — measured on tmux 3.7b, 16313 bytes went through
  and 16314 came back "command too long" — and cmd.exe caps at 8191 while
  flattening every newline, because a cmd command line cannot have one. It
  travels as a file now (`state/prompts/<id>.txt`, read by the session's own
  shell), leaving a fixed ~300-byte command; resume's opening message goes the
  same way under its own name, so reopening never overwrites the record of what
  was originally sent. `MaxPromptBytes` (100KB) is then the only ceiling and the
  refusal is ours, said at the moment of asking, because past the kernel's cap
  on one argument the exec fails *inside* the session and no hook ever fires.
  The `+` form's `CharLimit = 500` went with it: a silent truncation and a loud
  refusal are not two settings of one dial. Full record:
  `docs/adr/0009-a-dispatch-that-did-not-happen-is-a-thing-that-happened.md`.
- **`origin/HEAD` is a cache, not an answer — and the root branch is a
  choice.** Reported as "the dispatcher attempts to fork a dead branch", with
  the launch that did it sitting in the event log 0009 had just added:
  `git branch feature/validate-json-ld from origin/dev: fatal: not a valid
  object name: 'origin/dev'`. `refs/remotes/origin/HEAD` is written **once, by
  `git clone`**, and no fetch ever updates it, so a repo that has moved its
  default branch — the trunk-based migration every one of these repos has had —
  keeps naming the branch it left behind; `symbolic-ref` reports that name
  whether or not anything is there, and `baseRef` handed it straight to
  `git branch`. Measured on the reporting repo: origin/HEAD says `dev`, `dev`
  was archived and deleted, and `ls-remote` says the remote's HEAD is `main`.
  Two failures, and the quiet one is worse — the launch **dies** where the old
  branch was pruned locally, and **succeeds on a dead branch** where it was
  not, because a plain fetch never deletes a remote-tracking ref: the
  dispatcher then spends its whole run on top of a retired base and nothing
  anywhere says so, `BaseSHA` being a commit and a commit not saying which
  branch it was the tip of. So the default is read from the **remote**
  (`ls-remote --symref`), and every candidate after it — the local cache, the
  conventional names, the checked-out HEAD — must **resolve to a commit**
  before it is offered to git; nothing rewrites the human's `origin/HEAD`,
  because resolving a base reads, it does not edit somebody's repo. And the
  human gets to say: **ROOT** is on both dispatch forms — a filtered list step
  on the `+` overlay, a typed field on the dx form (these repos carry 170-odd
  branches each, which is a list you type at, not one you cycle), refusing an
  unknown name *on the form* with the near misses named. Its default names **no
  branch**, meaning "ask origin at launch", exactly as `ModelDefault` means
  "pass no flag" — a form's only local answer is the very cache this is about.
  A root named for a branch that already exists is refused rather than agreed
  with and dropped, since a branch that exists is not re-cut from anywhere; the
  ref it *was* cut from goes on the record (`Root`, e.g. `origin/main`). Stale
  remote-tracking refs are deliberately **not** pruned: `--prune` edits the
  human's repo and can strand commits, so the form may offer a branch origin
  retired and the launch refuses it by name. Full record:
  `docs/adr/0010-origin-head-is-a-cache-not-an-answer.md`.
- **A dispatch that ends is not a dispatch that goes away — and history is
  ordered by work, not by our own writes.** Reported as "the history needs to
  be ordered by last access/last action" and "the triage should hold
  dispatchments until they are dismissed; they should not auto-dismiss", which
  is one complaint from both ends. `collectFleet` sent every finished record
  straight to history, which is not in `fleetAll`, so a dispatcher that
  finished left the triage table on the next poll: the human watching the one
  screen they watch saw a count go from 4 to 3 and a line disappear, with
  nothing saying which one or how it ended — ADR 0009's defect moved to the
  other end of the run. So a finished dispatcher **holds its row** until `x`
  takes it off: its own rank under a `finished` divider, glyph ✓, the SIGNAL a
  history row carries, `⏎` resume and `o` open pr beside a **dismiss** that is
  not a kill (there is nothing left to kill), and its own headline count,
  because folding it into "running clean" hides a finished dispatcher inside
  the number that means everything is fine. No snapshot retires it, for the
  same reason none retires a failed launch's note. The dismissal is written to
  the record (`DismissedAt`, an annotation like parking, never a status) — kept
  in the model it would come back on the next load, and come back for all ~170
  finished records the moment the cockpit restarted. What is held is `Held()`:
  finished, stamped with `FinishedAt`, not dismissed. `state.Stop` stamps
  `FinishedAt` **at the transition and nowhere else**, which is also the whole
  of the migration — a record that ended before this build has no stamp, so it
  was never held, and shipping this cannot flush a year of history onto the
  fleet. `Save` clears both whenever the status is not a finished one, an
  invariant rather than a guess, and that is what makes a resumed dispatcher
  come back clean. And history is ordered by `fleetActed`: the transcript's own
  mtime — the only clock the session writes — falling back to the record only
  when there is no transcript. `UpdatedAt` cannot answer it, because `Save`
  stamps it on every write and a finished record goes on being written for the
  rest of its life by sweeps, the tracker and forge reconciliation. Measured on
  the reporting store: a dispatcher whose transcript had not moved in seven days
  carrying an `UpdatedAt` from that morning, and five that ended on four
  different days sharing one to the second because a sweep wrote them in a loop.
  `fleetMoved` keeps its `max` for live rows — a hook save there is real
  liveness. Full record:
  `docs/adr/0012-a-dispatch-that-ends-is-not-a-dispatch-that-goes-away.md`.
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
- **The Linear backlog is one token per product, and team scope belongs on the
  token.** A single `linear_api_key` was the whole of the Linear source:
  everything one token could see, in one undifferentiated list, tagged with no
  product — so two products in two workspaces could only ever show one of them.
  `[linear]` maps a product to the token its backlog is read with
  now, and every ticket carries the product its read came back on; a product
  with no entry reads with the unscoped key, and a config naming none is the one
  unscoped read it always was, so nothing that predates this has to know about
  it. There is deliberately NO team filter here. A Linear key is granted its
  teams when it is created, so two products sharing a workspace get a
  team-scoped key each and the split is one the API enforces — a filter we
  applied instead would be narrowing a list we had already been handed, and
  narrowing it after `first: 50` had chosen which fifty. Two products naming one
  token are one read, and that read names **neither** of them: a shared token
  cannot say which product an issue it returns belongs to, and crediting
  whichever product sorts first would be stable and wrong — every other sharer's
  tickets filed under a product that is not theirs, which is the "—" rule again.
  The unscoped key is likewise a read of its own, never a fallback the scoped
  ones switch off: it is another workspace as easily as the same one, so naming
  one product's token must not silently empty a backlog that had been reading
  for months. It goes last, so a scoped read keeps the tag on any overlap. The
  reads go out together and merge in the order they were named, so what a load
  produces never depends on which workspace answered first. Tickets are deduped
  on the issue **id**, not its identifier: "ENG-124" is unique inside a
  workspace and this is a list of several, so dropping the second ENG-124 would
  lose a ticket nobody ever saw. That keeps the identifier collision rather than
  fixing it — `m.picked` is keyed by the identifier, so two workspaces' ENG-124s
  tick together on one `space` — and keeping it is the cheaper failure: a row
  you look at twice beats a ticket that was never on the page. The token is
  typed where the product is named: `l` on the assignment editor's products
  pane, masked, because the map is keyed by product *name* and hand-writing it
  means retyping one exactly, in another file, with a silent read under a
  product that groups nowhere as the failure. The full record is
  `docs/adr/0006-a-linear-token-is-the-scope.md`.
- **A key sheet that can lie is worse than none, and `/` is fzf itself.**
  Every key was a literal in a switch with `?` hand-written beside it — merely
  duplicated while keys are fixed, and the central problem the moment they are
  not, because `?` is where you go to find out what your keys are. `internal/
  keymap` is now the one table: action id, scope, shipped key, and the sentence
  `?` prints, with `[keys]` in config.toml rebinding any of them and the sheet
  **generated** from the resolved map. A collision is refused at load with both
  actions named — including across scopes, since a lens is asked before the
  global keys are, so a lens binding that reuses `U` does not share it, it takes
  it. An unknown id, a key bound to nothing and `ctrl+c` are refused the same
  way; a bad `[keys]` keeps the defaults and says so, because the cockpit is how
  you look the ids up. **Scope** is what lets one key mean two things: `n` makes
  a product and steps to the next search match, never both at once. Resolution
  hands the switches the action's DEFAULT key, so they still read `case "n":`
  and a rebind changes which physical key arrives, not what the code is about —
  and a key whose action was rebound away is swallowed, not passed through.
  Resolving happens *after* each screen's text-entry guards: rebinding must not
  change which letters you can type. **An action the user thinks of as one thing
  must be one id** — "new product" shipped as two and rebinding moved half of
  it, caught against the real binary. `/` searches the list that has the
  keyboard (portfolio, the editor's repos, an unfolded row's checkouts) using
  fzf's own matcher linked in process, so the ranking is the one in the human's
  fingers and the match POSITIONS come back for an in-place highlight; the list
  is **lit, not filtered**, because a row's neighbours are often the point of it
  and a shrinking table makes the count at the top a different number from the
  one you were reading. `n`/`ctrl+n` jump between hits. Full record:
  `docs/adr/0013-a-key-sheet-that-can-lie-is-worse-than-none.md`.
- **`U` is the upgrade key and nothing else's; undo is ctrl+z.** Shift is not
  a namespace. `handleKey` resolves `U` globally, before any lens is asked, so
  a lens that wants its own capital U never sees the key — the assignment
  editor's "start over" was exactly that, dead since the upgrade key landed,
  and is ctrl+u now. And undo, the key you hit fastest and without looking,
  must not be one slipped shift from upgrading the machine in place, so it is
  a chord. That also frees `u` for the editor's unassign, which the global
  undo used to steal whenever a triage act had left something undoable.
- **An upgrade nobody is watching must not ask.** `U` was a confirm bar, then
  the terminal handed to the package manager the way `enter` hands it to tmux,
  then the re-exec. Homebrew 4.6 made ask mode the default, so the sequence had
  become: press `U`, agree, lose the screen, and be asked again by `brew` —
  two questions and a screen wipe for one keystroke that meant yes. The confirm
  existed *because* of the handover (it was a warning as much as a question),
  so the handover going takes it with it. The command runs behind the cockpit
  now, which makes "cannot ask" a correctness requirement rather than a
  courtesy: there is no terminal for a prompt to appear on and nobody to answer
  it, so a manager that stopped to ask would hang forever behind a spinning
  bar. Every question is therefore refused in advance —
  **`HOMEBREW_NO_ASK=1` in the child's environment** rather than the `-y` it
  shares a help entry with (an unknown flag fails the whole upgrade where an
  unknown variable is ignored — the same rule as claude's `--permission-mode`),
  winget's three accept/no-interactivity flags, and stdin at `/dev/null` as the
  backstop under both. All of it on screen is **one bar above the footer**,
  and everything on that bar is measured: the meter is indeterminate by
  construction (a fixed lit run sliding and wrapping — the manager does not say
  how far through it is, and a bar filling to a schedule we invented is the
  fabricated figure this cockpit refuses everywhere else), and the words beside
  it are the manager's own last line, quoted — pumps that break on `\r` as well
  as `\n`, or a download's percentage never reaches the screen at all. Owning
  the reporting also means a failure carries the manager's own `Error:` line
  into the notice, since it is no longer on a terminal anywhere. And the
  **exec waits for anything it would take away**: it replaces the process, so a
  build that lands while a dispatch prompt is half-typed holds
  (`model.inputPending`, released by the meter's own tick — the one message
  that arrives whoever has the keyboard) rather than taking it. Reading
  overlays are deliberately not on that list: help left open at lunch must not
  stop an upgrade from ever landing. No timeout — killing a package manager
  part-way through an install is the one outcome worse than a stale binary.
  Full record: `docs/adr/0011-an-upgrade-nobody-is-watching-must-not-ask.md`.
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
  The full record is `docs/adr/0003-amber-is-a-claim-about-the-human.md`.
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
- **A fan-out is hook truth, reported like ci, swept with the turn.** When a
  session spins out subagents, the cockpit sees them the way it sees checks:
  from the source that already carries every status fact. `SubagentStart`/
  `SubagentStop` hooks (installed by `init`; payload fields verified against
  the installed claude before building on them) append to and settle a
  `Subagents` list on the record — an annotation like parking, never a
  status: a start arriving on a blocked record leaves it blocked. The turn
  is the story: SessionStart clears the list, a new prompt drops the
  finished entries and keeps live ones (background agents cross turns), and
  a Stop with no background tasks sweeps anything still marked live,
  because at that point a "running" entry is a missed stop event, not a
  running agent — a count that can only go up is a ghost in miniature.
  Every other way a session ends settles it too: SessionEnd sweeps
  unconditionally, and the two hookless deaths sweep at their retirement
  sites (cockpit kill, `ReconcileSessions`). The annotation records through
  the done guard the way refreshCommits does, and `state.Lock` serialises
  the hook path — subagent events are the first that arrive in parallel,
  and unserialised load-modify-saves would drop each other's writes. The
  running row's SIGNAL says `fan-out · 3 live` beside the ci clause; the
  detail meta counts the turn and one further line names the types; the FAN
  OUT switch itself shows as config beside the mode. Existing installs
  re-run `init` to get the two new hook entries. Full record:
  `docs/adr/0005-a-fan-out-is-hook-truth-swept-with-the-turn.md`.
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
- **The dispatch form's MODEL is a flag read from claude's own help; FAN OUT
  is a sentence in the prompt.** Both forms offer them beside MODE. MODEL is
  `--model`, and its offer is "default" (no flag at all — the human's own
  Claude Code decides) plus exactly the aliases the installed claude's help
  names, parsed the way the mode's choices are: an alias we cannot vouch for
  is never offered and never passed, because a rejected value is a launch that
  dies or a session erroring unattended. Resume passes `--model` again;
  "default" is recorded as the word, distinct from the "" of records that
  never chose. FAN OUT has no flag to pass — Claude Code's multi-agent opt-in
  is the keyword "ultracode" in the prompt — so on, the launch appends one
  closing sentence carrying it (skipped if the human already typed the
  keyword), records the composed prompt, and sets `FanOut` on the record so
  screens need not grep for it. Not re-applied on resume: the transcript
  already carries it, and a resume prompt is the human's own message. The
  ask-nothing paths (backlog enter/ctrl+d, product-panel re-dispatch) launch
  on the defaults. Full record:
  `docs/adr/0002-model-is-a-flag-fan-out-is-a-sentence.md`.
- Commit attribution is by provenance (dispatch records its branch SHAs),
  NEVER by Co-Authored-By trailers — the user strips those from commits.
- User is on a Claude subscription (not API billing): portfolio roll-up
  speaks in tokens/effort, never dollars.

## Architecture map
- `main.go` — subcommand dispatch: cockpit (default), `init`, `hook`.
- `internal/state` — dispatch records, the event log (lifecycle hooks plus the
  dispatch audit) and `prompts/<id>.txt`, the prompt each dispatch is launched
  with, under `~/.local/state/claude-dispatcher/` (override:
  `CLAUDE_DISPATCHER_STATE`).
- `internal/keymap` — every action the cockpit has (id, scope, default key,
  help line), `[keys]` overrides, and the collision rules. `?` is built from it.
- `internal/fuzzy` — `/` search, wrapping fzf's own matcher in process; returns
  match positions so a hit is highlighted where it sits.
- `internal/repos` — repository discovery: the scan of the configured roots,
  and the read of git's own metadata that turns the checkouts it finds into
  repositories (common dir, name from origin, canonical checkout, the worktree
  registry). No git process — every answer comes from files, because a
  discovery runs on every load. `env.go` is the other half of what a repo
  is for a dispatch: the tmux server its sessions live on and the command
  they are launched under, both resolved rather than typed.
- `internal/appearance` — whether the OS is set light or dark, asked (never
  subscribed to) through the platform's own command. The cockpit's
  `theme.go` polls it and pairs it with the terminal's 2031 reports.
- `internal/hookcmd` — receives lifecycle hook events, drives the status
  state machine (launching/working/needs-input/blocked/done/exited).
- `internal/dispatch` — branch + tmux + record creation, and `Resume`: a
  finished dispatcher's session reopened with `claude --resume <session id>`
  in its own worktree (rebuilt if it was reclaimed). A session ending never
  loses a dispatcher — triage's `h` and the product panel's `H` tab list every
  finished one and resume it. `root.go` is where a feature branch starts: the
  remote's own default, the human's named `Root`, and the branch list the forms
  offer — and the reason none of it trusts `origin/HEAD`.
- `internal/cockpit` — Bubble Tea cockpit; responsive tiling breakpoints at 110
  and 170 columns (more panes on wide screens, never one ballooned view).
  `collect_sessions.go` is the one collector that reads the machine rather than
  the records: the sessions on it that no dispatch claims.
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
    presses `U` precisely when they think it is behind. Finding a release
    upgrades to it — they pressed it to upgrade, not to be told.

## Build
`make build` / `make vet` / `make install` (binary to ~/.local/bin — the init
hook embeds the absolute binary path, so reinstall to the same path).
