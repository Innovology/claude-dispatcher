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

**A field has a way out short of the form.** ctrl+u deletes everything before
the caret, which with the caret at the end (where it is unless moved) clears
the field; ctrl+w deletes the word before it and ctrl+k everything after it —
the readline keys the decisions search and the `+` overlay's textinput already
answer to — through the same `dxEdit` as backspace, so WHERE still sends its
cursor home and PREFIX still records an override. The footer names ctrl+u.
They are not keymap actions, for the reason a query's keys are not (ADR 0024):
while a field has the keyboard every key is the field's.

**The caret moves.** Asked for in the same breath as "enable arrow keys
navigation in the title": a typo three words back was fixed by deleting the
three words. In the one-line fields left/right step a character as the screen
draws it (a grapheme, so an accent or a skin tone is never split), alt or ctrl
with them — and alt+b/alt+f, what a Mac terminal sends for option-arrow — a
word, home/end and ctrl+a/ctrl+e the ends, and delete takes the character under
the caret; typing and pastes go in where it stands. It is held as the bytes
*after* it, tagged with its field (`dxCaret`): every edit is at the caret, so
that count is unchanged by anything done before it, the zero value is the end,
and a key arriving in another field sends it home. The row is drawn around it
every frame rather than scrolled and remembered: what follows gets up to half
the room, so at the end the line scrolls exactly as it did. WHAT keeps its
caret at the end — its wrap collapses runs of spaces, so a position in the text
is not a position on the screen without a layout that keeps one.

**A long field opens in the human's editor.** ctrl+g on WHAT or DONE WHEN is
what it is in Claude Code: the text in `$VISUAL`/`$EDITOR`/vi (the editor `e`
on settings uses, handed the terminal the same way), read back on exit with the
caret at the end and the editor's closing newline dropped — WHAT keeping its
lines, DONE WHEN folded onto its one. The file is private (0600) and removed
only once its text is in the form; an editor that exits with an error (vim's
`:cq`, the deliberate "never mind") leaves the field alone and keeps the file,
named in the notice, because it may be the only copy of a long brief. It is
the answer to WHAT's limits: the box shows six rows and the caret only the end.

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
