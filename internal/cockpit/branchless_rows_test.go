package cockpit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// branchlessRec is a branchless dispatcher that started on a repo, as the
// launch records one: no branch, no commits, its product named.
func branchlessRec(feature string) *state.Dispatch {
	return &state.Dispatch{ID: "bl-" + feature, Feature: feature, RepoName: "player-app", Product: "pp", Branchless: true}
}

// A branchless dispatcher's answer is the conversation, so it has nothing to
// ship and never will: "commits stay the condition" would leave y off its row
// for good, and the human's word that the conversation is over would have no
// key. It gets y as "mark done" — not "mark shipped", which is a claim that
// something went live.
func TestABranchlessRowCanBeMarkedDoneWithNothingShipped(t *testing.T) {
	for _, kind := range []string{"turn-done", "running", "parked"} {
		rec := branchlessRec("review 163")
		rec.Status = state.StatusNeedsInput
		var y *cqAct
		for _, a := range cqActs(rec, kind) {
			if a.k == "y" {
				y = &a
			}
		}
		if y == nil {
			t.Errorf("%s: a branchless row offers no y", kind)
			continue
		}
		if y.d != "mark done" || !strings.Contains(y.ok, "marked done") {
			t.Errorf("%s: y = %q / %q, want mark done", kind, y.d, y.ok)
		}
	}
	// It still asks first while the session is going, as mark shipped does.
	rec := branchlessRec("review 163")
	rec.Status = state.StatusWorking
	for _, a := range cqActs(rec, "running") {
		if a.k == "y" && !strings.Contains(a.confirm, "done while its session is still working") {
			t.Errorf("confirm = %q", a.confirm)
		}
	}
}

// And the record says what happened in the same words: done, not shipped.
func TestMarkingABranchlessDispatcherDoneSaysDone(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	rec := branchlessRec("sitrep")
	liveRecords = map[string]*state.Dispatch{"sitrep": rec}

	am, _ := markDoneCmd("sitrep")().(actionMsg)
	if !strings.Contains(am.notice, "marked done") || rec.StatusReason != "marked done" || rec.Status != state.StatusDone {
		t.Errorf("notice %q, record %s/%q; want it marked done", am.notice, rec.Status, rec.StatusReason)
	}
}

// Where a branch would be named the row says there is none, rather than
// leaving a gap that reads as a launch that failed before branching. A
// conversation about the whole product names no repo, so its repo cell says
// what it spans and its product comes from the record — the config can only
// map a repo to a product, and this record has no repo to map.
func TestABranchlessRowSaysNoBranchAndWhatItSpans(t *testing.T) {
	rec := branchlessRec("review 163")
	if got := cqRef("gh", rec); got != "no branch" {
		t.Errorf("ref = %q, want no branch", got)
	}
	if got := cqRepoOf(rec); got != "player-app" {
		t.Errorf("repo cell = %q, want the repo it started on", got)
	}

	whole := &state.Dispatch{Feature: "byga sitrep", Product: "pp", Branchless: true}
	if got := cqRepoOf(whole); got != "all repos" {
		t.Errorf("whole-product repo cell = %q, want all repos", got)
	}
	ctx := &collectCtx{cfg: &config.Config{Products: map[string][]string{"pp": {"player-app"}}}}
	if got := ctx.productFor(whole); got != "pp" {
		t.Errorf("whole-product row filed under %q, want pp", got)
	}
	// A PR still wins where there is one, exactly as for any row.
	rec.PRNumber = 12
	if got := cqRef("gh", rec); got != "#12" {
		t.Errorf("ref with a PR = %q", got)
	}
}

// The product panel's resume overlay falls back to a fresh dispatch when a
// record has no conversation to reopen. For a branchless record that fallback
// would cut a branch named after it — or, about a whole product, fail on a repo
// it never had — when what it asked for was a conversation. Resume already
// starts a never-started record again from its own prompt and in its own
// folder (dispatch.Resume's relaunch), so that is where a branchless one goes.
func TestTheProductPanelStartsABranchlessRecordAgainRatherThanCuttingABranch(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	rec := &state.Dispatch{ID: "bl-1", Feature: "pp roadmap", Product: "pp", Branchless: true, Prompt: "where is pp going?"}
	liveByID = map[string]*state.Dispatch{rec.ID: rec}

	m := newModel()
	m.resumeOpen, m.resumeText = true, "and the calendar?"
	m.resumeAt = &resumeTarget{id: rec.ID, feature: rec.Feature}
	m, cmd := m.updateProduct("enter")
	if cmd == nil || strings.HasPrefix(m.notice, "dispatching") {
		t.Fatalf("notice %q: a branchless record was dispatched afresh", m.notice)
	}
	// The panel calls the launch directly rather than through a seam, so what
	// it handed back is the evidence: a launch reports as a launch.
	if msg, isLaunch := cmd().(launchedMsg); isLaunch {
		t.Errorf("the overlay launched a new dispatch: %+v", msg)
	}
}

// "mark done" exists so a conversation's ending claims nothing went live, and
// the record says so — but every screen that reads an ending off the status
// alone read it as a ship. History said "marked shipped", velocity counted a
// deploy and priced nothing for it, the floor called it live. A conversation
// that is over is over, and none of those.
func TestADoneConversationIsNotAShip(t *testing.T) {
	rec := branchlessRec("byga sitrep")
	rec.Status, rec.StatusReason = state.StatusDone, "marked done"
	if got := cqEnded(rec); got != "marked done" {
		t.Errorf("history says %q, want marked done", got)
	}
	if _, live := velLiveTime(rec); live {
		t.Error("velocity counts a conversation as a feature that went live")
	}
	if got := floorState(rec); got == "live" {
		t.Error("the floor calls a conversation live")
	}
	// A branch dispatch marked by hand is still the ship it always was.
	shipped := testRec("billing", 3)
	shipped.Status = state.StatusDone
	if cqEnded(shipped) != "marked shipped" || floorState(shipped) != "live" {
		t.Errorf("a shipped feature changed: %q / %q", cqEnded(shipped), floorState(shipped))
	}
	if _, live := velLiveTime(shipped); !live {
		t.Error("a shipped feature stopped counting as live")
	}
}

// Sentences that name where a dispatcher is ("attaching to X session…",
// "resumed … in X") read off the repo, and a whole-product conversation names
// none: they said "attaching to  session…". They name the product instead.
func TestAWholeProductConversationIsNamedInSentences(t *testing.T) {
	whole := &state.Dispatch{Feature: "pp roadmap", Product: "pp", Branchless: true, Status: state.StatusWorking}
	if got := cqPlaceOf(whole); got != "all of pp" {
		t.Errorf("place = %q, want all of pp", got)
	}
	if got := cqPlaceOf(branchlessRec("x")); got != "player-app" {
		t.Errorf("a conversation on a repo is named %q, want the repo", got)
	}
	for _, kind := range []string{"running", "parked"} {
		for _, a := range cqActs(whole, kind) {
			if a.k == "⏎" && a.ok != "attaching to all of pp session…" {
				t.Errorf("%s: ⏎ says %q", kind, a.ok)
			}
		}
	}
}

// A conversation that took a branch is a worker from then on: its row shows
// the branch, y is "mark shipped" once it has commits and not before, and a
// done one counts as the ship it is. Only a dispatcher still talking is the
// conversation "mark done" was made for.
func TestAConversationThatTookABranchIsAWorker(t *testing.T) {
	rec := branchlessRec("sync")
	rec.Branch, rec.RepoName, rec.Checkout, rec.CheckoutCut = "feature/sync", "playerpulse", "/wt/sync", true
	if got := cqRef("gh", rec); got != "feature/sync" {
		t.Errorf("ref = %q, want its branch", got)
	}
	rec.Status = state.StatusNeedsInput
	for _, a := range cqActs(rec, "turn-done") {
		if a.k == "y" {
			t.Errorf("y offered with no commits on its branch: %q", a.d)
		}
	}
	rec.Commits = []string{"sha"}
	var y string
	for _, a := range cqActs(rec, "turn-done") {
		if a.k == "y" {
			y = a.d
		}
	}
	if y != "mark shipped" {
		t.Errorf("y = %q, want mark shipped", y)
	}
	rec.Status, rec.PRState = state.StatusDone, ""
	if cqEnded(rec) != "marked shipped" || floorState(rec) != "live" {
		t.Errorf("a shipped worker reads %q / %q", cqEnded(rec), floorState(rec))
	}
	if _, live := velLiveTime(rec); !live {
		t.Error("a shipped worker is not counted live")
	}
}

// Killing a conversation that cut a worktree for its work removes that
// worktree, as a kill removes any dispatch's clean one — and never its own
// folder, and never a worktree it only went into.
func TestKillingAWorkerConversationRemovesTheWorktreeItCut(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := t.TempDir()
	git := func(dir string, args ...string) {
		full := append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git(repo, "init", "-q", "-b", "main")
	git(repo, "commit", "-q", "--allow-empty", "-m", "base")
	cut := filepath.Join(t.TempDir(), "sync")
	git(repo, "worktree", "add", "-q", "-b", "feature/sync", cut)
	entered := filepath.Join(t.TempDir(), "theirs")
	git(repo, "worktree", "add", "-q", "-b", "umberto/x", entered)
	home := t.TempDir()

	for _, c := range []struct {
		feature, path string
		cutByUs       bool
	}{{"cut", cut, true}, {"entered", entered, false}} {
		rec := &state.Dispatch{ID: "k-" + c.feature, Feature: c.feature, Branchless: true, RepoPath: repo,
			WorktreePath: home, Branch: "b", Checkout: c.path, CheckoutCut: c.cutByUs, Status: state.StatusWorking}
		if err := state.Save(rec); err != nil {
			t.Fatal(err)
		}
		liveRecords = map[string]*state.Dispatch{c.feature: rec}
		killCmd([]string{c.feature})()
		_, err := os.Stat(c.path)
		if gone := os.IsNotExist(err); gone != c.cutByUs {
			t.Errorf("%s: worktree gone = %v, want %v", c.feature, gone, c.cutByUs)
		}
		if _, err := os.Stat(home); err != nil {
			t.Errorf("%s: the conversation's own folder went with it", c.feature)
		}
	}
}
