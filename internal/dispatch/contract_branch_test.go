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

// Anything but "none" cuts a branch, so a caller that chose nothing and a
// record from before the choice existed both launch as they always did.
func TestBranchingNormalizesOntoTheDefault(t *testing.T) {
	for in, want := range map[Branching]Branching{
		"": BranchNew, "new": BranchNew, "none": BranchNone, " none ": BranchNone, "nope": BranchNew,
	} {
		if got := in.Normalize(); got != want {
			t.Errorf("Branching(%q).Normalize() = %q, want %q", in, got, want)
		}
	}
	if got := Branchings(); len(got) != 2 || got[0] != DefaultBranching || !got[1].None() {
		t.Errorf("Branchings() = %v, want the default first, then none", got)
	}
	if BranchNew.Hint() == BranchNone.Hint() || !strings.Contains(BranchNone.Hint(), "no branch") {
		t.Errorf("hints do not tell the two apart: %q / %q", BranchNew.Hint(), BranchNone.Hint())
	}
}

// Every mode's own contract assumes a change will be made: auto commits and
// opens a PR, manual checks in "before committing, pushing or opening a PR",
// plan puts up a plan "for approval before changing anything" — and once it is
// approved, edits the human's own checkouts. A branchless dispatch reads those
// checkouts and changes nothing in them, so none of that survives; what does is
// how to wait and when to stop. The first version of this test looked for "open
// the PR" alone, and manual's "opening a PR" walked past it.
func TestABranchlessContractInvitesNoChangeInAnyMode(t *testing.T) {
	for _, mode := range Modes() {
		rest := strings.Replace(withContract("x", mode, BranchNone), BranchlessInstruction, "", 1)
		for _, change := range []string{"PR", "commit", "push", "approval", "changing anything"} {
			if strings.Contains(rest, change) {
				t.Errorf("%s: the branchless contract still mentions %q:\n%s", mode, change, rest)
			}
		}
	}
	// Manual still ends on its question, which is what the row quotes.
	if m := withContract("x", ModeManual, BranchNone); !strings.Contains(m, "End your message on the one question") {
		t.Errorf("manual lost its closing question:\n%s", m)
	}
}
