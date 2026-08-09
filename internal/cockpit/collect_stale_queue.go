package cockpit

// collect_stale_queue.go fills four product-lens fields: s.repoInventory (every
// discovered repo, product or not), s.staleRepos (repos with no active dispatch
// whose last commit is old), s.working (dispatches currently in flight) and
// s.queueItems (drafted dispatches read from the state dir's queue.json). Every
// git/exec/parse is guarded; missing inputs degrade to honest empty states.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// stqStaleDays is the threshold beyond which a repo with no active dispatch is
// surfaced as stale.
const stqStaleDays = 14

// collectStaleQueue fills the stale-repo list, the working list and the draft
// queue.
func collectStaleQueue(ctx *collectCtx, s *snapshot) {
	// Which repos have an active dispatch on them (by path or name).
	activePaths := map[string]bool{}
	activeNames := map[string]bool{}
	for _, d := range ctx.records {
		if !stqActive(d.Status) {
			continue
		}
		if d.RepoPath != "" {
			activePaths[d.RepoPath] = true
		}
		if d.RepoName != "" {
			activeNames[d.RepoName] = true
		}
	}

	// How many dispatchers are still out on each repo — the "OUT" column of the
	// inventory. Counted over every record, not just the active ones, so a repo
	// whose only dispatcher is blocked still reads as busy.
	openByRepo := map[string]int{}
	for _, d := range ctx.records {
		if d.Status != state.StatusDone && d.Status != state.StatusExited {
			openByRepo[d.RepoName]++
		}
	}

	// repoInventory: one row per discovered repo. The two git calls per repo
	// (origin remote, last commit) run with the shared bounded fan-out — serially
	// they were the slowest thing in this collector on a large portfolio.
	inv := make([]repoRow, len(ctx.repos))
	forEach(ctx.repos, func(i int, r repos.Repo) {
		row := repoRow{
			name:    r.Name,
			product: r.Product,
			forge:   ctx.forge(r.Path),
			out:     openByRepo[r.Name],
			days:    -1,
		}
		if t, ok := stqLastCommit(r.Path); ok {
			row.last = stqAge(t)
			row.days = stqWholeDays(t)
		}
		inv[i] = row
	})
	s.repoInventory = inv

	// staleRepos: repos with no active dispatch and a last commit older than
	// the threshold, most-stale first. inv is index-aligned with ctx.repos, so
	// the commit age is already paid for.
	stale := []staleRepo{}
	for i, r := range ctx.repos {
		if activePaths[r.Path] || activeNames[r.Name] {
			continue
		}
		days := inv[i].days
		if days <= stqStaleDays { // -1 (git said nothing) falls out here too
			continue
		}
		stale = append(stale, staleRepo{
			repo:    r.Name,
			product: r.Product,
			days:    days,
			note:    strconv.Itoa(days) + "d untouched",
		})
	}
	sort.SliceStable(stale, func(i, j int) bool { return stale[i].days > stale[j].days })
	s.staleRepos = stale

	// working: dispatches presently launching or working.
	work := []workingItem{}
	for _, d := range ctx.records {
		if d.Status != state.StatusWorking && d.Status != state.StatusLaunching {
			continue
		}
		work = append(work, workingItem{
			feature: d.Feature,
			repo:    d.RepoName,
			product: d.Product,
			age:     stqAge(stqStartOf(d)),
		})
	}
	s.working = work

	// queueItems: drafts from <state dir>/queue.json, if present.
	s.queueItems = stqLoadQueue()
}

// stqActive reports whether a dispatch status counts as still in flight, for
// the purpose of excluding a repo from the stale list.
func stqActive(st state.Status) bool {
	switch st {
	case state.StatusWorking, state.StatusLaunching,
		state.StatusNeedsInput, state.StatusBlocked:
		return true
	}
	return false
}

// stqLastCommit returns the time of the repo's last commit. The bool is false
// when git is unavailable or the output cannot be parsed.
func stqLastCommit(repoPath string) (time.Time, bool) {
	out, err := exec.Command("git", "-C", repoPath, "log", "-1", "--format=%ct").Output()
	if err != nil {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}, false
	}
	return time.Unix(secs, 0), true
}

// stqWholeDays is whole days between t and now, floored at 0.
func stqWholeDays(t time.Time) int {
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	return int(d / (24 * time.Hour))
}

// stqDaysSinceCommit returns whole days since the repo's last commit. The bool
// is false when git is unavailable or the output cannot be parsed.
func stqDaysSinceCommit(repoPath string) (int, bool) {
	t, ok := stqLastCommit(repoPath)
	if !ok {
		return 0, false
	}
	return stqWholeDays(t), true
}

// stqStartOf picks the best "since" timestamp for a working item: its creation
// time, falling back to the last update.
func stqStartOf(d *state.Dispatch) time.Time {
	if !d.CreatedAt.IsZero() {
		return d.CreatedAt
	}
	return d.UpdatedAt
}

// stqQueueDraft is one row of queue.json.
type stqQueueDraft struct {
	Feature string `json:"feature"`
	Repo    string `json:"repo"`
	Prompt  string `json:"prompt"`
}

// stqLoadQueue reads the drafted batch from <state dir>/queue.json. A missing
// or unreadable/unparsable file yields an empty (non-nil) slice.
func stqLoadQueue() []queueItem {
	items := []queueItem{}
	path := filepath.Join(state.Dir(), "queue.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return items
	}
	var drafts []stqQueueDraft
	if json.Unmarshal(raw, &drafts) != nil {
		return items
	}
	for _, d := range drafts {
		items = append(items, queueItem{
			feature: d.Feature,
			repo:    d.Repo,
			prompt:  d.Prompt,
			status:  "ready",
			color:   cGreen,
			edge:    "#2f6b41",
		})
	}
	return items
}

// stqAge renders a timestamp as a short relative age like "4m", "2h", "3d".
func stqAge(t time.Time) string {
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
