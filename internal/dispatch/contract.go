package dispatch

import "strings"

// contract.go is the working contract a dispatch closes its prompt with: how
// far to take the work in the mode it was launched in, how to wait, and when a
// stop is warranted.
//
// It is composed at Launch, beside FAN OUT's sentence, so every way in gets it.
// It used to be the triage form's alone: the + overlay and every backlog launch
// (a Linear, GitHub or Azure ticket on enter) went out in auto mode saying
// nothing about how far to go, and got Claude Code's default — finish a step,
// stop, offer the next one. The flag (--permission-mode) says what the session
// may do without asking; this says how far to take the work. They are chosen
// together, from the one mode.
//
// The auto contract is written against what the transcripts show dispatchers
// stopping for (ADR 0018): 28% of turn endings offered a next step or asked a
// question, and 9% ended the turn to "check back once CI finishes". Claude
// Code already has the machinery for the second — a background task or Monitor
// keeps the turn alive and wakes it when the wait is over — so the contract
// asks for it by name rather than the cockpit inventing its own. And every
// mode's stop ends on its one question, which is the sentence the triage row
// quotes (cqAsk): a stop that says what it needs can be answered from the table.
//
// Like FAN OUT it is a property of the brief, not the session: Resume does not
// re-apply it, because a follow-up prompt is the human's own message.

// contractSentences is each mode's contract, one sentence per entry so a
// prompt that already carries some of it (a human who typed the old closing
// line into the + form) gains only what it lacks.
var contractSentences = map[Mode][]string{
	ModeAuto: {
		"Commit as you go, open the PR, and fix your own CI failures without stopping to ask.",
		"When you are waiting on something slow — CI, a deploy, a long job — wait on it with a background task or Monitor rather than ending your turn to check back later.",
		"Take the next step this brief implies instead of offering it.",
		"Stop only for a decision that is genuinely mine — outside this brief, destructive or irreversible, or needing access you do not have — and end your message on that one question.",
	},
	ModeManual: {
		"Do one pass, then stop and check in before committing, pushing or opening a PR.",
		"End your message on the one question you need answered.",
	},
	// Plan mode already stops claude from changing anything; the sentence says
	// what to spend the read-only pass on, so the plan that comes back is about
	// this work rather than a summary of the repo.
	ModePlan: {
		"Work out how you would do this and put the plan up for approval before changing anything.",
	},
}

// BranchlessInstruction leads the contract of a dispatch that cut no branch
// (branching.go), in every mode.
//
// A conversation is where it starts, not where it has to stay: the point is to
// talk about a product and then, when the talk arrives somewhere, become a
// worker on a branch of its own or an orchestrator sending work out. So the
// sentence says where it is — in the human's own checkouts, shared with them and
// with any session they started there, which it reads and never changes — and
// that work goes onto a branch first, by the two verbs its CLAUDE.md names
// (work.go). It replaces auto's "commit as you go, open the PR", which is how to
// carry work through once there is a branch, and the brief says that there.
const BranchlessInstruction = "This dispatch starts as a conversation, with no branch: the repositories CLAUDE.md lists are the human's own working copies, so read them freely and answer here, but change nothing in them — when the conversation turns into work, take it onto a branch of its own or send it out, as CLAUDE.md describes, and carry it through from there."

// autoShipSentence is auto's "commit as you go, open the PR": how to carry work
// through on a branch, which a conversation does not have yet. Its brief says
// it for when it does. Manual's check-in before committing and plan's plan put
// up for approval stay: both stop for the human before any change, which is
// what a conversation turning into work should do in those modes.
var autoShipSentence = contractSentences[ModeAuto][0]

// contractFor is the contract a dispatch closes with: the mode's, and for a
// dispatch with no branch, the branchless sentence leading it in place of
// auto's commit-and-PR.
func contractFor(mode Mode, b Branching) []string {
	sentences := contractSentences[mode.Normalize()]
	if !b.None() {
		return sentences
	}
	out := []string{BranchlessInstruction}
	for _, s := range sentences {
		if s != autoShipSentence {
			out = append(out, s)
		}
	}
	return out
}

// withContract closes the prompt with the contract, leaving out any sentence
// the prompt already carries.
func withContract(prompt string, mode Mode, b Branching) string {
	var missing []string
	for _, s := range contractFor(mode, b) {
		if !strings.Contains(prompt, s) {
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		return prompt
	}
	return strings.TrimRight(prompt, "\n") + "\n\n" + strings.Join(missing, " ")
}
