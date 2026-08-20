// Package linear is a best-effort Linear API client for the cockpit backlog.
// A read is scoped by its key: one token sees one workspace, and only the teams
// Linear granted it. Every call degrades cleanly on a transport, JSON, or
// GraphQL error, returning an error the collector treats as "no signal" rather
// than surfacing it to the UI loop.
package linear

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"
)

// endpoint is a var rather than a const so a test can point a read at an
// httptest server. Everything this package does that is worth testing happens
// on the far side of one HTTP call — the decode, the GraphQL error, the
// field mapping — and with a compiled-in URL none of it was reachable: the
// package sat at 9.5% covered with only "an empty key reads nothing" behind it.
// Nothing writes to it outside tests.
var endpoint = "https://api.linear.app/graphql"

// Key is the unscoped API key: the ambient LINEAR_API_KEY, which the cockpit
// seeds from config at start-up. It is what a product whose source names no key
// of its own reads with, and the whole of the backlog when no product does.
func Key() string {
	return os.Getenv("LINEAR_API_KEY")
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

// newRequest builds the assigned-issues request, read with key. The key is the
// whole of the scope — which workspace, and which of its teams — because Linear
// grants that when the key is created, so nothing here narrows it further.
func newRequest(key string) (*http.Request, error) {
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// Linear uses the raw key as the Authorization header (no "Bearer").
	req.Header.Set("Authorization", key)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// Assigned returns the open issues assigned to key's viewer. It returns
// (nil, nil) for an empty key, and (nil, err) on any transport, JSON, or
// GraphQL error so the caller can degrade cleanly.
func Assigned(key string) ([]Issue, error) {
	if key == "" {
		return nil, nil
	}

	req, err := newRequest(key)
	if err != nil {
		return nil, err
	}

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
