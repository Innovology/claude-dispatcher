package cockpit

// collect_stale_queue.go fills three product-lens fields: s.staleRepos (repos
// with no active dispatch whose last commit is old), s.working (dispatches
// currently in flight) and s.queueItems (drafted dispatches read from the
// state dir's queue.json). Every git/exec/parse is guarded; missing inputs
// degrade to honest empty states.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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

	// staleRepos: repos with no active dispatch and a last commit older than
	// the threshold, most-stale first.
	stale := []staleRepo{}
	for _, r := range ctx.repos {
		if activePaths[r.Path] || activeNames[r.Name] {
			continue
		}
		days, ok := stqDaysSinceCommit(r.Path)
		if !ok || days <= stqStaleDays {
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

// stqDaysSinceCommit returns whole days since the repo's last commit. The bool
// is false when git is unavailable or the output cannot be parsed.
func stqDaysSinceCommit(repoPath string) (int, bool) {
	out, err := exec.Command("git", "-C", repoPath, "log", "-1", "--format=%ct").Output()
	if err != nil {
		return 0, false
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || secs <= 0 {
		return 0, false
	}
	d := time.Since(time.Unix(secs, 0))
	if d < 0 {
		d = 0
	}
	return int(d / (24 * time.Hour)), true
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
			edge:    cFillGreen,
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
