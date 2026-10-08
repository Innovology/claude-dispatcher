// Package ask finds what a stopped Claude Code session is asking the human,
// in the session's own words.
//
// A turn ends with its headline first and its question last ("PR #700 is
// open…" … "Want me to merge it?"), and the question is the sentence a human
// needs to decide whether to answer, attach, or leave it. Of quotes it and
// never rewrites it; a message with no ask yields "", which is the right answer
// for a report. The cockpit's triage row and the status command both read it
// from here, so no two screens can quote different asks for one dispatcher.
package ask

import (
	"regexp"
	"strings"
)

// invite matches a sentence that hands the next move to the human without a
// question mark: "Say the word and I'll merge it." is the commonest ask in the
// transcripts and has none.
var invite = regexp.MustCompile(`(?i)\b(say the word|let me know|tell me|your call|want me to|shall i|should i|would you like|do you want|ok to|okay to)\b`)

// sentenceBreak splits prose at a sentence end followed by what starts one.
var sentenceBreak = regexp.MustCompile(`([.!?])\s+([A-Z*` + "`" + `(\[])`)

// paragraphs is how far from the end an ask is looked for. The ask closes
// the message; a question five paragraphs up is one the message went on past.
const paragraphs = 3

// Of is the question a stopped turn ended on, verbatim — the last sentence
// in the closing paragraphs that is a question or hands the move over — or ""
// when the message asks nothing.
//
// Measured against 949 real turn endings it finds an ask in 401; the rest are
// reports, which is the right answer for them. Emphasis markers are dropped
// because the terminal would print them as asterisks, and a list is split into
// its items so "Say the word." does not run on into the bullet under it.
func Of(said string) string {
	paras := strings.Split(strings.ReplaceAll(strings.TrimSpace(said), "\r\n", "\n"), "\n\n")
	for i, seen := len(paras)-1, 0; i >= 0 && seen < paragraphs; i-- {
		p := strings.TrimSpace(paras[i])
		if p == "" {
			continue
		}
		seen++
		lines := strings.Split(p, "\n")
		for j := len(lines) - 1; j >= 0; j-- {
			line := strings.NewReplacer("**", "", "__", "").Replace(lines[j])
			line = strings.TrimLeft(strings.TrimSpace(line), "-*•> ")
			sents := strings.Split(sentenceBreak.ReplaceAllString(line, "$1\n$2"), "\n")
			for k := len(sents) - 1; k >= 0; k-- {
				s := strings.Join(strings.Fields(sents[k]), " ")
				if s == "" {
					continue
				}
				if strings.HasSuffix(s, "?") || invite.MatchString(s) {
					return s
				}
			}
		}
	}
	return ""
}
