// Package linear is an env-gated, best-effort Linear API client for the
// cockpit backlog. It is active only when LINEAR_API_KEY is set; every call
// degrades cleanly on a transport, JSON, or GraphQL error, returning an error
// the collector treats as "no signal" rather than surfacing it to the UI loop.
package linear

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"
)

const endpoint = "https://api.linear.app/graphql"

// Configured reports whether a Linear API key is present in the environment.
func Configured() bool {
	return os.Getenv("LINEAR_API_KEY") != ""
}

// Issue is a single Linear issue assigned to the current viewer.
type Issue struct {
	ID          string
	Identifier  string // e.g. "ENG-124"
	Title       string
	Description string
	Priority    string // "Urgent"/"High"/"Medium"/"Low"/"No priority"
	State       string
	Team        string
	UpdatedAt   time.Time
}

const query = `query { viewer { assignedIssues(first: 50, filter: { state: { type: { nin: ["completed","canceled"] } } }) {
    nodes { id identifier title description priority priorityLabel updatedAt
            state { name } team { key } } } } }`

// Assigned returns the open issues assigned to the current Linear viewer.
// It returns (nil, nil) when not Configured(), and (nil, err) on any
// transport, JSON, or GraphQL error so the caller can degrade cleanly.
func Assigned() ([]Issue, error) {
	if !Configured() {
		return nil, nil
	}

	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// Linear uses the raw key as the Authorization header (no "Bearer").
	req.Header.Set("Authorization", os.Getenv("LINEAR_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var payload struct {
		Data struct {
			Viewer struct {
				AssignedIssues struct {
					Nodes []struct {
						ID            string    `json:"id"`
						Identifier    string    `json:"identifier"`
						Title         string    `json:"title"`
						Description   string    `json:"description"`
						PriorityLabel string    `json:"priorityLabel"`
						UpdatedAt     time.Time `json:"updatedAt"`
						State         struct {
							Name string `json:"name"`
						} `json:"state"`
						Team struct {
							Key string `json:"key"`
						} `json:"team"`
					} `json:"nodes"`
				} `json:"assignedIssues"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.Errors) > 0 {
		return nil, errors.New(payload.Errors[0].Message)
	}

	nodes := payload.Data.Viewer.AssignedIssues.Nodes
	issues := make([]Issue, 0, len(nodes))
	for _, n := range nodes {
		issues = append(issues, Issue{
			ID:          n.ID,
			Identifier:  n.Identifier,
			Title:       n.Title,
			Description: n.Description,
			Priority:    n.PriorityLabel,
			State:       n.State.Name,
			Team:        n.Team.Key,
			UpdatedAt:   n.UpdatedAt,
		})
	}
	return issues, nil
}
