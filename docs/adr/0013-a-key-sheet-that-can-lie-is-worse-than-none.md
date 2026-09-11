# A key sheet that can lie is worse than none

## Status

Accepted (2026-09-11)

## Context

Every key in the cockpit was a literal in a switch, and `?` was a hand-written
table beside them. That is merely duplicated effort while the keys are fixed.
Asked for remappable keys, it becomes the central problem: the help sheet is
exactly where someone goes to find out what their keys are, and a sheet written
beside a keymap is a lie waiting for its first rebind.

The request that forced it: a `/` search on the products lens with `n` stepping
forward through matches and `ctrl+n` back. `n` is "new product" there. As the
reporter put it — *"this however means that the `n` needs to be remappable,
since now it's used for the new project."*

## Decision

**One table, and `?` is built from it.** `internal/keymap` holds every action:
a stable id, the scope it belongs to, the key it ships with, and the sentence
the help sheet prints. `[keys]` in config.toml rebinds any of them, and the
sheet is generated from the resolved map, so a rebind appears in `?` the moment
it takes effect. A test asserts every documented binding reaches the sheet, so a
new action cannot be invisible because someone forgot an ordering list.

**A collision is refused at load, with both actions named.** Letting the last
one quietly win would present as a key that stopped working, which is the worst
possible way to learn about a typo in a config file. That includes *across*
scopes: a lens is asked before the global keys are, so a lens binding that
reuses a global key does not share it, it takes it — bind `U` to a fleet action
and the upgrade key is gone from triage with nothing saying why. An id that is
not an action, a key bound to nothing, and any attempt to rebind `ctrl+c` are
refused the same way. A bad `[keys]` keeps the defaults and says so rather than
refusing to open: the cockpit is how you would look the action ids up.

**Scope is what lets one key mean two things.** `n` makes a product on the
products lens and steps to the next match while a search is up, because only one
of those scopes has the keyboard at a time. That is why the search's jump keys
did not have to displace anything, and why the defaults ship without a
collision despite `n` appearing twice.

**Resolution translates a pressed key into the action's DEFAULT key**, so the
cockpit's own switches go on reading `case "n":`. A rebind changes which
physical key arrives at an action, not what the code is about. Three outcomes,
and the third is what makes rebinding correct rather than merely possible: a
bound key becomes its action's canonical key; an unbound key passes through, so
text entry still works; and a key that is the *default* of an action rebound
away is swallowed, because leaving the old key working beside the new one is not
what rebinding means.

**Resolving happens after each screen's text-entry guards.** Rebinding must not
change which letters you can type into a prompt.

**`/` is fzf, not fzf-like.** `internal/fuzzy` links
`github.com/junegunn/fzf/src/algo` and calls it in process. The ranking is then
the one the human already has in their fingers, and the matcher returns the
positions it matched at — which is what makes an in-place highlight possible,
and the reason not to shell out to the binary and hand the screen away.

**The list is lit, not filtered.** A row's neighbours are often the point of it:
how many repos a product has, what else is stale beside it. A table that shrinks
under a query makes the count at the top of the screen a different number from
the one you were reading a second ago. The cursor jumps between hits instead.

**The search applies to the list that has the keyboard** — the portfolio table,
the editor's repos, or an unfolded row's checkouts. Nothing asks which you
meant, because there is only ever one in front of you.

## Consequences

- Using the real fzf is only worth anything if it is used correctly, and two
  mistakes here looked completely fine. `algo.Init` builds the tables the scorer
  reads and is package state fzf's own main sets up; without it `"papp"` found
  no match at all in `Player-App-2`. And the matcher does not fold the pattern —
  case-insensitively it lowercases the text it scans and compares against the
  pattern as given, so an unfolded pattern silently matches nothing. Neither is
  catchable by a hand-written expectation, because the expectation is written by
  whoever got it wrong. Hence a **differential test**: it runs the installed
  `fzf -f` over the same candidates and requires the same order back, across 23
  queries. It skips when the installed binary and the linked library are
  different releases — measured, 0.72.0 against a 0.74.3 library ranks one query
  differently, which is a finding about two versions. With them matched, all 23
  agree.
- Score alone is not fzf's ranking either: its default tiebreak prefers the
  shorter line, which is why `"papp"` puts `player-app` above `soccerpulse-app`
  despite scoring a point lower.
- **An action the user thinks of as one thing must be one id.** "New product"
  shipped as two — `products.new` and `editor.new` — so rebinding it moved half
  of it, and `n` went on making products inside the editor. Caught against the
  real binary, not by a test. One id bound in two scopes now, as `search.open`
  already was.
- Every on-screen key hint is read from the keymap for the same reason `?` is.
- fzf is a new dependency; `nix/package.nix` vendorHash updated accordingly.
