package cockpit

// collect_backlog.go fills s.backlogTickets — the BACKLOG lens's data — by
// merging three best-effort sources: GitHub Issues (per repo, assigned to @me),
// Linear assigned issues, and Azure Boards work items. Any source that is
// unreachable/unconfigured is simply skipped; the result is always a non-nil
// slice so the lens shows an honest state (empty when genuinely nothing).

import (
	"strconv"
	"strings"
	"time"

	"claude-dispatcher/internal/azure"
	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/linear"
	"claude-dispatcher/internal/state"
)

// collectBacklog merges GitHub, Linear and Azure backlog items into
// s.backlogTickets.
func collectBacklog(ctx *collectCtx, s *snapshot) {
	tickets := []ticket{}

	// GitHub Issues for every repo at once. This used to ask each discovered
	// repo separately — 57 requests for an answer one search returns, and the
	// main reason a refresh could exhaust the hourly API quota. Issues for
	// repos the user has not checked out are ignored, keeping the backlog
	// scoped to the configured scan roots exactly as before.
	assigned, searched := gh.AssignedIssues()
	for _, r := range ctx.repos {
		if !searched {
			break
		}
		for _, is := range assigned[r.Name] {
			id := r.Name + "#" + strconv.Itoa(is.Number)
			tickets = append(tickets, ticket{
				id:      id,
				src:     "gh",
				title:   is.Title,
				repo:    r.Name,
				product: r.Product,
				pri:     blkPriFromLabels(is.Labels),
				age:     blkAge(is.UpdatedAt),
				labels:  strings.Join(is.Labels, " · "),
				body:    is.Body,
				prompt:  "",
				taken:   blkTakenBy(ctx.records, id, is.Title),
			})
		}
	}

	// Linear assigned issues (only when configured).
	if linear.Configured() {
		if issues, err := linear.Assigned(); err == nil {
			for _, is := range issues {
				tickets = append(tickets, ticket{
					id:     is.Identifier,
					src:    "lin",
					title:  is.Title,
					pri:    blkPriFromLinear(is.Priority),
					age:    blkAge(is.UpdatedAt),
					labels: is.State,
					body:   is.Description,
					taken:  blkTakenBy(ctx.records, is.Identifier, is.Title),
				})
			}
		}
	}

	// Azure Boards work items (only when configured).
	if azure.Configured() {
		if items, err := azure.WorkItems(); err == nil {
			for _, w := range items {
				id := "AB#" + strconv.Itoa(w.ID)
				tickets = append(tickets, ticket{
					id:     id,
					src:    "ado",
					title:  w.Title,
					pri:    blkNormalizePri(w.Priority),
					labels: w.State,
					body:   "",
					taken:  blkTakenBy(ctx.records, id, w.Title),
				})
			}
		}
	}

	s.backlogTickets = tickets
}

// blkPriFromLabels maps a GitHub issue's labels onto a cockpit priority band.
// A label naming urgency/incident wins; a customer-facing bug is high; else med.
func blkPriFromLabels(labels []string) string {
	high := false
	for _, l := range labels {
		ll := strings.ToLower(strings.TrimSpace(l))
		switch {
		case strings.Contains(ll, "urgent"), strings.Contains(ll, "incident"),
			strings.Contains(ll, "critical"), ll == "p0":
			return "urgent"
		case strings.Contains(ll, "customer"), strings.Contains(ll, "bug"),
			ll == "p1", ll == "high":
			high = true
		}
	}
	if high {
		return "high"
	}
	return "med"
}

// blkPriFromLinear maps a Linear priority label onto a cockpit band.
func blkPriFromLinear(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "urgent":
		return "urgent"
	case "high":
		return "high"
	case "medium":
		return "med"
	default:
		return "low"
	}
}

// blkNormalizePri accepts an already-banded priority (Azure emits urgent/high/
// med/low) and returns a safe cockpit band, defaulting to med.
func blkNormalizePri(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "urgent":
		return "urgent"
	case "high":
		return "high"
	case "low":
		return "low"
	case "med", "medium":
		return "med"
	default:
		return "med"
	}
}

// blkTakenBy reports the feature of an active dispatch that appears to be
// working this ticket, or "" when none is. A dispatch matches when its branch
// is the branch this ticket would get (feature/<slug of id>), or when its
// feature slug equals the ticket's id/title slug.
func blkTakenBy(records []*state.Dispatch, id, title string) string {
	wantBranch := "feature/" + backlogSlug(id)
	idSlug := backlogSlug(id)
	titleSlug := backlogSlug(title)
	for _, d := range records {
		if !blkActive(d.Status) {
			continue
		}
		if d.Branch != "" && d.Branch == wantBranch {
			return d.Feature
		}
		fs := backlogSlug(d.Feature)
		if fs != "" && (fs == idSlug || fs == titleSlug) {
			return d.Feature
		}
	}
	return ""
}

// blkActive reports whether a dispatch status counts as still in flight.
func blkActive(st state.Status) bool {
	switch st {
	case state.StatusWorking, state.StatusLaunching,
		state.StatusNeedsInput, state.StatusBlocked:
		return true
	}
	return false
}

// blkAge renders a timestamp as a short relative age like "4m", "2h", "3d".
// A zero time renders as "".
func blkAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	default:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	}
}
