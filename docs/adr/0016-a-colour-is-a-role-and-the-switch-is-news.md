# A colour is a role, and the switch is news

## Status

Accepted (2026-09-15)

## Context

The cockpit is a port of a dark design and was drawn as one. Every colour was a
hex from the design's C table, passed straight to lipgloss, and only
foregrounds are painted: the ground is the terminal's own. So on a light
terminal (reported from NixOS/niri with ghostty following the desktop's
light/dark switch) the design's pale slate greys were pale grey on white. The
header, the hints and every faint column could barely be read, and the
selected row was a slab of near-black `#18222f` across a white screen.

A fixed light palette would only move the problem. That machine flips between
light and dark with the desktop, and a cockpit stays open for days.

## Decision

**A colour is a role.** `cFaint`, `cSel`, `cGreen` and the rest name what a
colour is *for*. A **theme** (`internal/cockpit/theme.go`) is the table that
says what hex each role is. `fg` and `paint` resolve through the active theme
at render time, so no lens knows which theme it is drawing in, and the style
caches (keyed by role) are emptied when the theme changes. The design's values
are the `dark` theme, verbatim. `light` turns the design over rather than
inverting it:

- The emphasis ramp keeps its order. `cWhite` is still the loudest word on the
  screen; it is just the darkest now.
- Each hue takes the shade of itself that reads on white, so amber still means
  "this wants you".
- Roles that sit *behind* text flip the other way. Rule and selection get
  separate light greys again (the dark design shares one hex for them), because
  on white a border as heavy as the selection reads as a second selection.

A test holds every registered theme to every role, and every word-carrying
role to WCAG AA on the theme's own ground and 3.5:1 on its selection.

**The mode is config's `theme`.** `"system"` (or no line at all) follows the
switch. A theme's name holds that theme. Settings (`,`) cycles the mode on
enter, redraws at once and saves; nothing is reloaded, because a theme changes
how the snapshot is drawn, not what is in it. A name that is not a theme falls
back to system and says so.

**Two things report the switch, and neither is enough alone.**

- *The operating system* knows the setting, but can only be asked:
  `internal/appearance` reads the freedesktop portal's `color-scheme` (`busctl`,
  else `gdbus`), macOS's `AppleInterfaceStyle`, or Windows' `AppsUseLightTheme`.
  It is polled every 2 seconds. A portal read costs about 2ms. Portal value 0,
  "no preference", is *unknown* rather than light. A machine with nothing to
  ask stops polling rather than spawning a failing lookup for the life of the
  process.
- *The terminal* knows what it is actually painting. Mode 2031 asks it to send
  `CSI ? 997 ; 1 n` (dark) or `; 2 n` (light) on every change, and
  `CSI ? 996 n` asks for the current answer. tmux 3.6+ relays both (verified in
  the 3.7b binary), and ghostty, kitty, contour and foot send them. Bubble Tea
  v1 has no type for the sequence and delivers it as its unexported
  unknown-CSI byte slice, so it is matched by shape and then exactly by
  content. The enabling write goes straight to stdout. It cannot tear a frame,
  because the renderer writes through the same `*os.File`, whose write lock
  holds for a whole `Write`. It is re-sent after a jump-in, since a tmux client
  attached in the cockpit's terminal turns the mode off again as it leaves.
  `2031l` is written on exit.

Where the terminal speaks, the switch is instant. Where it does not, the poll
catches it within a couple of seconds.

**The newest change wins.** When the two disagree, for example a terminal
pinned to a dark theme on a light desktop, the OS answer counts only when it
differs from the OS's *previous* answer. A terminal that has said "dark" is not
overruled every two seconds by a setting that has not moved. The first frame is
decided before Bubble Tea starts: the OS is asked once, and if it cannot answer,
the terminal's background is read once (OSC 11, via lipgloss) while the tty is
still free. The boot screen is therefore drawn in the right theme rather than
flashing the dark design first.

**A third-party theme is a table.** Catppuccin, say, is one more entry in
`themes`, nameable by a static mode, with its `appearance` saying which side of
the switch it is drawn for. Choosing *which* registered theme `system` uses for
each side is the part not built yet. It is one lookup (`themeForAppearance`)
waiting for a config key.

## Consequences

- Every existing call site is unchanged: the constants kept their names and
  changed their meaning from a hex to a role. A literal hex passed to `fg` still
  renders as itself, but nothing in the cockpit passes one any more. The boot
  screen's leader and gradient became roles too.
- The OS poll is a subprocess every two seconds while the mode is `system` —
  the price of no D-Bus dependency. A static mode never polls.
- A terminal that follows neither the desktop nor 2031, on a machine with
  nothing to ask, gets the background read once at startup and nothing after.
  `theme = "light"` is the answer there.
