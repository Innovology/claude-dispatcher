package dispatch

import (
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
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

// A conversation starts as one and may become a worker or an orchestrator, so
// its contract does not forbid work: it forbids changing the human's own
// checkouts, and sends work onto a branch first, by the verbs its CLAUDE.md
// names. Auto's "commit as you go, open the PR" is how to carry work through
// once there is a branch, so the brief says it for then; the opening contract
// would otherwise ask a conversation for a PR before it has a branch to open
// one from. Manual's check-in before committing and plan's plan put up for
// approval stay — both stop for the human before any change, which is what a
// conversation turning into work in those modes should do.
func TestABranchlessContractSendsWorkOntoABranchFirst(t *testing.T) {
	for _, mode := range Modes() {
		c := withContract("x", mode, BranchNone)
		for _, want := range []string{"change nothing in them", "onto a branch of its own or send it out", "CLAUDE.md"} {
			if !strings.Contains(c, want) {
				t.Errorf("%s: the branchless contract lacks %q:\n%s", mode, want, c)
			}
		}
	}
	if strings.Contains(withContract("x", ModeAuto, BranchNone), "Commit as you go") {
		t.Error("auto asks a conversation to commit before it has a branch")
	}
	if m := withContract("x", ModeManual, BranchNone); !strings.Contains(m, "check in before committing") {
		t.Errorf("manual lost its check-in:\n%s", m)
	}
	if p := withContract("x", ModePlan, BranchNone); !strings.Contains(p, "put the plan up for approval") {
		t.Errorf("plan lost its plan:\n%s", p)
	}
}

// The brief is where the two ways into work are written down, with the very
// command that reaches this build.
func TestTheBriefNamesBothWaysIntoWork(t *testing.T) {
	prev := selfCommand
	selfCommand = func() string { return "/opt/cd" }
	t.Cleanup(func() { selfCommand = prev })
	b := BranchlessBrief(Scope{Product: "pp", Repos: []repos.Repo{{Name: "app", Path: "/src/app"}}}, "talk")
	for _, want := range []string{"`/opt/cd worktree --repo <repo> <name>`", "`/opt/cd dispatch --repo <repo>",
		"commit as you go, open the PR", "`dispatched_by`", "do not edit, commit, or switch branches in them"} {
		if !strings.Contains(b, want) {
			t.Errorf("brief lacks %q:\n%s", want, b)
		}
	}
}

// A repo whose worktrees are cut in a folder that holds its checkout — the
// project layout, <project>/.bare beside <project>/main — is given that folder
// once, not the checkout inside it as well.
func TestAProjectFolderCoversTheCheckoutInIt(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	project := t.TempDir()
	s := Scope{Repos: []repos.Repo{
		{Name: "app", Path: filepath.Join(project, "main"), WorktreeRoot: project},
		{Name: "lib", Path: "/src/lib"},
	}}
	got := s.dirs()
	want := []string{project, "/src/lib", filepath.Join(state.WorktreesDir(), "lib")}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("dirs = %v, want %v", got, want)
	}
}
