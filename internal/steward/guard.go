package steward

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// The guard is the steward's own PreToolUse hook (`steward guard`, installed in
// its settings for every tool), and it exists because the permission rules
// beside it match spellings. "ask: Bash(<exe> ruling add:*)" asks about that
// exact command and nothing else: the same verb from PATH, behind `env`, or a
// shell writing ../rulings.json directly matched no rule at all, and in auto
// mode a command that matches no rule goes to the classifier, not to the human.
// The same held for the deny list: `gh -R o/r pr merge`, an absolute gh, `gh api
// -X PUT …/pulls/N/merge` and `git -C <repo> push` each walked past the rule
// that was meant to stop them.
//
// So the guard reads every command a tool is about to run, not the way it is
// spelled: quotes and escapes are dropped, it is cut at the shell's separators,
// and each piece is matched on its words. A hook's "ask" stops an auto-mode
// session for the human even where an allow rule matches, and its "deny"
// refuses with the reason given — both measured on claude 2.1.326.
//
// What it is NOT is a boundary against a steward that sets out to get round it.
// The steward runs as the human's user, a command that builds a file name or a
// gh call at run time is not one any rule can read, and `merge` accepts a
// hand-written rulings.json. It holds the steward to its brief; it is not a
// sandbox, and the docs say so.

// A Verdict is the guard's answer about one tool call: Decision is "ask",
// "deny" or "" (no opinion — the permission rules decide as before).
type Verdict struct {
	Decision string
	Reason   string
}

var (
	// rulingAdd is recording a ruling, by any path to the binary.
	rulingAdd = regexp.MustCompile(`\bruling\s+add\b`)
	// ghMergeRoute is gh merging a pull request however the flags fall, the
	// REST merge endpoints (a pull request's, and the branch-into-branch one),
	// and the GraphQL mutations that merge or queue one.
	ghMergeRoute = regexp.MustCompile(`\bgh\b.*\bpr\b.*\bmerge\b|\brepos/\S*/merges?\b|mergepullrequest|enablepullrequestautomerge|enqueuepullrequest`)
	// gitPush is git pushing, whatever options come first.
	gitPush = regexp.MustCompile(`\bgit\b.*\bpush\b`)
	// pieceBreak cuts a command into the simple commands it runs.
	pieceBreak = regexp.MustCompile("&&|\\|\\||[;|&\n`]|\\$\\(")
)

// Judge is the guard's verdict on a tool call. Only a call that runs a command
// is read; everything else the permission rules decide.
func Judge(tool string, input map[string]any) Verdict {
	cmd, _ := input["command"].(string)
	if cmd == "" {
		return Verdict{}
	}
	flat := flatten(cmd)
	// Named anywhere, even mid-pipeline: there is no reason for the steward to
	// touch either file, and every reason it must not.
	if strings.Contains(flat, "rulings") {
		return Verdict{"deny", "the rulings file is the human's record: a ruling is added with `ruling add`, which asks them, and never written any other way"}
	}
	if strings.Contains(flat, "settings.json") || strings.Contains(flat, "settings.local.json") {
		return Verdict{"deny", "your settings are the cockpit's to write; they hold what you may do"}
	}
	ask := false
	for _, piece := range pieceBreak.Split(flat, -1) {
		switch {
		case ghMergeRoute.MatchString(piece):
			return Verdict{"deny", "merging goes through the dispatcher's own `merge <repo> <pr> --head <sha>`, which checks the ruling that allows it"}
		case gitPush.MatchString(piece):
			return Verdict{"deny", "a steward pushes nothing: the dispatchers own their branches"}
		case rulingAdd.MatchString(piece):
			ask = true
		}
	}
	if ask {
		return Verdict{"ask", "a ruling is the human's decision: confirm this one is what you said"}
	}
	return Verdict{}
}

// flatten is a command as the guard matches it: lower case, with the quotes and
// backslashes that change its spelling and not what runs taken out, and runs of
// blanks made one space.
func flatten(cmd string) string {
	cmd = strings.NewReplacer(`'`, "", `"`, "", `\`, "").Replace(strings.ToLower(cmd))
	return strings.Join(strings.FieldsFunc(cmd, func(r rune) bool { return r == ' ' || r == '\t' }), " ")
}

// guard is `steward guard`: the hook's payload in, claude's hook answer out.
// A payload it cannot read gets no answer, so a change in claude's hook format
// leaves the permission rules deciding rather than refusing every tool.
func guard(in io.Reader, out io.Writer) error {
	var p struct {
		ToolName  string         `json:"tool_name"`
		ToolInput map[string]any `json:"tool_input"`
	}
	if err := json.NewDecoder(in).Decode(&p); err != nil {
		return nil
	}
	v := Judge(p.ToolName, p.ToolInput)
	if v.Decision == "" {
		return nil
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       v.Decision,
			"permissionDecisionReason": v.Reason,
		},
	})
}
