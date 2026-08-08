package gh

// Live read helpers used by the cockpit's data collectors. Every call is
// best-effort in the same spirit as gh.go: it shells out via the gh CLI with
// cmd.Dir pinned to the repo, and degrades to an empty/zero value on any
// error (missing gh, no remote, offline, malformed JSON). Callers never see an
// error.

import (
	"encoding/json"
	"strconv"
	"time"
)

// Issue is an open issue assigned to the current user.
type Issue struct {
	Number    int
	Title     string
	URL       string
	Body      string
	State     string
	Labels    []string
	UpdatedAt time.Time
}

// Issues returns open issues assigned to @me for the repo. The bool is false
// when gh is unavailable or the call/parse fails; true (with a possibly empty
// slice) otherwise.
func Issues(repoPath string) ([]Issue, bool) {
	type result struct {
		issues []Issue
		ok     bool
	}
	r := memo("issues:"+repoPath, RepoTTL, func() result {
		issues, ok := issuesUncached(repoPath)
		return result{issues, ok}
	})
	return r.issues, r.ok
}

func issuesUncached(repoPath string) ([]Issue, bool) {
	if !Available() {
		return nil, false
	}
	out, err := command(repoPath, "issue", "list",
		"--assignee", "@me", "--state", "open",
		"--json", "number,title,url,body,state,labels,updatedAt",
		"--limit", "100").Output()
	if err != nil {
		return nil, false
	}
	var rows []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"url"`
		Body   string `json:"body"`
		State  string `json:"state"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if json.Unmarshal(out, &rows) != nil {
		return nil, false
	}
	issues := make([]Issue, 0, len(rows))
	for _, r := range rows {
		labels := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		issues = append(issues, Issue{
			Number:    r.Number,
			Title:     r.Title,
			URL:       r.URL,
			Body:      r.Body,
			State:     r.State,
			Labels:    labels,
			UpdatedAt: r.UpdatedAt,
		})
	}
	return issues, true
}

// Checks summarises the CI check-run states for a PR.
type Checks struct {
	Passed  int
	Total   int
	Failing int
	Running int
}

// PRChecksFor counts the check-run states of a PR. Degrades to a zero Checks
// on any error.
func PRChecksFor(repoPath string, number int) Checks {
	return memo("checks:"+repoPath+":"+strconv.Itoa(number), PRTTL, func() Checks {
		return prChecksUncached(repoPath, number)
	})
}

func prChecksUncached(repoPath string, number int) Checks {
	var c Checks
	if !Available() {
		return c
	}
	out, err := command(repoPath, "pr", "checks", strconv.Itoa(number),
		"--json", "state").Output()
	if err != nil {
		// Fall back to the rollup view, which is populated even before
		// checks are queryable via `pr checks`.
		return prChecksRollup(repoPath, number)
	}
	var rows []struct {
		State string `json:"state"`
	}
	if json.Unmarshal(out, &rows) != nil {
		return c
	}
	for _, r := range rows {
		c.Total++
		classifyCheck(r.State, &c)
	}
	return c
}

func prChecksRollup(repoPath string, number int) Checks {
	var c Checks
	out, err := command(repoPath, "pr", "view", strconv.Itoa(number),
		"--json", "statusCheckRollup").Output()
	if err != nil {
		return c
	}
	var row struct {
		StatusCheckRollup []struct {
			State      string `json:"state"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if json.Unmarshal(out, &row) != nil {
		return c
	}
	for _, r := range row.StatusCheckRollup {
		c.Total++
		// Check runs report status+conclusion; status contexts report state.
		s := r.State
		if s == "" {
			if r.Status != "" && r.Status != "COMPLETED" {
				s = r.Status
			} else {
				s = r.Conclusion
			}
		}
		classifyCheck(s, &c)
	}
	return c
}

func classifyCheck(state string, c *Checks) {
	switch state {
	case "SUCCESS":
		c.Passed++
	case "FAILURE", "ERROR":
		c.Failing++
	case "PENDING", "IN_PROGRESS", "QUEUED":
		c.Running++
	}
}

// Review summarises the review posture of a PR.
type Review struct {
	Approvals        int
	ChangesRequested int
	Decision         string // APPROVED / CHANGES_REQUESTED / REVIEW_REQUIRED / ""
}

// PRReviewFor counts the latest review state per author and reports the
// aggregate review decision. Degrades to a zero Review on any error.
func PRReviewFor(repoPath string, number int) Review {
	return memo("review:"+repoPath+":"+strconv.Itoa(number), PRTTL, func() Review {
		return prReviewUncached(repoPath, number)
	})
}

func prReviewUncached(repoPath string, number int) Review {
	var rv Review
	if !Available() {
		return rv
	}
	out, err := command(repoPath, "pr", "view", strconv.Itoa(number),
		"--json", "reviewDecision,reviews").Output()
	if err != nil {
		return rv
	}
	var row struct {
		ReviewDecision string `json:"reviewDecision"`
		Reviews        []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			State       string    `json:"state"`
			SubmittedAt time.Time `json:"submittedAt"`
		} `json:"reviews"`
	}
	if json.Unmarshal(out, &row) != nil {
		return rv
	}
	rv.Decision = row.ReviewDecision

	// Keep the latest submitted review per author, then tally.
	latest := map[string]string{}
	latestAt := map[string]time.Time{}
	for _, r := range row.Reviews {
		who := r.Author.Login
		if r.State != "APPROVED" && r.State != "CHANGES_REQUESTED" {
			continue
		}
		if prev, ok := latestAt[who]; !ok || !r.SubmittedAt.Before(prev) {
			latest[who] = r.State
			latestAt[who] = r.SubmittedAt
		}
	}
	for _, state := range latest {
		switch state {
		case "APPROVED":
			rv.Approvals++
		case "CHANGES_REQUESTED":
			rv.ChangesRequested++
		}
	}
	return rv
}

// Run is a recent workflow run on a branch.
type Run struct {
	Name       string
	Status     string
	Conclusion string
	CreatedAt  time.Time
}

// RunsForBranch returns up to 10 recent workflow runs for the branch.
func RunsForBranch(repoPath, branch string) []Run {
	return memo("runs:"+repoPath+":"+branch, RepoTTL, func() []Run {
		return runsForBranchUncached(repoPath, branch)
	})
}

func runsForBranchUncached(repoPath, branch string) []Run {
	if !Available() {
		return nil
	}
	out, err := command(repoPath, "run", "list", "--branch", branch,
		"--json", "workflowName,status,conclusion,createdAt",
		"--limit", "10").Output()
	if err != nil {
		return nil
	}
	var rows []struct {
		WorkflowName string    `json:"workflowName"`
		Status       string    `json:"status"`
		Conclusion   string    `json:"conclusion"`
		CreatedAt    time.Time `json:"createdAt"`
	}
	if json.Unmarshal(out, &rows) != nil {
		return nil
	}
	runs := make([]Run, 0, len(rows))
	for _, r := range rows {
		runs = append(runs, Run{
			Name:       r.WorkflowName,
			Status:     r.Status,
			Conclusion: r.Conclusion,
			CreatedAt:  r.CreatedAt,
		})
	}
	return runs
}

// OpenPR is an open pull request in a repo.
type OpenPR struct {
	Number         int
	Title          string
	Author         string
	HeadRefName    string
	BaseRefName    string
	ReviewDecision string
	Additions      int
	Deletions      int
	CreatedAt      time.Time
}

// OpenPRsFor returns up to 50 open pull requests for the repo.
func OpenPRsFor(repoPath string) []OpenPR {
	return memo("openprs:"+repoPath, RepoTTL, func() []OpenPR {
		return openPRsForUncached(repoPath)
	})
}

func openPRsForUncached(repoPath string) []OpenPR {
	if !Available() {
		return nil
	}
	out, err := command(repoPath, "pr", "list", "--state", "open",
		"--json", "number,title,author,headRefName,baseRefName,reviewDecision,additions,deletions,createdAt",
		"--limit", "50").Output()
	if err != nil {
		return nil
	}
	var rows []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		HeadRefName    string    `json:"headRefName"`
		BaseRefName    string    `json:"baseRefName"`
		ReviewDecision string    `json:"reviewDecision"`
		Additions      int       `json:"additions"`
		Deletions      int       `json:"deletions"`
		CreatedAt      time.Time `json:"createdAt"`
	}
	if json.Unmarshal(out, &rows) != nil {
		return nil
	}
	prs := make([]OpenPR, 0, len(rows))
	for _, r := range rows {
		prs = append(prs, OpenPR{
			Number:         r.Number,
			Title:          r.Title,
			Author:         r.Author.Login,
			HeadRefName:    r.HeadRefName,
			BaseRefName:    r.BaseRefName,
			ReviewDecision: r.ReviewDecision,
			Additions:      r.Additions,
			Deletions:      r.Deletions,
			CreatedAt:      r.CreatedAt,
		})
	}
	return prs
}
