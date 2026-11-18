# 31. A failure is read for its cause, and the rest is kept

Date: 2026-11-12

## Status

Accepted.

## Context

Reported from the NixOS machine as "on macos build i could see more
errors … not so here on nixos", with a screenshot of the cockpit after a
failed resume. The footer had spilled everything `nix develop` printed down
the screen: the download of google-chrome-stable_142.0.7444.175-1_amd64.deb
failing, then the builds that depended on it. Messy, but the error was on
screen. On the NixOS machine's own screen, the same failures showed nothing
useful.

There were two causes, and neither was specific to NixOS:

- **The footer dropped a notice that did not fit.** It put the key help on
  the left and the notice on the right through `spread`, and `spread` keeps
  the left and throws the right away when both do not fit. "resume failed: …"
  and "launch failed: …" are longer than the room beside the help on a
  laptop-width terminal, so there they never appeared. On the wide screen in
  the screenshot they did, and a multi-line one spilled.
- **A failure was its first line.** `failLaunch` kept the first line of the
  supervisor's output as the record's reason, and `launchFailed` did the same
  for the notice. tmux's refusals are one line ("command too long", ADR 0009),
  so that worked for tmux. It does not work for anything that wraps tmux. In
  the machine's event log, a dispatch to playerpulse whose dev shell would not
  build printed 883 lines, 97KB: "copying path … from https://cache.nixos.org"
  and "building '…'" for every dependency, then the failure. The record said
  `did not start: tmux new-session: copying path
  '/nix/store/…-nss-cacert-3.115' from 'https://cache.nixos.org'...`, the one
  line in all of it that says nothing went wrong. git does the same in fewer
  lines: `git worktree add` prints "Preparing worktree (checking out '…')"
  before its `fatal:` line.

The rest of the output was not lost, but the only place it lived was the
`DispatchFailed` event, as one JSON string.

## Decision

**A notice is never dropped, and it never hides the keys.** One that fits
beside the help sits there as before. One that does not goes on its own line
above the help, clipped with "…" only when it is wider than the whole line.
The body gives up that row while the notice stands. The first version gave
the notice the help's room instead, and running it showed the cost: a notice
stands until the next one replaces it, so the keys were hidden for as long,
including the key that reads the error. A notice is also one line: a
multi-line one has its lines run together, not cut to the first.

**A failed command's error is its cause** (`internal/cause`). The first line
that opens with `error:` or `fatal:` names the failure. The indented block
under it is searched for the deepest error it holds. For a failed nix build
that is the builder's own complaint behind its `> ` log lines; for an
evaluation error it is the error at the bottom of the trace. Output with no
such line is read from the end, past nix's progress lines. Store hashes are
dropped from that one line, and it is capped at 300 characters. The measured
case reads:

    did not start: tmux new-session: error: Cannot build
    'google-chrome-stable_142.0.7444.175-1_amd64.deb.drv' — cannot download
    google-chrome-stable_142.0.7444.175-1_amd64.deb from any mirror

`tmux new-session` and the two git calls that cut a worktree return a
`cause.Error`. Its `Error()` is that one line and its output is kept behind
it, so every caller that shows `err.Error()` gets the cause without knowing
about it. The fixtures are real `nix develop` output: the download failure
from the reporting machine, plus an evaluation error, a fetch error and a
failing builder captured for this.

**The rest is kept, and one key away.** A session start that fails writes
the cause and everything printed after it to `state/output/<id>.txt` and
names the file on the record (`StartOutput`). That applies to a launch, a
relaunch of a row that never started, and a resume of a finished
conversation. A failed resume leaves the record's status alone, because it is
still the finished dispatcher it was. `l` (`fleet.output`) on the row opens
the file in the human's editor at the line where the error starts
(`cause.LineOf`), the way `o` opens a decision. It is offered only where
there is more than the one line: a key that opens what the row already says
does nothing. A start that works clears the field. The audit keeps
everything too (`cause.Full`), so ADR 0009's "reason verbatim" still holds.

## Consequences

- The record's reason, the footer, the failed row and `status` all say the
  same one line, because they all read `err.Error()`.
- Records written before this keep the first line they were given. Their full
  output is still in `events.jsonl`, and `⏎ start again` on one writes a new
  reason in the new way.
- A launch refused before its record exists (a git failure while cutting the
  worktree) has no record to name a file on. Its row's reason is the cause,
  and the whole text is in the audit only.
- The cause reader knows nix and git by their words, not by name: `error:`,
  `fatal:`, an indented block, and a short list of progress prefixes. A tool
  that fails with none of these is read from its last line that is not
  progress, which is where a command that died says the last thing it knew.
