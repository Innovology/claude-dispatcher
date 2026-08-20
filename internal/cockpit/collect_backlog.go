package cockpit

// collect_backlog.go fills s.backlogTickets — the BACKLOG lens's data — by
// merging three best-effort sources: GitHub Issues (per repo, assigned to @me),
// Linear assigned issues, and Azure Boards work items. Any source that is
// unreachable/unconfigured is simply skipped; the result is always a non-nil
// slice so the lens shows an honest state (empty when genuinely nothing).

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"claude-dispatcher/internal/azure"
	"claude-dispatcher/internal/config"
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
				prompt:  blkPrompt("GitHub issue", id, is.Title, is.Body),
				taken:   blkTakenBy(ctx.records, id, is.Title),
			})
		}
	}

	// Linear assigned issues, one read per configured token (see linearReads),
	// each ticket carrying the product whose token it came back on. The reads go
	// out together — five products would otherwise wait out five round-trips in
	// a row on every refresh — but are merged back in the order linearReads
	// named them, so what a load produces never depends on which workspace
	// answered first. Tickets are deduped because two tokens can still overlap
	// on a team: the picked set is keyed by ticket id, so one issue on two rows
	// would tick both with one space.
	//
	// Deduped on the issue's id and NOT on its identifier: "ENG-124" is unique
	// inside a workspace, and this is a list of several. Two workspaces that
	// both key a team ENG really can both raise an ENG-124, and dropping the
	// second as a duplicate would lose a ticket that was never seen — the one
	// failure a backlog must not have.
	reads := linearReads(ctx.cfg)
	readIssues := make([][]linear.Issue, len(reads))
	forEach(reads, func(i int, r linearRead) {
		if issues, err := linear.Assigned(r.key); err == nil {
			readIssues[i] = issues
		}
	})
	linSeen := map[string]bool{}
	for i, r := range reads {
		for _, is := range readIssues[i] {
			if linSeen[is.ID] {
				continue
			}
			linSeen[is.ID] = true
			tickets = append(tickets, ticket{
				id:      is.Identifier,
				src:     "lin",
				title:   is.Title,
				product: r.product,
				pri:     blkPriFromLinear(is.Priority),
				age:     blkAge(is.UpdatedAt),
				labels:  is.State,
				body:    is.Description,
				prompt:  blkPrompt("Linear issue", is.Identifier, is.Title, is.Description),
				taken:   blkTakenBy(ctx.records, is.Identifier, is.Title),
			})
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
					prompt: blkPrompt("Azure Boards work item", id, w.Title, ""),
					taken:  blkTakenBy(ctx.records, id, w.Title),
				})
			}
		}
	}

	s.backlogTickets = tickets
}

// linearRead is one call to Linear: the product whose backlog it fills, and the
// token it reads with.
type linearRead struct {
	product string
	key     string
}

// linearReads names the Linear calls a load makes. A token sees one workspace,
// and only the teams Linear granted it when the key was created, so a portfolio
// spanning several workspaces is several reads — one per product that names a
// token, or the unscoped one for a product that names none. Nothing here
// narrows a read further: two products in one workspace are separated by giving
// each a team-scoped key, which is a split the API enforces rather than one we
// apply to a list we were already handed. A config naming no token at all is
// the one unscoped read the ambient key implies, which is every install
// predating this.
//
// Products are visited in name order because map order is random: two products
// naming one token are one read, and which of them keeps it must not change
// between two loads of the same config.
func linearReads(cfg *config.Config) []linearRead {
	unscoped := linear.Key()
	var out []linearRead
	if cfg != nil {
		seen := map[string]bool{}
		for _, product := range slices.Sorted(maps.Keys(cfg.Linear)) {
			key := cfg.Linear[product]
			if key == "" {
				key = unscoped
			}
			// A product naming no token, with no unscoped key to fall back on,
			// has nothing to ask with. Skipping it says nothing about that
			// product's backlog, which is the honest answer.
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, linearRead{product: product, key: key})
		}
	}
	if len(out) > 0 {
		return out
	}
	if unscoped == "" {
		return nil
	}
	return []linearRead{{key: unscoped}}
}

// blkPrompt composes what the dispatcher is actually sent when a ticket is
// dispatched: the ticket's own words and nothing else.
//
// Every ticket used to carry an empty prompt. The lens still drew a "dispatch
// as" section over it, and enter launched a session with no instruction at all
// — the dispatcher opened on an empty prompt and waited. The body is clipped
// because a long issue thread is a conversation, not a brief; the dispatcher
// can read the rest from the ticket itself.
func blkPrompt(kind, id, title, body string) string {
	head := kind + " " + id + ": " + title
	body = strings.TrimSpace(body)
	if body == "" {
		return head
	}
	const maxBody = 1200
	if len(body) > maxBody {
		// Cut on a rune boundary; a half-written rune reaches the shell as a
		// replacement character.
		body = strings.ToValidUTF8(body[:maxBody], "") + "\n\n(…truncated — read the rest on the ticket.)"
	}
	return head + "\n\n" + body
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
