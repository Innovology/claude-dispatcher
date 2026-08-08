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

// AssignedIssues returns every open issue assigned to the current user across
// all of their repos, keyed by bare repo name. One request, cached.
//
// The bool reports whether the search succeeded; false means callers should
// leave their existing data alone rather than render "no tickets", which would
// be a claim rather than an observation.
func AssignedIssues() (map[string][]Issue, bool) {
	type result struct {
		byRepo map[string][]Issue
		ok     bool
	}
	r := memo("search:issues:assigned", SearchTTL, func() result {
		if !Available() {
			return result{}
		}
		out, err := exec.Command("gh", "search", "issues",
			"--assignee", "@me", "--state", "open", "--limit", "100",
			"--json", "repository,number,title,url,body,state,labels,updatedAt").Output()
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
				UpdatedAt: row.UpdatedAt,
			})
		}
		return result{byRepo: byRepo, ok: true}
	})
	return r.byRepo, r.ok
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
		out, err := exec.Command("gh", "search", "prs",
			"--state", "open", "--involves", "@me", "--limit", "100",
			"--json", "repository,number,title,author,createdAt").Output()
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
