package gh

// repoprs.go asks a repository about all of its open pull requests at once.
//
// This is the same move search.go made for issues, applied to the place that
// still fanned out: the cockpit wanted a check rollup and a review posture per
// PR, and asked for each of them separately — `pr checks` and `pr view` per
// pull request, per repository, on every refresh. Measured on a real portfolio
// that was ~103 GraphQL requests a minute for two columns of the display, and
// it was the largest single item in a bill that came to 5,409 requests an hour
// against a limit of 5,000. One idle cockpit could exhaust the whole quota by
// itself and take the human's own `gh` down with it.
//
// `gh pr list --json` carries statusCheckRollup, reviewDecision and reviews for
// every open PR in one request, so that is what this asks for. The cost now
// scales with repositories rather than with pull requests — and the answer is
// better as well as cheaper: nothing is capped at the newest few PRs any more,
// and the head branch and diff size come back filled in, which the search this
// replaced never returned.

import (
	"encoding/json"
	"time"
)

// PRDetail is an open pull request with the signals that used to cost a request
// each.
type PRDetail struct {
	OpenPR
	Checks Checks
	Review Review

	// rollup is the check rollup this read already paid for, kept so that
	// asking what one named check concluded (GateState) costs nothing. It is
	// unexported because the only thing a caller may have out of it is the
	// verdict of a check it can name: handing over every check name invites a
	// second opinion about what green means, and this package has exactly one.
	//
	// The gate's name is not a parameter of RepoPRs for the reason the whole
	// file exists. The read is memoised per repository and must stay ONE entry:
	// a name in the cache key would buy the same payload again for every caller
	// that spelled the gate differently — PRChecksFor and PRReviewFor name no
	// gate at all — which is the bill this package was written to stop.
	rollup []rollupNode
}

// GateState returns what the check called name concluded on this pull request,
// as one of "SUCCESS", "FAILURE", "PENDING" or "" — the empty string meaning
// the rollup carries no such check, which is not a verdict and must never be
// read as one. An umbrella workflow routinely skips its aggregator (a targeted
// or dispatch run has no profile to aggregate), so "absent" is the common case
// rather than the error case.
//
// A name can appear twice in one rollup — a re-run leaves the earlier job's
// entry beside the new one, measured at four and five repeated names per pull
// request on a real umbrella workflow — so the rule is deterministic and
// cautious: a settled verdict beats an unsettled one, and among settled ones a
// failure beats a success. Reporting green while a run of the same gate is red
// is the one answer worth ruling out.
func (d PRDetail) GateState(name string) string {
	if name == "" {
		return ""
	}
	out := ""
	for _, n := range d.rollup {
		if n.label() != name {
			continue
		}
		switch rollupState(n) {
		case "FAILURE", "ERROR":
			// Final: nothing later in the list makes a red run of the gate
			// green again.
			return "FAILURE"
		case "SUCCESS":
			out = "SUCCESS"
		case "PENDING", "IN_PROGRESS", "QUEUED":
			if out == "" {
				out = "PENDING"
			}
		}
		// Anything else — CANCELLED, SKIPPED, NEUTRAL, ACTION_REQUIRED, and
		// whatever GitHub adds next — is a gate that ran and said nothing this
		// cockpit can translate, which is the same as not having run.
	}
	return out
}

// RepoPRs returns every open pull request in the repo, keyed by number, with
// its checks and review posture. One request, cached at PRTTL because the check
// rollup it carries is the fastest-moving thing the cockpit shows.
//
// The bool separates "this repo has no open pull requests" from "we could not
// ask", and the difference matters beyond an empty pane here: absence from a
// list that answered is how the rest of the package knows a pull request has
// merged or closed, and so that its checks and reviews have stopped changing.
// Absence from a list that failed is no evidence of anything.
func RepoPRs(repoPath string) (prs map[int]PRDetail, asked bool) {
	type result struct {
		byNumber map[int]PRDetail
		ok       bool
	}
	r := memo("repoprs:"+repoPath, PRTTL, func() result {
		byNumber, ok := repoPRsUncached(repoPath)
		return result{byNumber, ok}
	})
	return r.byNumber, r.ok
}

func repoPRsUncached(repoPath string) (map[int]PRDetail, bool) {
	if !Available() {
		return nil, false
	}
	out, err := run(command(repoPath, "pr", "list", "--state", "open", "--limit", "50",
		"--json", "number,title,author,headRefName,baseRefName,reviewDecision,"+
			"reviews,additions,deletions,createdAt,statusCheckRollup"))
	if err != nil {
		return nil, false
	}
	var rows []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		HeadRefName       string       `json:"headRefName"`
		BaseRefName       string       `json:"baseRefName"`
		ReviewDecision    string       `json:"reviewDecision"`
		Reviews           []reviewNode `json:"reviews"`
		Additions         int          `json:"additions"`
		Deletions         int          `json:"deletions"`
		CreatedAt         time.Time    `json:"createdAt"`
		StatusCheckRollup []rollupNode `json:"statusCheckRollup"`
	}
	if json.Unmarshal(out, &rows) != nil {
		return nil, false
	}
	byNumber := make(map[int]PRDetail, len(rows))
	for _, r := range rows {
		byNumber[r.Number] = PRDetail{
			OpenPR: OpenPR{
				Number:         r.Number,
				Title:          r.Title,
				Author:         r.Author.Login,
				HeadRefName:    r.HeadRefName,
				BaseRefName:    r.BaseRefName,
				ReviewDecision: r.ReviewDecision,
				Additions:      r.Additions,
				Deletions:      r.Deletions,
				CreatedAt:      r.CreatedAt,
			},
			Checks: countRollup(r.StatusCheckRollup),
			Review: tallyReviews(r.ReviewDecision, r.Reviews),
			rollup: r.StatusCheckRollup,
		}
	}
	return byNumber, true
}
