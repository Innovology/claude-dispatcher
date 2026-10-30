package gh

// search.go answers portfolio-wide questions with ONE request instead of one
// per repo.
//
// The cockpit wants "every issue assigned to me" and "every open PR in my
// repos". Asking each of 57 repos separately is 57 requests for an answer the
// search endpoint gives in one, and it was the single largest source of the
// API-quota exhaustion described in cache.go. Results are keyed by repo name so
// the collectors can look up exactly what they used to fetch per repo.

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// repoField is the repository shape gh's search output uses.
type repoField struct {
	Name          string `json:"name"`
	NameWithOwner string `json:"nameWithOwner"`
}

// shortName returns the bare repo name ("shop-api" from "acme/shop-api"),
// matching how repos.Discover names a checkout.
func (r repoField) shortName() string {
	if r.Name != "" {
		return r.Name
	}
	if i := strings.LastIndex(r.NameWithOwner, "/"); i >= 0 {
		return r.NameWithOwner[i+1:]
	}
	return r.NameWithOwner
}

// AssignedLimit is as many assigned issues as the backlog reads: the most
// GitHub's search will return for any query. gh pages it a hundred at a time,
// so it costs a request per hundred issues the human actually has — two for
// 141 — once per SearchTTL.
//
// It was 100. Measured on the account it was reported from, 141 issues were
// open and assigned and the backlog showed exactly 100: the forty-one it did
// not were in no list, no search and no count.
const AssignedLimit = 1000

// AssignedIssues returns every open issue assigned to the current user across
// all of their repos, keyed by bare repo name. One search, cached.
//
// ok reports whether the search succeeded; false means callers should leave
// their existing data alone rather than render "no tickets", which would be a
// claim rather than an observation. capped reports that the search stopped at
// AssignedLimit, so there may be more than it returned.
func AssignedIssues() (byRepo map[string][]Issue, ok, capped bool) {
	type result struct {
		byRepo     map[string][]Issue
		ok, capped bool
	}
	r := memo("search:issues:assigned", SearchTTL, func() result {
		if !Available() {
			return result{}
		}
		out, err := run(exec.Command("gh", "search", "issues",
			"--assignee", "@me", "--state", "open", "--limit", strconv.Itoa(AssignedLimit),
			"--json", "repository,number,title,url,body,state,labels,createdAt,updatedAt"))
		if err != nil {
			return result{}
		}
		var rows []struct {
			Repository repoField `json:"repository"`
			Number     int       `json:"number"`
			Title      string    `json:"title"`
			URL        string    `json:"url"`
			Body       string    `json:"body"`
			State      string    `json:"state"`
			Labels     []struct {
				Name string `json:"name"`
			} `json:"labels"`
			CreatedAt time.Time `json:"createdAt"`
			UpdatedAt time.Time `json:"updatedAt"`
		}
		if json.Unmarshal(out, &rows) != nil {
			return result{}
		}
		byRepo := map[string][]Issue{}
		for _, row := range rows {
			labels := make([]string, 0, len(row.Labels))
			for _, l := range row.Labels {
				labels = append(labels, l.Name)
			}
			name := row.Repository.shortName()
			byRepo[name] = append(byRepo[name], Issue{
				Number:    row.Number,
				Title:     row.Title,
				URL:       row.URL,
				Body:      row.Body,
				State:     row.State,
				Labels:    labels,
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			})
		}
		return result{byRepo: byRepo, ok: true, capped: len(rows) >= AssignedLimit}
	})
	return r.byRepo, r.ok, r.capped
}

// OpenPRs returns open pull requests across the user's repos, keyed by bare
// repo name. One request, cached.
func OpenPRs() (map[string][]OpenPR, bool) {
	type result struct {
		byRepo map[string][]OpenPR
		ok     bool
	}
	r := memo("search:prs:open", SearchTTL, func() result {
		if !Available() {
			return result{}
		}
		out, err := run(exec.Command("gh", "search", "prs",
			"--state", "open", "--involves", "@me", "--limit", "100",
			"--json", "repository,number,title,author,createdAt"))
		if err != nil {
			return result{}
		}
		var rows []struct {
			Repository repoField `json:"repository"`
			Number     int       `json:"number"`
			Title      string    `json:"title"`
			Author     struct {
				Login string `json:"login"`
			} `json:"author"`
			CreatedAt time.Time `json:"createdAt"`
		}
		if json.Unmarshal(out, &rows) != nil {
			return result{}
		}
		byRepo := map[string][]OpenPR{}
		for _, row := range rows {
			name := row.Repository.shortName()
			byRepo[name] = append(byRepo[name], OpenPR{
				Number:    row.Number,
				Title:     row.Title,
				Author:    row.Author.Login,
				CreatedAt: row.CreatedAt,
			})
		}
		return result{byRepo: byRepo, ok: true}
	})
	return r.byRepo, r.ok
}

// MergedPRs returns pull requests merged in the last `days`, keyed by bare repo
// name. One search, cached — the same shape as OpenPRs.
//
// The cockpit's other counts come from its own dispatch records, which only see
// work it started. A merged PR is a fact about the repository regardless of who
// or what opened it, so this is how a product the human works in directly still
// registers as moving.
func MergedPRs(days int) (map[string][]OpenPR, bool) {
	type result struct {
		byRepo map[string][]OpenPR
		ok     bool
	}
	if days <= 0 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	r := memo("search:prs:merged:"+since, SearchTTL, func() result {
		if !Available() {
			return result{}
		}
		out, err := run(exec.Command("gh", "search", "prs",
			"--merged", "--merged-at", ">="+since, "--involves", "@me",
			"--limit", "100",
			"--json", "repository,number,title,author,createdAt"))
		if err != nil {
			return result{}
		}
		var rows []struct {
			Repository repoField `json:"repository"`
			Number     int       `json:"number"`
			Title      string    `json:"title"`
			Author     struct {
				Login string `json:"login"`
			} `json:"author"`
			CreatedAt time.Time `json:"createdAt"`
		}
		if json.Unmarshal(out, &rows) != nil {
			return result{}
		}
		byRepo := map[string][]OpenPR{}
		for _, row := range rows {
			name := row.Repository.shortName()
			byRepo[name] = append(byRepo[name], OpenPR{
				Number: row.Number, Title: row.Title,
				Author: row.Author.Login, CreatedAt: row.CreatedAt,
			})
		}
		return result{byRepo: byRepo, ok: true}
	})
	return r.byRepo, r.ok
}
