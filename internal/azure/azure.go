// Package azure wraps the Azure DevOps CLI (`az boards`) for backlog signals.
// It is env-gated and best-effort: without the `az` binary or the
// AZURE_DEVOPS_ORG / AZURE_DEVOPS_PROJECT env vars it stays silent, and any
// query failure degrades to no work items rather than surfacing an error to
// the cockpit loop.
package azure

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"time"
)

// Configured reports whether Azure Boards can be queried: the `az` binary is on
// PATH and both AZURE_DEVOPS_ORG and AZURE_DEVOPS_PROJECT are set.
func Configured() bool {
	if _, err := exec.LookPath("az"); err != nil {
		return false
	}
	return os.Getenv("AZURE_DEVOPS_ORG") != "" && os.Getenv("AZURE_DEVOPS_PROJECT") != ""
}

// WorkItem is a single Azure Boards work item assigned to the current user.
// Repo is always empty: Azure Boards items are not repo-scoped.
type WorkItem struct {
	ID       int
	Title    string
	State    string
	Priority string
	AreaPath string
	Repo     string
	// Created is when the item was filed and Changed when it was last edited.
	// The backlog sorts by both; without them every Azure row had a blank age
	// and sorted as if it had been filed at the epoch.
	Created time.Time
	Changed time.Time
}

const wiql = `SELECT [System.Id],[System.Title],[System.State],[Microsoft.VSTS.Common.Priority],[System.AreaPath],` +
	`[System.CreatedDate],[System.ChangedDate] ` +
	`FROM WorkItems WHERE [System.AssignedTo]=@Me AND [System.State] NOT IN ('Closed','Done','Removed') ` +
	`ORDER BY [System.ChangedDate] DESC`

// WorkItems returns the current user's open Azure Boards work items, most
// recently changed first. When not Configured() it returns (nil, nil); any
// query or parse failure degrades to (nil, err).
func WorkItems() ([]WorkItem, error) {
	if !Configured() {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "az", "boards", "query",
		"--org", os.Getenv("AZURE_DEVOPS_ORG"),
		"--project", os.Getenv("AZURE_DEVOPS_PROJECT"),
		"--wiql", wiql,
		"--output", "json",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var rows []struct {
		ID     int `json:"id"`
		Fields struct {
			Title    string    `json:"System.Title"`
			State    string    `json:"System.State"`
			Priority int       `json:"Microsoft.VSTS.Common.Priority"`
			AreaPath string    `json:"System.AreaPath"`
			Created  time.Time `json:"System.CreatedDate"`
			Changed  time.Time `json:"System.ChangedDate"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, errors.New("azure: cannot parse work item query response")
	}

	items := make([]WorkItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, WorkItem{
			ID:       r.ID,
			Title:    r.Fields.Title,
			State:    r.Fields.State,
			Priority: priorityLabel(r.Fields.Priority),
			AreaPath: r.Fields.AreaPath,
			Created:  r.Fields.Created,
			Changed:  r.Fields.Changed,
		})
	}
	return items, nil
}

// priorityLabel maps the Azure Boards priority int (1-4) to a cockpit band.
func priorityLabel(p int) string {
	switch p {
	case 1:
		return "urgent"
	case 2:
		return "high"
	case 4:
		return "low"
	default:
		return "med"
	}
}
