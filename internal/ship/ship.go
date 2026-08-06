// Package ship collects shipping stats across all tracked repos: the
// credibility layer. Commits are attributed to dispatchers by provenance —
// the SHAs each dispatch recorded on its feature branch — never by trailers
// or markers in the repo's public history.
package ship

import (
	"os/exec"
	"strings"
	"sync"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// Stats are pure git + dispatch-record numbers; PRsToday/PRsOK are filled in
// by the caller (the cockpit adds them from gh) so Collect stays hermetic.
type Stats struct {
	Commits      int // commits today across all tracked repos (all branches)
	Dispatched   int // of which were produced under a dispatch
	PRsToday     int // PRs the user launched today, across all of GitHub
	PRsOK        bool
	FeaturesLive int // features that went live today
	ReposActive  int // repos with at least one commit today
	ReposTotal   int
	CollectedAt  time.Time
}

func (s Stats) DispatchedPct() int {
	if s.Commits == 0 {
		return 0
	}
	return s.Dispatched * 100 / s.Commits
}

func Collect(rs []repos.Repo, ds []*state.Dispatch) Stats {
	dispatched := map[string]bool{}
	for _, d := range ds {
		for _, sha := range d.Commits {
			dispatched[sha] = true
		}
	}

	stats := Stats{ReposTotal: len(rs), CollectedAt: time.Now()}
	today := time.Now().Format("2006-01-02")
	for _, d := range ds {
		if d.DeployedAt != nil && d.DeployedAt.Local().Format("2006-01-02") == today {
			stats.FeaturesLive++
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, r := range rs {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			shas := repoToday(path)
			viaDispatch := 0
			for _, sha := range shas {
				if dispatched[sha] {
					viaDispatch++
				}
			}
			mu.Lock()
			stats.Commits += len(shas)
			stats.Dispatched += viaDispatch
			if len(shas) > 0 {
				stats.ReposActive++
			}
			mu.Unlock()
		}(r.Path)
	}
	wg.Wait()
	return stats
}

func repoToday(path string) []string {
	out, err := exec.Command("git", "-C", path, "log", "--all",
		"--since=midnight", "--format=%H").Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}
