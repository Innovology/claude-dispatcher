# 30. A field is the shape it is drawn, and every key has a way out

Date: 2026-11-12

## Status

Accepted.

## Context

Reported as "need to have an escape from a bad state": a brief pasted into the
dx form's TITLE instead of WHAT left a title that could not be deleted, a
cockpit that would not quit, and a terminal that had to be closed. The
screenshot shows the form with an email reply chain filling TITLE — ninety-odd
rows of it — and nothing below it: no WHAT, no switches, no caret.

Reproduced against the real binary in a scratch tmux server, with a bracketed
paste (`paste-buffer -p -r`) of a 2,930-character email into TITLE, and found
to be three faults that compound:

- **A one-line field took line breaks.** A bracketed paste arrives as one key
  message carrying every rune, newlines included, and `typedTextFor` handed
  them through. TITLE is budgeted one row and drew as one per line. Every
  hand-rolled field shared the path: the triage form's six, the palette,
  reply, park, resume, adopt naming, the products editor's name, token and
  socket, both searches. The `+` overlay never had the fault, because bubbles'
  textinput folds a pasted newline into a space.
- **The scroll was quadratic.** `dxValue` kept the caret in view by dropping
  one rune from the front and re-measuring everything left, on every redraw.
  At 9,000 characters a frame cost half a second. Bubble Tea redraws after
  every message, so each key queued behind the paste paid it again. Measured on
  the old binary: paste 9,163 characters, press backspace twenty times, then
  ctrl+c — **the cockpit quit 122 seconds later**. ctrl+c was resolved first
  all along (`handleKey`); it was waiting in the same queue as the backspaces.
- **There was nothing between one character and the whole form.** backspace
  takes a character from the end of a title whose end is off screen, and esc
  closes the form with the repo pick, the root, the mode and every other field.

And behind them, a ceiling nobody held: the form's own comment said "a title
is already short", the `+` overlay caps the name at 80, and the dx form took
anything — a brief pasted there and submitted would have been filed under a
3,000-character feature name, printed on every row and in history.

## Decision

**A field takes the shape it is drawn.** `typedTextFor` is now what a key types
into a *one-line* field: each line break (`\r\n` counted once, and the
Unicode separators) and each tab folds into a space, and every other control
character is dropped — an escape byte in a field is written raw to the terminal
on every redraw. WHAT is the one field that holds a paragraph, so it takes
`typedBlockFor`: line breaks kept, all as `\n` since a terminal may send a
paste's as `\r`, other controls dropped. The fold is at the one place text
enters, so every field shares it and a new one inherits it.

**A row costs its length, not its square.** `tailFit` measures each grapheme
once and keeps the end that fits; the row also draws through `oneLine`, so a
value set by any route other than typing still draws as one row. A
200,000-character title draws in about 20ms, and ctrl+c after the same
paste-and-backspace sequence quits in 0.2s.

**A field has a way out short of the form.** ctrl+u clears the field that has
the keyboard and ctrl+w drops its last word — the readline pair the decisions
search already answers to — through the same `dxEdit` as backspace, so WHERE
still sends its cursor home and PREFIX still records an override. The footer
names ctrl+u. They are not keymap actions, for the reason a query's keys are
not (ADR 0024): while a field has the keyboard every key is the field's.

**A title is a name.** `featureNameMax` (80) is the ceiling on both forms. The
`+` overlay's textinput stops there as it always did; the dx form **refuses** at
submit, on TITLE and before it asks for WHAT, and the line under TITLE says so
as soon as the title runs past it — the length, that the brief goes in WHAT,
and ctrl+u. It refuses rather than cuts because what overruns it is a paste,
and a paste cut at 80 characters reads as a title that stops mid-word (the
same call as `MaxPromptBytes`, ADR 0009).

## Consequences

- A multi-line paste into reply, park or the palette now arrives as one line.
  Reply types one line into a session by design; before, a pasted line break
  went to the session's prompt as a raw newline (`send-keys -l` types what it
  is given), and the reply box drew it as several rows.
- Nothing moves a brief from TITLE to WHAT for the human. The form says where it
  goes and how to clear the field; the clipboard still has it. Moving it would
  be the form deciding what the human meant.
- `truncate` still trims by re-measuring. Every caller hands it a bounded
  string — names are capped now, briefs go through the word-wrappers — so it is
  left as it is rather than changed under twenty callers for no measured cost.
