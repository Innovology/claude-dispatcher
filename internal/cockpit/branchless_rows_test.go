package cockpit

import (
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
