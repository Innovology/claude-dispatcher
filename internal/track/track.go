// Package track closes the shipping loop: it links each open dispatch to its
// PR by branch name and flips features to done when they go live — "done
// means live". A repo with no deploy workflow treats merge itself as live.
//
// Refresh runs from the cockpit's periodic poll; changes are written to the
// state dir, and the cockpit's fsnotify watcher picks them up like any other
// status change. (Consequence: auto-done only advances while a cockpit is
// open somewhere.)
//
// It never marks a dispatch done out from under a session that is still
// working or waiting on an approval — a merged PR is not the end of a run.
// See midWork.
package track

import (
	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
)

// Refresh polls PR and deploy state for every non-done dispatch. Returns how
// many records changed.
func Refresh(ds []*state.Dispatch, cfg *config.Config) int {
	if !gh.Available() {
		return 0
	}
	updated := 0
	for _, d := range ds {
		if d.Status == state.StatusDone {
			continue
		}
		changed := false
		// A dispatcher that has stopped will not open a pull request; only a
		// human can now, by hand, on the branch it left behind. gh holds that
		// answer far longer than a live dispatcher's — see PRForBranch. It is
		// the difference between asking about a feature that shipped last week
		// every minute for ever and asking occasionally.
		if pr := gh.PRForBranch(d.RepoPath, d.Branch, finished(d)); pr != nil &&
			(pr.Number != d.PRNumber || pr.State != d.PRState) {
			d.PRNumber = pr.Number
			d.PRState = pr.State
			d.PRURL = pr.URL
			d.PRMergedAt = pr.MergedAt
			changed = true
		}
		// The done flip waits until the session behind the dispatch has stopped;
		// see midWork. The PR fields above are refreshed either way, so a row
		// still on the triage table keeps showing where its PR stands.
		//
		// There is deliberately no "DeployedAt == nil" guard here. A record that
		// shipped and was then reopened — hookcmd.reopensDone, when its session
		// turned out to still be going — already carries a deploy time, and
		// skipping it would leave it open forever once its session went quiet
		// again. Re-asking is cheap (gh memoises the read) and both branches
		// write the same timestamp they wrote the first time, so it is idempotent
		// and the reason stays the one the signal actually justifies.
		if !midWork(d) && d.PRState == "MERGED" && d.PRMergedAt != nil {
			deployed, at, hasWorkflow := gh.DeploySignal(
				d.RepoPath, *d.PRMergedAt, cfg.DeployWorkflows[d.RepoName])
			switch {
			case deployed:
				d.DeployedAt = &at
				d.Status = state.StatusDone
				d.StatusReason = "deployed — live"
				changed = true
			case !hasWorkflow:
				d.DeployedAt = d.PRMergedAt
				d.Status = state.StatusDone
				d.StatusReason = "PR merged (repo has no deploy workflow)"
				changed = true
			}
		}
		if changed {
			_ = state.Save(d)
			updated++
		}
	}
	return updated
}

// finished reports whether the session behind a dispatch has stopped for good.
//
// Deliberately narrower than "not midWork": a needs-input dispatcher is waiting
// on the human and will carry on the moment they answer, so it may well open a
// PR in the next minute and is worth asking about at the ordinary rate. An
// exited one has no session left to open anything.
func finished(d *state.Dispatch) bool {
	return d.Status == state.StatusExited
}

// midWork reports whether the session behind a dispatch is still doing
// something: working, stopped at a permission prompt it needs answered, or not
// yet started.
//
// Such a dispatch is not done however the forge reads. These dispatchers are
// told to open and merge their own PRs and keep going, so a merged PR mid-run
// is routine, not the end — and flipping the record to done there used to
// strand a live session off the triage table for good, because
// internal/hookcmd would not let anything the session said afterwards
// downgrade done. Waiting for the turn to end costs one poll; "done means
// live" still holds, a few seconds later.
//
// A needs-input dispatch is deliberately not mid-work: the turn is over and
// the feature shipped, which is exactly the case "done means live" is about.
func midWork(d *state.Dispatch) bool {
	switch d.Status {
	case state.StatusWorking, state.StatusBlocked, state.StatusLaunching:
		return true
	}
	return false
}
