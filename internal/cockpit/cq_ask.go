package cockpit

// cq_ask.go is what a stopped dispatcher is actually asking, in its own words,
// and what an API error has done to one — the two facts a waiting row used to
// leave the human to attach and find out.
//
// Both quote. cqAsk picks a sentence out of the session's own last message and
// never rewrites it; cqFailSignal names Claude Code's own error category and the
// retry the cockpit has scheduled. Neither guesses: a message with no ask in it
// yields "", and the row falls back to saying the turn finished.

import (
	"regexp"
	"strings"
	"time"

	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// cqInvite matches a sentence that hands the next move to the human without a
// question mark: "Say the word and I'll merge it." is the commonest ask in the
// transcripts and has none.
var cqInvite = regexp.MustCompile(`(?i)\b(say the word|let me know|tell me|your call|want me to|shall i|should i|would you like|do you want|ok to|okay to)\b`)

// cqSentenceBreak splits prose at a sentence end followed by what starts one.
var cqSentenceBreak = regexp.MustCompile(`([.!?])\s+([A-Z*` + "`" + `(\[])`)

// cqAskParagraphs is how far from the end an ask is looked for. The ask closes
// the message; a question five paragraphs up is one the message went on past.
const cqAskParagraphs = 3

// cqAsk is the question a stopped turn ended on, verbatim — the last sentence
// in the closing paragraphs that is a question or hands the move over — or ""
// when the message asks nothing.
//
// Measured against 949 real turn endings it finds an ask in 401; the rest are
// reports, which is the right answer for them. Emphasis markers are dropped
// because the terminal would print them as asterisks, and a list is split into
// its items so "Say the word." does not run on into the bullet under it.
func cqAsk(said string) string {
	paras := strings.Split(strings.ReplaceAll(strings.TrimSpace(said), "\r\n", "\n"), "\n\n")
	for i, seen := len(paras)-1, 0; i >= 0 && seen < cqAskParagraphs; i-- {
		p := strings.TrimSpace(paras[i])
		if p == "" {
			continue
		}
		seen++
		lines := strings.Split(p, "\n")
		for j := len(lines) - 1; j >= 0; j-- {
			line := strings.NewReplacer("**", "", "__", "").Replace(lines[j])
			line = strings.TrimLeft(strings.TrimSpace(line), "-*•> ")
			sents := strings.Split(cqSentenceBreak.ReplaceAllString(line, "$1\n$2"), "\n")
			for k := len(sents) - 1; k >= 0; k-- {
				s := strings.Join(strings.Fields(sents[k]), " ")
				if s == "" {
					continue
				}
				if strings.HasSuffix(s, "?") || cqInvite.MatchString(s) {
					return s
				}
			}
		}
	}
	return ""
}

// cqErrorName is Claude Code's error category in words: "server_error" →
// "server error".
func cqErrorName(f *state.Failure) string {
	return strings.ReplaceAll(f.Error, "_", " ")
}

// cqFailSignal is the SIGNAL clause for a dispatcher an API error stopped, or
// "" for one no error touched. It says which error, and what is being done
// about it: a retry coming (and when), a retry sent and the session going
// again, or nothing more the machine will try.
func cqFailSignal(rec *state.Dispatch, now time.Time) string {
	f := rec.Failure
	if f == nil {
		return ""
	}
	name := cqErrorName(f)
	steps := itoa(len(dispatchpkg.RetryBackoff))
	if rec.Status == state.StatusWorking {
		if f.Retries == 0 {
			return "" // the human answered it themselves; it is going again
		}
		return "retried after " + name + " · " + itoa(f.Retries) + " of " + steps
	}
	if dispatchpkg.RetryPending(rec, now) {
		due, at := dispatchpkg.RetryDue(rec, now)
		next := itoa(f.Retries+1) + " of " + steps
		if due {
			return "api error · " + name + " · retry " + next + " now"
		}
		return "api error · " + name + " · retry " + next + " in " + cqUntil(at, now)
	}
	if f.Transient() && f.Retries > 0 {
		return "api error · " + name + " · " + itoa(f.Retries) + " retries spent"
	}
	return "api error · " + name
}

// cqFailLead is the detail panel's sentence for a stopped-by-error row: the
// error in Claude Code's words, with its detail when it sent one.
func cqFailLead(rec *state.Dispatch) string {
	f := rec.Failure
	lead := "An API error ended its turn: " + cqErrorName(f)
	if f.Detail != "" {
		lead += " (" + f.Detail + ")"
	}
	if !f.Transient() {
		return lead + ". This one is yours to fix; retrying would not get past it."
	}
	return lead + "."
}

// cqUntil is how long until t, in cqAge's units.
func cqUntil(t, now time.Time) string {
	d := t.Sub(now)
	switch {
	case d < time.Minute:
		return itoa(int((d+time.Second-1)/time.Second)) + "s"
	case d < time.Hour:
		return itoa(int((d+time.Minute-1)/time.Minute)) + "m"
	}
	return itoa(int(d/time.Hour)) + "h"
}
