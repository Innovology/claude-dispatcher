package steward

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Found in review: the permission rules match spellings, so every one of these
// walked past them. The guard reads what the command does.
func TestTheGuardReadsWhatACommandDoes(t *testing.T) {
	const exe = "/home/u/.local/bin/claude-dispatcher"
	for cmd, want := range map[string]string{
		// Recording a ruling asks, however the binary is reached.
		exe + " ruling add --product p --allow merge --pr any --until 6h merge greens": "ask",
		"claude-dispatcher ruling add --product p x":                                   "ask",
		"env FOO=1 " + exe + " ruling   add --product p x":                             "ask",
		`claude-dispatcher "ruling" 'add' --product p x`:                               "ask",
		exe + " status --json && " + exe + " ruling add --product p x":                 "ask",
		// The rulings file and the steward's own settings are never named.
		`python3 -c "open('../rulings.json','w').write('[]')"`: "deny",
		"cat ../RULINGS.json":                        "deny",
		"printf '{}' > ../\"rul\"ings.json":          "deny",
		"sed -i s/deny/allow/ .claude/settings.json": "deny",
		"echo {} > .claude/settings.local.json":      "deny",
		// Merging round the verb, in any spelling.
		"gh pr merge 12 --squash":                                 "deny",
		"gh -R o/r pr merge 12":                                   "deny",
		"/usr/local/bin/gh pr merge 12":                           "deny",
		"gh pr -R o/r merge 12 --auto":                            "deny",
		"gh api -X PUT repos/o/r/pulls/12/merge":                  "deny",
		"gh api repos/o/r/merges -f base=main -f head=feature/x":  "deny",
		`gh api graphql -f query='mutation{mergePullRequest(…)}'`: "deny",
		"git push origin HEAD:main":                               "deny",
		"git -C /src/app push":                                    "deny",
		"cd /src/app; git push":                                   "deny",
		// What the steward is there to do goes on to the rules as before.
		exe + " merge player-app 931 --head 0123":                  "",
		exe + " ruling list --product p":                           "",
		exe + " ruling end r1a2b3":                                 "",
		"gh pr view 12 --json mergeable,mergeStateStatus,mergedAt": "",
		"gh pr view 12 && " + exe + " merge app 12 --head 0123":    "",
		"git log --oneline -5":                                     "",
		"gh run list --branch feature/x 2>&1 | head":               "",
		"": "",
	} {
		if got := Judge("Bash", map[string]any{"command": cmd}).Decision; got != want {
			t.Errorf("%q: %q, want %q", cmd, got, want)
		}
	}
	// A tool that runs no command is the rules' to decide.
	if v := Judge("Read", map[string]any{"file_path": "../rulings.json"}); v.Decision != "" {
		t.Errorf("Read: %+v", v)
	}
}

// The hook speaks claude's PreToolUse answer, and says nothing to a payload it
// cannot read, so a format change leaves the rules deciding instead of refusing
// every tool.
func TestTheGuardAnswersAsAHook(t *testing.T) {
	in := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"gh pr merge 12","description":"x"}}`
	var out bytes.Buffer
	ps := stdin
	t.Cleanup(func() { stdin = ps })
	stdin = strings.NewReader(in)
	if code := Run([]string{"guard"}, &out, &out); code != 0 {
		t.Fatalf("guard: %d %s", code, out.String())
	}
	var got struct {
		HookSpecificOutput struct {
			HookEventName, PermissionDecision, PermissionDecisionReason string
		}
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v: %q", err, out.String())
	}
	if h := got.HookSpecificOutput; h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" || !strings.Contains(h.PermissionDecisionReason, "merge <repo> <pr>") {
		t.Fatalf("answer = %+v", h)
	}
	for _, quiet := range []string{"not json", `{"tool_name":"Bash","tool_input":{"command":"git log"}}`} {
		out.Reset()
		stdin = strings.NewReader(quiet)
		if code := Run([]string{"guard"}, &out, &out); code != 0 || out.Len() != 0 {
			t.Errorf("%q: code %d, said %q", quiet, code, out.String())
		}
	}
}
