// Package gh wraps the GitHub CLI for PR and deploy signals. Every call is
// best-effort: a repo without a GitHub remote, an offline machine, or a
// missing gh binary degrades to "no signal", never an error surfaced to the
// cockpit loop.
package gh

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

type PR struct {
	Number   int        `json:"number"`
	State    string     `json:"state"` // OPEN, MERGED, CLOSED
	URL      string     `json:"url"`
	MergedAt *time.Time `json:"mergedAt"`
}

// PRForBranch returns the PR whose head is the given branch, preferring a
// live one (merged or open) over closed. Nil when there is none.
//
// Cached like every other forge read. track.Refresh asks this for every
// dispatch that is not done, on every poll — and most of a long-lived state
// dir is exited records that will never reach done, so an uncached read here
// was a fixed per-poll tax that grew with history and never came down. The
// actions that make the answer stale (a merge, a jump-in) drop the cache
// themselves, so the poll is the only caller a TTL delays.
func PRForBranch(repoPath, branch string) *PR {
	return memo("prforbranch:"+repoPath+":"+branch, PRTTL, func() *PR {
		return prForBranchUncached(repoPath, branch)
	})
}

func prForBranchUncached(repoPath, branch string) *PR {
	out, err := run(command(repoPath, "pr", "list", "--head", branch, "--state", "all",
		"--json", "number,state,url,mergedAt"))
	if err != nil {
		return nil
	}
	var prs []PR
	if json.Unmarshal(out, &prs) != nil || len(prs) == 0 {
		return nil
	}
	for _, preferred := range []string{"MERGED", "OPEN"} {
		for i := range prs {
			if prs[i].State == preferred {
				return &prs[i]
			}
		}
	}
	return &prs[0]
}

var deployRe = regexp.MustCompile(`(?i)deploy|release|publish|ship|prod`)

var (
	wfMu    sync.Mutex
	wfCache = map[string][]string{}
)

func workflows(repoPath string) []string {
	wfMu.Lock()
	cached, ok := wfCache[repoPath]
	wfMu.Unlock()
	if ok {
		return cached
	}
	var names []string
	if out, err := run(command(repoPath, "workflow", "list", "--json", "name")); err == nil {
		var rows []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(out, &rows) == nil {
			for _, r := range rows {
				names = append(names, r.Name)
			}
		}
	}
	wfMu.Lock()
	wfCache[repoPath] = names
	wfMu.Unlock()
	return names
}

// deployTarget is the workflow a repo deploys with: the override from config
// if set, otherwise the first workflow with a deploy-ish name. Empty means the
// repo has no deploy workflow at all — a real answer, not a failure to look.
func deployTarget(repoPath, override string) string {
	if override != "" {
		return override
	}
	for _, name := range workflows(repoPath) {
		if deployRe.MatchString(name) {
			return name
		}
	}
	return ""
}

// deployRun is one run of a repo's deploy workflow.
type deployRun struct {
	Conclusion string    `json:"conclusion"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

// deployRuns lists the workflow's recent runs, newest first. One cached read
// shared by DeploySignal and DeployStatus: the tracker and the product lens
// were each fetching the same list on the same poll, which is both a wasted
// request and the only way the two could ever disagree about a deploy.
func deployRuns(repoPath, target string) []deployRun {
	return memo("deployruns:"+repoPath+":"+target, RepoTTL, func() []deployRun {
		out, err := run(command(repoPath, "run", "list", "--workflow", target,
			"--json", "conclusion,createdAt,status", "--limit", "20"))
		if err != nil {
			return nil
		}
		var runs []deployRun
		if json.Unmarshal(out, &runs) != nil {
			return nil
		}
		return runs
	})
}

// DeploySignal reports whether a deploy workflow succeeded after `since`.
// hasWorkflow=false means the repo has no deploy workflow at all — callers
// treat merge itself as "live".
func DeploySignal(repoPath string, since time.Time, override string) (deployed bool, at time.Time, hasWorkflow bool) {
	target := deployTarget(repoPath, override)
	if target == "" {
		return false, time.Time{}, false
	}
	for _, r := range deployRuns(repoPath, target) {
		if r.Conclusion == "success" && r.CreatedAt.After(since) {
			return true, r.CreatedAt, true
		}
	}
	return false, time.Time{}, true
}

// PRsCreatedToday counts PRs the user launched today across all of GitHub —
// one search call, no per-repo fan-out. ok=false when gh is unavailable.
func PRsCreatedToday() (count int, ok bool) {
	if !Available() {
		return 0, false
	}
	out, err := run(exec.Command("gh", "search", "prs",
		"--author=@me", "--created="+time.Now().Format("2006-01-02"),
		"--json", "url", "--limit", "200"))
	if err != nil {
		return 0, false
	}
	var rows []struct{}
	if json.Unmarshal(out, &rows) != nil {
		return 0, false
	}
	return len(rows), true
}

func command(repoPath string, args ...string) *exec.Cmd {
	cmd := exec.Command("gh", args...)
	cmd.Dir = repoPath
	return cmd
}

// DeployPipeline is a repo's deploy workflow and the state of its most recent
// run. Name is empty when the repo has no deploy workflow at all, which is a
// real answer — "merge is live" — not a failure to look.
type DeployPipeline struct {
	Name       string
	Status     string // completed | in_progress | queued
	Conclusion string // success | failure | cancelled | "" while running
	At         time.Time
	RunsToday  int
	// Successes is how many runs of this workflow succeeded in the window the
	// caller asked about — the deployments that actually happened, as opposed
	// to the dispatches the cockpit happens to have started.
	Successes int
}

// DeployStatus reports the repo's deploy workflow and its latest run, using the
// same target selection as DeploySignal so the product lens and the "done means
// live" tracker can never disagree about which workflow is the deploy.
func DeployStatus(repoPath, override string) DeployPipeline {
	return memo("deploy:"+repoPath+":"+override, RepoTTL, func() DeployPipeline {
		if !Available() {
			return DeployPipeline{}
		}
		target := deployTarget(repoPath, override)
		if target == "" {
			return DeployPipeline{}
		}
		rows := deployRuns(repoPath, target)
		p := DeployPipeline{Name: target}
		day := time.Now().Add(-24 * time.Hour)
		week := time.Now().AddDate(0, 0, -7)
		for i, r := range rows {
			if i == 0 {
				p.Status, p.Conclusion, p.At = r.Status, r.Conclusion, r.CreatedAt
			}
			if r.CreatedAt.After(day) {
				p.RunsToday++
			}
			if r.Conclusion == "success" && r.CreatedAt.After(week) {
				p.Successes++
			}
		}
		return p
	})
}
