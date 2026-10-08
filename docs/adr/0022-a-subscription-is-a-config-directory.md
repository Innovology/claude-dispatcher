# 22. A subscription is a config directory, and what it has left is the status line's to say

Date: 2026-10-07

## Status

Accepted.

## Context

Asked for as "allow multiple active subscriptions, and when you start a
dispatch you select the subscription and can see next to it the current %
capacity left in the 5hr/weekly limit".

Every dispatch ran as `claude` with no account of its own, so every one spent
the one subscription the human's own Claude Code was logged in to. Three
questions decide what "several" can mean, and each was answered against the
installed claude (2.1.281) before anything was built on it:

- **What separates two logins on one machine.** `CLAUDE_CONFIG_DIR`. Claude
  Code keys everything a login is to it: settings, `.claude.json`, the
  transcripts under `projects/`, and the credentials themselves — on macOS
  the keychain entry is keyed to the directory too. Measured: an empty
  directory answers `claude auth status --json` with `loggedIn: false` (exit
  1, JSON still printed) in 0.12 s, while the default answers for the human's
  Max login with email, org and `subscriptionType`.
- **Who a directory is.** `claude auth status --json`, Claude Code's own
  answer. Nothing here reads a credential.
- **How much of a subscription's limits is left.** Claude Code enforces both
  windows and says so in exactly one documented place outside itself: the
  JSON its **status line** command is handed. Captured from a real session:
  `"rate_limits":{"five_hour":{"used_percentage":5,"resets_at":1791367800},
  "seven_day":{"used_percentage":3,"resets_at":1791892800}}` — percent used
  and epoch seconds, present only for a subscription, only after the
  session's first response, either window independently absent. No hook
  payload carries it and no CLI prints it. The usage lens's learned caps
  (internal/usage) are a model of a limit from transcripts; this is the
  limit.

And one trap: each config directory reads **only its own** `settings.json`.
Our hooks live there, so a session under a second directory that nobody
installed them into would report nothing — its record would sit at
`launching` until a sweep called it a ghost — and the status line would never
record its figures.

## Decision

**An account is a name and a config directory.** `[accounts]` in the config
maps one to the other. The human's own login is always there as `default` and
is never spelled out: it sets no `CLAUDE_CONFIG_DIR` at all, because setting it
to `~/.claude` explicitly keys a different keychain entry — exactly as
`ModelDefault` passes no `--model`. `claude-dispatcher account add <name>
[dir]` is the whole setup: the directory (default `~/.claude-<name>`), the
config entry, our hooks and status line in its settings, `claude auth login`
under it, and Claude Code opened there once if its first-run setup has not
happened. `account` lists who each is and what each has left; `account login`
and `account remove` (which forgets the name and leaves the directory, the
login and its transcripts alone) complete it. `init` installs into every
account's directory as well as the default's.

**What is left is recorded by the status line.** `claude-dispatcher
statusline [--then <command>]` is installed as every account's status line. It
files the payload's `rate_limits` under the config directory the session runs
under (its own `CLAUDE_CONFIG_DIR`, so one command serves every account) in
`state/accounts/<hash>.json`, then runs the human's previous status line on the
same input and prints what it printed — a status line already there is wrapped,
never replaced, and one that was not there stays blank. A payload with no
rate limits is not a reading and leaves the last one in place. Writes are
whole-file renames, and an unchanged reading is rewritten at most once a
minute.

**The figure is shown with its age, and never invented.** Both forms offer
ACCOUNT beside MODEL: the dx form as a switch whose words carry the least of
the two windows (`default 59%  work 92%`) and whose hint is the selected
account in full (`me@… · max · 5h 77% · wk 59% left · 3m ago`); the `+`
overlay as a step listing every account that way. The least window leads
because it is the one that stops a session first. The only inference made
from a reading is the window's own rule: past `resets_at` it has emptied. An
account no session has drawn a status line for says "no usage reading yet"
rather than borrow the usage lens's learned estimate — this is where a
subscription is chosen on the strength of a number, so it carries only the
number Claude Code gave. The probe runs when a form opens (a tenth of a
second per account, in parallel), not on the poll: it answers a question
asked at the moment of dispatching, and the names are on screen before it
lands, reading "…" until then.

**An account that would stall a session is refused at the launch.** A
directory that is not logged in opens on a login prompt; one without our hooks
never reports; one never opened stops on Claude Code's first-run screens. All
three are cheap to know before the launch and invisible after it, so `Launch`
refuses on them (ADR 0009's rule — a dispatch that did not happen says why),
each refusal naming the command that fixes it. A login that could not be asked
about is not a refusal. The default account is not checked: nothing about it
changed.

**The account rides the record.** `Account` (the name; empty for the default,
so a record from before accounts and one launched on the default since say the
same thing) and `ConfigDir` (the path). `Resume` reopens under `ConfigDir`,
not under whatever the name now points at, because the transcript `--resume`
reads is in that directory and claude started under any other cannot find the
conversation. The worktree's trust is inherited into that account's own
`.claude.json`, read from its file first and the default's second: trusting a
folder is the human's judgement about the code, not about which subscription
pays — and a repo nobody vouched for under any login is still never trusted.

The account shows on a running dispatcher's detail line (`on work`) and in
`status --json`, so the steward can see which subscription a session spends.
The ask-nothing paths (backlog enter, product re-dispatch) launch on the
default, as they do for every other form choice.

## Consequences

- Existing installs re-run `init` to get the status line; until a session of
  an account draws one, its forms say there is no reading.
- Readings are only as fresh as the last session of that account to draw its
  status line. An idle subscription's figure can be hours old; the age is on
  screen, and a window past its reset reads as full.
- The Windows build wraps no existing status line (its command is not run by a
  POSIX shell, and quoting for one would be a guess); it installs ours only
  where there was none.
- Nothing chooses an account for the human — no "least used" default. The
  default stays the human's own login; the figures are there to choose by.
