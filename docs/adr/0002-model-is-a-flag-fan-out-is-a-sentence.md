# The model is a flag read from claude's help; fan-out is a sentence in the prompt

## Status

Accepted (2026-08-17)

## Context

The dispatch form chose two of a session's three launch-time properties — the
permission mode, and (implicitly) the prompt — but not the model the session
runs, and not whether it may spread the work across multiple agents. Every
dispatch ran whatever the human's own Claude Code defaults to, and the dx
form's summary line deliberately named no model, because printing one we did
not pass would have been a fabricated fact.

Making them choosable raises the same problem the mode had (see
`internal/dispatch/mode.go`): Claude Code's `--model` values are not a fixed
enum. The flag takes an alias for the latest model ('fable', 'opus', 'sonnet'
today) or a full model name, the alias set grows with releases, and a value
the installed claude does not accept is either a launch that dies before it
reads the prompt or a session erroring on its first message with nobody
watching. Fan-out is different in kind: Claude Code has no flag for it at all.
Its opt-in for multi-agent orchestration is the keyword "ultracode" appearing
in the prompt itself.

## Decision

Both forms (the triage lens's dx form and the `+` overlay) offer two new
choices, and each travels by the only honest channel it has:

- **MODEL is a launch flag whose offer is read from the installed claude's
  help.** `dispatch.Models()` is "default" — pass no flag at all — plus the
  aliases `claude --help` names for `--model`, parsed once per process the way
  the mode's choices are. An alias the help does not vouch for is never
  offered and never passed: `ModelArgs` emits nothing for it, so a record
  resumed on a machine whose claude no longer advertises the alias opens on
  that build's own default rather than being handed a value it may reject.
  "default" is recorded as the explicit word, distinct from the "" of records
  written before the model was a choice — those sessions did not choose.

- **FAN OUT is prompt composition, not a flag and not a status.** On, the
  launch appends one closing sentence carrying the ultracode keyword
  (`dispatch.FanOutInstruction`) — unless the human already typed the keyword,
  in which case they opted in themselves and saying it twice is noise. The
  record carries `FanOut` so screens can say which kind of session they are
  looking at without grepping the prompt, and it carries the composed prompt,
  because the record's Prompt is the prompt the session actually received.

Resume passes `--model` again alongside `--permission-mode` — both are
properties of the new session, not of the transcript it reopens. Fan-out is
deliberately *not* re-applied on resume: the keyword lives in the recorded
prompt and the transcript already carries it, and a resume prompt is the
human's own message, which this feature never edits.

The paths that ask no questions — the backlog's enter and ctrl+d, the product
panel's re-dispatch — launch on the defaults, for the same reason they take
the default mode: a choice made for a previous run is not consent for this
one, and a form that did not ask must not answer.

## Consequences

The offer is machine-dependent by design: on a box whose claude help cannot be
read, MODEL collapses to a one-position switch ("default") and stays honest
about having nothing else to offer. Tests answer for the parsed aliases the
way mode tests answer for the mode names, so CI needs no claude on PATH. The
alias hints claim nothing about capability ("runs the latest Opus"), because
capability claims go stale with every release. And fan-out costing tokens is
the human's call at dispatch time, made per-dispatch on a switch whose off
position — solo — is the default.
