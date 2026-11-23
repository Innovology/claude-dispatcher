package fleetcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ghRead runs a read-only gh command in a repo's checkout, a seam for tests:
// what it printed, or on a failure what it said was wrong.
var ghRead = func(dir string, args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	b, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		b = ee.Stderr
	}
	return strings.TrimSpace(string(b)), err
}

// readyToMerge is what merge holds the pull request itself to, once a ruling
// allows it: not a draft, still on the head the steward read, and green — every
// check finished and passed, in one complete run of each workflow. The brief
// asked the steward for all of that and the verb checked none of it, so in a
// repo with no required checks a red pull request merged under a standing
// ruling. A merge into main needs every check green, and nobody is there to
// look when the steward is the one merging.
//
// Any read that fails refuses the merge: what could not be checked is not
// green.
func readyToMerge(dir string, pr int, head string) error {
	raw, err := ghRead(dir, "pr", "view", strconv.Itoa(pr), "--json", "isDraft,headRefOid,statusCheckRollup")
	if err != nil {
		return fmt.Errorf("could not read #%d to check it is green: %s", pr, firstLine(raw, err))
	}
	var v struct {
		IsDraft           bool   `json:"isDraft"`
		HeadRefOid        string `json:"headRefOid"`
		StatusCheckRollup []struct {
			Typename   string `json:"__typename"`
			Name       string `json:"name"`
			Context    string `json:"context"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			State      string `json:"state"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return fmt.Errorf("could not read #%d to check it is green: %v", pr, err)
	}
	if v.IsDraft {
		return fmt.Errorf("#%d is a draft", pr)
	}
	if v.HeadRefOid != head {
		return fmt.Errorf("#%d's head is %.9s now, not the %.9s whose checks you read: read them again", pr, v.HeadRefOid, head)
	}
	if len(v.StatusCheckRollup) == 0 {
		return fmt.Errorf("no checks ran on #%d, so nothing says it is green: it is the human's to merge", pr)
	}
	for _, c := range v.StatusCheckRollup {
		if c.Typename == "StatusContext" {
			if c.State != "SUCCESS" {
				return fmt.Errorf("%s on #%d is %s: every check has to pass", c.Context, pr, strings.ToLower(c.State))
			}
			continue
		}
		if c.Status != "COMPLETED" {
			return fmt.Errorf("%s on #%d has not finished (%s): every check has to pass", c.Name, pr, strings.ToLower(c.Status))
		}
		switch c.Conclusion {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
		default:
			return fmt.Errorf("%s on #%d is %s: every check has to pass", c.Name, pr, strings.ToLower(c.Conclusion))
		}
	}
	return wholeRuns(dir, pr, head)
}

// wholeRuns refuses a head whose checks are green only across attempts. The
// rollup keeps each check's latest result, so re-running only a workflow's
// failed jobs turns it all green without any one run having passed every job.
// A merge into main needs one complete run that did. GitHub copies the jobs it
// did not re-run into the new attempt under that attempt's number. What gives
// them away is their times: they keep the start of the attempt that ran them,
// which is before this attempt began. Measured on run 35229216768 of this repo:
// attempt 2 started 13:50:18, and its lint and build-windows started at
// 13:46:36.
func wholeRuns(dir string, pr int, head string) error {
	raw, err := ghRead(dir, "run", "list", "--commit", head, "--limit", "100", "--json", "databaseId,attempt,workflowName")
	if err != nil {
		return fmt.Errorf("could not read #%d's workflow runs: %s", pr, firstLine(raw, err))
	}
	var runs []struct {
		ID       int64  `json:"databaseId"`
		Attempt  int    `json:"attempt"`
		Workflow string `json:"workflowName"`
	}
	if err := json.Unmarshal([]byte(raw), &runs); err != nil {
		return fmt.Errorf("could not read #%d's workflow runs: %v", pr, err)
	}
	seen := map[string]bool{}
	for _, r := range runs { // newest first: the run each check's result came from
		if seen[r.Workflow] {
			continue
		}
		seen[r.Workflow] = true
		if r.Attempt <= 1 {
			continue
		}
		raw, err := ghRead(dir, "api", fmt.Sprintf("repos/{owner}/{repo}/actions/runs/%d/attempts/%d", r.ID, r.Attempt), "--jq", ".run_started_at")
		if err != nil {
			return fmt.Errorf("could not read run %d of #%d: %s", r.ID, pr, firstLine(raw, err))
		}
		began, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("could not read run %d of #%d: %v", r.ID, pr, err)
		}
		raw, err = ghRead(dir, "api", fmt.Sprintf("repos/{owner}/{repo}/actions/runs/%d/attempts/%d/jobs?per_page=100", r.ID, r.Attempt))
		if err != nil {
			return fmt.Errorf("could not read the jobs of run %d of #%d: %s", r.ID, pr, firstLine(raw, err))
		}
		var jobs struct {
			Jobs []struct {
				Name      string    `json:"name"`
				StartedAt time.Time `json:"started_at"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal([]byte(raw), &jobs); err != nil {
			return fmt.Errorf("could not read the jobs of run %d of #%d: %v", r.ID, pr, err)
		}
		for _, j := range jobs.Jobs {
			if j.StartedAt.Before(began) {
				return fmt.Errorf("%s on #%d is green across attempts, not in one: attempt %d of run %d re-ran only some jobs and kept %s from before. Re-run the whole workflow (gh run rerun %d, not --failed) so one run passes every job", r.Workflow, pr, r.Attempt, r.ID, j.Name, r.ID)
			}
		}
	}
	return nil
}
