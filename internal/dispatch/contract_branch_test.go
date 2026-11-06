package dispatch

import (
	"strings"
	"testing"
)

// A dispatch that cut no branch has no pull request to open, so the auto
// contract's "commit as you go, open the PR" is the one sentence it must not
// be told — that sentence is what had review-163 asked for a PR on a brief that
// said "only provide the succinct report". What it is told instead leads, in
// every mode: answer in the conversation, and leave the human's checkouts as
// they are, since that is where it is reading.
func TestABranchlessContractAsksForAnAnswerNotAPullRequest(t *testing.T) {
	for _, mode := range Modes() {
		got := withContract("review PR 163 and report", mode, BranchNone)
		if !strings.HasPrefix(got, "review PR 163 and report\n\n"+BranchlessInstruction) {
			t.Errorf("%s: the branchless sentence does not lead the contract:\n%s", mode, got)
		}
		if strings.Contains(got, "open the PR") {
			t.Errorf("%s: a branchless dispatch was told to open a PR:\n%s", mode, got)
		}
	}
	// The rest of auto's contract is about waiting and stopping, which is as
	// true of a review as of a feature.
	auto := withContract("x", ModeAuto, BranchNone)
	for _, want := range []string{"background task or Monitor", "Take the next step", "end your message on that one question"} {
		if !strings.Contains(auto, want) {
			t.Errorf("branchless auto contract lost %q:\n%s", want, auto)
		}
	}
	// And a dispatch that cuts a branch is told exactly what it always was.
	if got, want := withContract("x", ModeAuto, BranchNew), "x\n\n"+strings.Join(contractSentences[ModeAuto], " "); got != want {
		t.Errorf("a branch dispatch's contract changed:\n%s", got)
	}
	if strings.Contains(withContract("x", ModeAuto, BranchNew), BranchlessInstruction) {
		t.Error("a branch dispatch was told it has no branch")
	}
}
