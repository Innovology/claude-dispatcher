# 34. A dispatcher can hand its work on to another repo of its product

Date: 2026-11-20

## Status

Accepted.

## Context

Asked for as "spawn a new separate dispatch session from within an existing
one": a dispatcher in one repo of a product builds something another repo of
the same product has to consume — an endpoint in the api that the app then
calls — and should be able to start that work itself. The one it started from
"should be considered finished, whatever the correct signal for that is", and
the new session must be on the cockpit.

Three facts shaped it:

- **Only the dispatcher knows.** Nothing the cockpit reads says that a change
  in one repo is something a repo beside it has to consume. The decision is
  the session's judgement, so the session needs a command, and to be told it
  has one.
- **A dispatch started from inside a session inherits that session.** A pane
  takes its environment from the client that asks for it (ADR 0014), and the
  client here is a process in the session's Bash tool, carrying `CLAUDECODE`,
  `CLAUDE_CODE_SESSION_ID`, its messaging socket, `CLAUDE_DISPATCHER_ID` and,
  on another subscription, `CLAUDE_CONFIG_DIR`.
- **The session that hands off goes on running.** The handoff is a command in
  the middle of a turn. The tool returns, the turn ends, and later claude idles
  at its prompt. On an ordinary record each of those fires a hook that puts it
  back on the table as waiting on the human.

PR #89 (branchless work, open) has a `dispatch` verb that sends work out from
a conversation turned orchestrator. That verb leaves its sender running, sends
anywhere, and is part of a larger change that has not merged. A handoff is
narrower in all three ways: it goes only to another repo of the same product,
it ends the sender's run, and it is built on main as it stands.

## Decision

**`claude-dispatcher handoff --repo <repo> --title <words> <brief>`**, run from
inside the dispatcher's session. It finds the dispatcher by the
`CLAUDE_DISPATCHER_ID` its launch line set. An adopted session has none, so it
is found by the `CLAUDE_CODE_SESSION_ID` Claude Code gives every tool, against
the record's session id. `--from` names one from a shell, and a brief of `-` is
read from stdin.

**Only another repo of the same product** (`dispatch.HandoffTarget`). The
product is the one config gives the repo now, because a repo the human has
moved belongs where they put it; a record whose repo discovery cannot find
keeps the product it launched under. A repo in no product, the dispatcher's own
repo and a repo of another product are refused, and the refusal names the
repos that would be accepted. A title that slugs to the dispatcher's own name
is refused too: the feature name is the cockpit's key, and the dispatcher is
still live when the launch is asked for.

**The new dispatch is an ordinary one** (`Launch`), on the subscription, mode
and model of the dispatcher handing off unless a flag says otherwise. Which
login pays was the human's choice for this work. The account is taken by its
config directory, as Resume takes it. Its prompt is the title and brief and
then a line saying where the work it builds on lives: the dispatcher, its
repo, branch, pull request and checkout. A brief written mid-work may take all
of that for granted. The session's variables are scrubbed from the process
before the launch (`ScrubSessionEnv`), so the new pane is nobody's child.

**Both ends are linked**: `HandedOffFrom` on the new record and `HandedOffTo`
on the old one. They are written under the hook lock against a fresh read,
because the new session's hooks may already have written its record. They are
lineage, never a status. `status --json` carries both.

**The run that handed off ends `exited`, not `done`.** That is the answer to
"whatever the correct signal is". Done means live in this cockpit; it is what
the tracker writes when a deploy goes green, and what velocity and the products
lens count as shipped. A handoff does not make the sender's work live. Its own
pull request may still be open, and the tracker still follows an exited
record's branch, so the record becomes done when that pull request merges and
deploys. A dispatcher that is already live keeps done, because a handoff takes
nothing back. The reason is `state.ReasonHandedOff` plus what it handed off to.
The record is **held** and not dismissed (ADR 0012), because nobody watched it
end: it stays under the finished divider, saying "handed off", until the human
presses `x`.

**The session's own wind-down cannot reopen it.** `hookcmd` holds a record in
the handoff's ending (`state.HandedOff`: finished, with that reason) exactly as
it holds a done one. Only a human's prompt or a dialog going up reopens it
(`reopensDone`). The fan-out bookkeeping and the commit refresh still apply.
The link alone holds nothing: once anything else has ended the record, it is
an ordinary finished record that happens to have successors.

**The run ends only if the launch worked.** If the handoff fails, the work
stays with the dispatcher that has it. A launch that failed after its record
was written is kept, as ADR 0009 keeps one, and still says where it came from.
The command's error says both of those things.

Worked means claude reporting in, not the supervisor accepting a pane (ADR
0038). The first handoff in real use, to player-app on 2026-11-26, ended its
dispatcher's run and said "handed off" over a claude that had died at once in
a deleted directory. So `Handoff` waits on `AwaitStart` before it ends
anything. A claude that died comes back as a `StartError` carrying claude's
own words, and the launch is kept as a row that did not start. One that
nothing could confirm within `StartWait` is said as not a handoff yet, with
the two linked in the cockpit. In both cases the run goes on.

**A dispatch is told it can do this.** A launch in a repo that has siblings in
its product (`repos.Repo.Siblings`, found by discovery) gets one sentence before
the working contract. The sentence names the product, the repos beside it, and
the command with the binary's durable path (`binpath.Self`, the reasoning `init`
already used for the hooks). It also says that the handoff finishes this
dispatcher, so the dispatcher's own part should go up first. A repo alone in its
product, or in none, is told nothing. Like the contract, the sentence belongs to
the brief and Resume does not add it again. A dispatch started by a handoff is
told the same, so work can go on along a chain of repos.

**The cockpit shows both ends.**

- SIGNAL says "handed off", below the row's own merge or deploy and above
  "marked shipped" and "stopped".
- The new row's detail panel says where its work came from. The old row's
  panel leads with the reason, so the line repeats only what the reason does
  not say: a second successor.
- History counts handoffs apart from shipped and stopped, since a handoff is
  neither.
- The products lens no longer counts a handoff among the dispatches turned down.

## Consequences

- One handoff can be followed by another from the same run. A change with two
  consumers gets two successors, and the first one stays the reason.
- After the handoff, the old session is still at its prompt. ⏎ on its held row
  attaches to it, and a prompt typed there reopens the record, as it would a
  done one.
- The sentence lengthens every dispatch prompt in a product with more than one
  repo by about seventy words.
- The tracker is what moves a handed-off record on to done, and it runs only
  while a cockpit is open. Until it does, the record stays exited.
- The new pane takes the environment of the session that asked for it, minus
  that session's own variables. A target repo with a flake is still launched
  under that flake (ADR 0014). A target without one sees whatever the
  handing-off session had on PATH, the same way a dispatch from the cockpit
  sees the cockpit's PATH.
- `ScrubSessionEnv` is the same function as #89's. Whichever of the two lands
  second keeps one copy.
- As ADR 0028 found, a hook binary older than the cockpit drops record fields
  it does not know when it saves. A machine whose hook is an older build than
  the binary that ran `handoff` loses the link on the next hook save, and the
  hold with it. Keep the two the same build.
