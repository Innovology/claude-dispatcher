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
	out, err := run(command(repoPath, "issue", "list",
		"--assignee", "@me", "--state", "open",
		"--json", "number,title,url,body,state,labels,updatedAt",
		"--limit", "100"))
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
//
// The repo's open PRs come with their rollups attached in one read, so an open
// PR is answered from that and costs nothing. Only a merged or closed PR — one
// a dispatch record still points at, which is no longer in the open list — is
// worth a request of its own, and then only rarely: its check runs are history
// now, and re-reading history once a minute for every feature the portfolio has
// ever shipped is most of what a long-lived cockpit was spending.
func PRChecksFor(repoPath string, number int) Checks {
	open, asked := RepoPRs(repoPath)
	if d, ok := open[number]; ok {
		return d.Checks
	}
	return memo("checks:"+repoPath+":"+strconv.Itoa(number), ttlWhenAbsent(asked), func() Checks {
		return prChecksUncached(repoPath, number)
	})
}

// ttlWhenAbsent is how long to hold a pull request's signals when the repo's
// open list did not contain it. If the list answered, the PR is merged or
// closed and what it says has stopped changing. If the list failed, its silence
// proves nothing and the ordinary TTL stands.
func ttlWhenAbsent(asked bool) time.Duration {
	if asked {
		return SettledTTL
	}
	return PRTTL
}

func prChecksUncached(repoPath string, number int) Checks {
	var c Checks
	if !Available() {
		return c
	}
	// `gh pr checks` answers in its exit code as well as on stdout: 1 when a
	// check has failed, 8 while any is still pending. Those are precisely the
	// two states the cockpit polls hardest, so reading a non-zero exit as "no
	// answer" threw away the JSON we had already paid a request for and bought
	// the same answer again from the rollup — two GraphQL requests per running
	// or red PR, on every refresh, on the largest key class there is. Read
	// stdout first; the exit code only decides what to do when it is empty.
	out, _ := run(command(repoPath, "pr", "checks", strconv.Itoa(number),
		"--json", "state"))
	var rows []struct {
		State string `json:"state"`
	}
	if json.Unmarshal(out, &rows) != nil || len(rows) == 0 {
		// No checks reported (gh exits 1 and prints nothing), or gh failed
		// outright. Fall back to the rollup view, which is populated even
		// before checks are queryable via `pr checks`.
		return prChecksRollup(repoPath, number)
	}
	for _, r := range rows {
		c.Total++
		classifyCheck(r.State, &c)
	}
	return c
}

func prChecksRollup(repoPath string, number int) Checks {
	var c Checks
	out, err := run(command(repoPath, "pr", "view", strconv.Itoa(number),
		"--json", "statusCheckRollup"))
	if err != nil {
		return c
	}
	var row struct {
		StatusCheckRollup []rollupNode `json:"statusCheckRollup"`
	}
	if json.Unmarshal(out, &row) != nil {
		return c
	}
	return countRollup(row.StatusCheckRollup)
}

// rollupNode is one entry of a PR's statusCheckRollup. GitHub returns two
// shapes through it: check runs, which report status plus conclusion, and the
// older status contexts, which report a single state.
type rollupNode struct {
	State      string `json:"state"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

func countRollup(nodes []rollupNode) Checks {
	var c Checks
	for _, r := range nodes {
		c.Total++
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
//
// Answered from the repo's open PRs where it can be, for the reason given on
// PRChecksFor: the batch already carries it.
func PRReviewFor(repoPath string, number int) Review {
	open, asked := RepoPRs(repoPath)
	if d, ok := open[number]; ok {
		return d.Review
	}
	return memo("review:"+repoPath+":"+strconv.Itoa(number), ttlWhenAbsent(asked), func() Review {
		return prReviewUncached(repoPath, number)
	})
}

func prReviewUncached(repoPath string, number int) Review {
	if !Available() {
		return Review{}
	}
	out, err := run(command(repoPath, "pr", "view", strconv.Itoa(number),
		"--json", "reviewDecision,reviews"))
	if err != nil {
		return Review{}
	}
	var row struct {
		ReviewDecision string       `json:"reviewDecision"`
		Reviews        []reviewNode `json:"reviews"`
	}
	if json.Unmarshal(out, &row) != nil {
		return Review{}
	}
	return tallyReviews(row.ReviewDecision, row.Reviews)
}

// reviewNode is one submitted review as gh reports it.
type reviewNode struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// tallyReviews keeps the latest submitted review per author, then counts.
func tallyReviews(decision string, reviews []reviewNode) Review {
	rv := Review{Decision: decision}
	latest := map[string]string{}
	latestAt := map[string]time.Time{}
	for _, r := range reviews {
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
	out, err := run(command(repoPath, "run", "list", "--branch", branch,
		"--json", "workflowName,status,conclusion,createdAt",
		"--limit", "10"))
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
