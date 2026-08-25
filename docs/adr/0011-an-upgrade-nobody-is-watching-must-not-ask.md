# 11. An upgrade nobody is watching must not ask

Date: 2026-08-24

## Status

Accepted

## Context

Asked for as "auto update should use the `--y` and reload in the background
just showing a bar".

`U` shipped as three acts. A confirm bar naming the exact command; then
`tea.ExecProcess`, which suspends Bubble Tea and hands the terminal to the
package manager the way `enter` hands it to tmux; then, on a clean exit, quit
and `syscall.Exec` the build that had just been installed.

The reasoning written into it was that the package manager's output is the
human's — "a cask download has its own progress to show, and hiding it behind a
spinner would mean inventing a summary of a process we do not control" — and
that a confirm was owed for the one act in the cockpit that changes the machine
rather than the state dir.

### Homebrew started asking too

Homebrew 4.6 made ask mode the default. From `brew upgrade --help` on the
machine that reported this (Homebrew 6.0.19):

```
  -y, --no-ask, --yes              Do not ask for confirmation before
                                   downloading and upgrading. Ask mode is the
                                   default. Enabled by default if
                                   $HOMEBREW_NO_ASK is set.
```

So the sequence had become: the human presses `U`, the cockpit asks whether to
upgrade, the human says yes, the cockpit takes their screen away, and `brew`
asks whether to upgrade. Two questions and a screen wipe, for one keystroke
whose whole meaning was "yes".

### The handover was what the confirm was for

The confirm existed because of what came next — the terminal being taken away.
It was a warning as much as a question. Take the handover away and what is left
is a second agreement to the thing they just pressed a key for.

### And a question nobody can answer is worse than a question

This is the part that decides the shape of the change. The moment the command
runs behind the cockpit rather than in front of it, **there is no terminal for
a prompt to appear on and nobody to answer it**. A package manager that stops
to ask would hang on a pipe, forever, behind a bar that goes on spinning. So
running it in the background is not only a presentation choice; it makes
"cannot ask" a correctness requirement.

## Decision

### The ask is answered before the command runs

Every manager's question is refused in advance, in the form that cannot itself
be the failure:

- **Homebrew** — `HOMEBREW_NO_ASK=1` in the child's environment
  (`version.Install.Env`), alongside the `HOMEBREW_AUTO_UPDATE_SECS=0` that was
  already there. Deliberately the environment variable and not the `-y` it
  shares a help entry with: a brew too old to know the flag fails the whole
  upgrade on an unknown option, where the same brew simply does not read the
  variable. It is the rule
  [0002](0002-model-is-a-flag-fan-out-is-a-sentence.md) already applies to
  claude's `--permission-mode` — a rejected flag is not a degraded run, it is a
  run that never happens.
- **winget** — `--accept-package-agreements --accept-source-agreements
  --disable-interactivity`. It asks twice before it will install anything (the
  source agreement on a machine that has never used winget, then the package's
  own) and can hand off to an installer with UI of its own.
- **nix profile, scoop** — neither prompts.
- **All of them** — stdin is left at `/dev/null`. That is the backstop under
  the flags: whatever we failed to anticipate reads EOF and fails immediately
  rather than waiting on an answer nobody can type. A test drives a real child
  that reads stdin and asserts it comes back.

### The run is one bar, and everything on it is measured

`upgradeRunCmd` runs the command off the UI goroutine and pumps both its output
streams into `upgradeRun`, which the bar above the footer renders:

```
  upgrading  v3.1.2 → v3.2.0  ░░░░████████░░░░░░░░░░░░  ==> Downloading …   4s
```

The meter is **indeterminate by construction** — a fixed lit run sliding and
wrapping, the same number of cells lit at every frame. The package manager does
not tell us how far through it is, and a bar filling to a schedule we invented
would be exactly the fabricated figure this cockpit refuses everywhere else. It
claims motion, and motion is all it claims. A test asserts the lit count never
changes.

What is beside it is not ours to invent either, so it is quoted: the manager's
own most recent line, and how long it has been going. The output pumps break on
`\r` as well as `\n`, because a download's percentage is carriage-returned over
itself and never terminated — splitting on newlines alone would show nothing
for the length of a download and then dump it in one line. Escape sequences and
control characters are stripped, since `dispWidth` cannot measure them and the
rest of the footer would inherit the colour.

The bar renders over an overlay as well as over a lens. An upgrade running
while the dispatch form is open is precisely the case it exists for, and a bar
that vanished when a form opened would read as an upgrade that had stopped.

### A failure now has to carry its own reason

The old notice named the command and nothing else, because the manager's output
was already on screen — the cockpit had handed it the terminal. Nothing is on
screen now, so the reason travels with the failure: `upgradeRun.errLine` keeps
the last line that named an error (managers say `Error: …` and then keep
talking — cleanup notices, a hint, a blank line — so the last line out is
routinely not the one that explains anything), and the notice quotes it.

### The exec waits for anything it would take away

`syscall.Exec` replaces the process: the screen goes, and everything typed into
it goes. That was safe when an upgrade could only land seconds after a confirm
the human was sitting in front of. It is not safe now — the upgrade finishes
while they are working, and a build installed mid-sentence must not be the
reason a dispatch prompt they spent five minutes on is gone. That is the defect
[0009](0009-a-dispatch-that-did-not-happen-is-a-thing-that-happened.md) is
about, arriving by another door.

So a settled run holds, and the bar says so ("restarting as soon as you are
done here"), until `model.inputPending` is false: no settings, no dispatch
form, no palette, no park reason, no product name or Linear token being typed,
no confirm waiting on a key, and nothing typed into the triage form
(`dxTouched`, because on an empty fleet that form is always on screen).

Reading is not on the list. Help, review and resume lose nothing by being
redrawn by a new build, and holding for them would mean an overlay left open at
lunch stops the upgrade from ever landing.

The release comes from `upgradeTickMsg` — the tick that was already pacing the
meter. It is the one message that arrives whoever has the keyboard, which makes
it the only place a held exec can be let go from without threading a check
through every return in `Update`.

### There is no timeout

A wedged upgrade is visible: the bar prints how long it has been going, and `q`
still quits. Killing a package manager part-way through an install to satisfy a
timer is the one outcome worse than a stale binary — and leaving it to finish
is why quitting the cockpit mid-upgrade does not kill it either.

## Consequences

- `U` is one keystroke from "there is a new build" to running it. Nothing else
  asks, because nothing else can be answered.
- The confirm bar's `"upgrade"` kind is gone; `confirmState` is back to `kill`
  and `ship`, which are the acts that genuinely need a second thought.
- The human keeps their screen, and their typing, for the whole upgrade.
- We now own the reporting. Where the package manager used to explain itself on
  a terminal it had been given, one line of it reaches the bar and one line of
  it reaches the failure notice. That is a real loss of detail on a failure,
  and it is the price of not taking the screen: the command is named in the
  notice, so it can be run by hand to see the rest.
- Verified against the real binary, not only in tests: a build stamped `v0.0.1`
  in a `Caskroom` path, a stub `brew` on PATH standing in for the manager, and
  `tmux capture-pane` once a second through the whole run — the meter moving,
  the caption following brew's carriage-returned percentages,
  `HOMEBREW_NO_ASK=1` arriving in the child, a typed dispatch form holding the
  exec off and surviving it, the exec landing the moment the form closed, and a
  non-zero exit leaving the cockpit where it was with brew's own `Error:` line
  in the footer.
