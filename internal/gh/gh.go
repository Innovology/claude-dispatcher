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
func PRForBranch(repoPath, branch string) *PR {
	out, err := command(repoPath, "pr", "list", "--head", branch, "--state", "all",
		"--json", "number,state,url,mergedAt").Output()
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
	if out, err := command(repoPath, "workflow", "list", "--json", "name").Output(); err == nil {
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

// DeploySignal reports whether a deploy workflow succeeded after `since`.
// The deploy workflow is the override from config if set, otherwise the
// first workflow with a deploy-ish name. hasWorkflow=false means the repo
// has no deploy workflow at all — callers treat merge itself as "live".
func DeploySignal(repoPath string, since time.Time, override string) (deployed bool, at time.Time, hasWorkflow bool) {
	target := override
	if target == "" {
		for _, name := range workflows(repoPath) {
			if deployRe.MatchString(name) {
				target = name
				break
			}
		}
	}
	if target == "" {
		return false, time.Time{}, false
	}
	out, err := command(repoPath, "run", "list", "--workflow", target,
		"--json", "conclusion,createdAt,status", "--limit", "20").Output()
	if err != nil {
		return false, time.Time{}, true
	}
	var runs []struct {
		Conclusion string    `json:"conclusion"`
		CreatedAt  time.Time `json:"createdAt"`
	}
	if json.Unmarshal(out, &runs) != nil {
		return false, time.Time{}, true
	}
	for _, r := range runs {
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
	out, err := exec.Command("gh", "search", "prs",
		"--author=@me", "--created="+time.Now().Format("2006-01-02"),
		"--json", "url", "--limit", "200").Output()
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
}

// DeployStatus reports the repo's deploy workflow and its latest run, using the
// same target selection as DeploySignal so the product lens and the "done means
// live" tracker can never disagree about which workflow is the deploy.
func DeployStatus(repoPath, override string) DeployPipeline {
	return memo("deploy:"+repoPath+":"+override, RepoTTL, func() DeployPipeline {
		if !Available() {
			return DeployPipeline{}
		}
		target := override
		if target == "" {
			for _, name := range workflows(repoPath) {
				if deployRe.MatchString(name) {
					target = name
					break
				}
			}
		}
		if target == "" {
			return DeployPipeline{}
		}
		out, err := command(repoPath, "run", "list", "--workflow", target,
			"--json", "conclusion,createdAt,status", "--limit", "20").Output()
		if err != nil {
			return DeployPipeline{Name: target}
		}
		var rows []struct {
			Conclusion string    `json:"conclusion"`
			Status     string    `json:"status"`
			CreatedAt  time.Time `json:"createdAt"`
		}
		if json.Unmarshal(out, &rows) != nil {
			return DeployPipeline{Name: target}
		}
		p := DeployPipeline{Name: target}
		cutoff := time.Now().Add(-24 * time.Hour)
		for i, r := range rows {
			if i == 0 {
				p.Status, p.Conclusion, p.At = r.Status, r.Conclusion, r.CreatedAt
			}
			if r.CreatedAt.After(cutoff) {
				p.RunsToday++
			}
		}
		return p
	})
}
