// Package track closes the shipping loop: it links each open dispatch to its
// PR by branch name and flips features to done when they go live — "done
// means live". A repo with no deploy workflow treats merge itself as live.
//
// Refresh runs from the cockpit's periodic poll; changes are written to the
// state dir, and the cockpit's fsnotify watcher picks them up like any other
// status change. (Consequence: auto-done only advances while a cockpit is
// open somewhere.)
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
		if pr := gh.PRForBranch(d.RepoPath, d.Branch); pr != nil &&
			(pr.Number != d.PRNumber || pr.State != d.PRState) {
			d.PRNumber = pr.Number
			d.PRState = pr.State
			d.PRURL = pr.URL
			d.PRMergedAt = pr.MergedAt
			changed = true
		}
		if d.PRState == "MERGED" && d.PRMergedAt != nil && d.DeployedAt == nil {
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
			state.Save(d)
			updated++
		}
	}
	return updated
}
