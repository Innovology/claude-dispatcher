// Package ship collects shipping stats across all tracked repos: the
// credibility layer. Claude-stamped commits are detected by the
// Co-Authored-By: Claude trailer.
package ship

import (
	"os/exec"
	"strings"
	"sync"
	"time"

	"claude-dispatcher/internal/repos"
)

type Stats struct {
	Commits     int // commits today across all tracked repos (all branches)
	Stamped     int // of which carry a Co-Authored-By: Claude trailer
	ReposActive int // repos with at least one commit today
	ReposTotal  int
	CollectedAt time.Time
}

func (s Stats) StampedPct() int {
	if s.Commits == 0 {
		return 0
	}
	return s.Stamped * 100 / s.Commits
}

func Collect(rs []repos.Repo) Stats {
	stats := Stats{ReposTotal: len(rs), CollectedAt: time.Now()}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, r := range rs {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			commits, stamped := repoToday(path)
			mu.Lock()
			stats.Commits += commits
			stats.Stamped += stamped
			if commits > 0 {
				stats.ReposActive++
			}
			mu.Unlock()
		}(r.Path)
	}
	wg.Wait()
	return stats
}

func repoToday(path string) (commits, stamped int) {
	// %x1e delimits commits so multi-line bodies can be scanned per commit.
	out, err := exec.Command("git", "-C", path, "log", "--all",
		"--since=midnight", "--format=%x1e%B").Output()
	if err != nil {
		return 0, 0
	}
	for _, body := range strings.Split(string(out), "\x1e")[1:] {
		commits++
		if strings.Contains(body, "Co-Authored-By:") && strings.Contains(body, "Claude") {
			stamped++
		}
	}
	return commits, stamped
}
