package dispatch

import "strings"

// goalLead and goalClose are the two lines DONE WHEN becomes in a prompt. They
// are named here, not in the form that writes them, because the record reads
// them back (GoalOf): a completion condition that only ever existed as a
// sentence could not be handed to anything that checks it — the steward
// pushing a dispatcher toward it, a row saying what it is for — and a writer
// and reader in two packages would drift apart the way ReasonQuestion warns.
const (
	goalLead  = "done when: "
	goalClose = "Keep working until that is true."
)

// GoalSentence is DONE WHEN as the prompt says it, "" for no goal.
func GoalSentence(goal string) string {
	goal = strings.Join(strings.Fields(goal), " ")
	if goal == "" {
		return ""
	}
	return goalLead + goal + "\n" + goalClose
}

// GoalOf is the completion condition a prompt carries — the last "done when:"
// line that GoalSentence's closing line follows — or "". It reads only that
// exact shape: a brief that merely mentions being done somewhere is a brief,
// and calling it a goal would be inventing one.
func GoalOf(prompt string) string {
	lines := strings.Split(strings.ReplaceAll(prompt, "\r\n", "\n"), "\n")
	for i := len(lines) - 2; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, goalLead) && strings.TrimSpace(lines[i+1]) == goalClose {
			return strings.TrimSpace(strings.TrimPrefix(line, goalLead))
		}
	}
	return ""
}
