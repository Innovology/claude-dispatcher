package dispatch

import "strings"

// fanout.go is the dispatch form's FAN OUT switch: whether the session may
// spread the work across multiple agents when the task warrants it.
//
// Unlike the mode and the model, fanning out is not a launch flag — Claude
// Code's opt-in for multi-agent orchestration is the keyword "ultracode"
// appearing in the prompt itself. So the switch is prompt composition: on, the
// launch appends one sentence carrying the keyword; off, the prompt goes out
// as written and the session works alone. The choice is recorded on the
// dispatch (state.Dispatch.FanOut) so screens can say which kind of session
// they are looking at, but it is a property of the brief, not of the session:
// a resumed dispatcher's follow-up prompt is the human's own message, and this
// never edits what a human typed.

// FanOutInstruction is the sentence a fan-out dispatch closes its prompt with.
// The word "ultracode" is the load-bearing part — it is Claude Code's opt-in
// keyword for multi-agent workflows — and the rest keeps the offer
// conditional: fan out where the task splits, not everywhere.
const FanOutInstruction = "ultracode: fan out across multiple agents where the task splits into independent parts; work solo where it does not."

// withFanOut appends the opt-in sentence when fanOut is on and the prompt does
// not already carry the keyword — a human who typed "ultracode" themselves has
// already opted in, and saying it twice reads as noise.
func withFanOut(prompt string, fanOut bool) string {
	if !fanOut || strings.Contains(strings.ToLower(prompt), "ultracode") {
		return prompt
	}
	return prompt + "\n\n" + FanOutInstruction
}
