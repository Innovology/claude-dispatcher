package cockpit

// collect_parallel.go holds the small concurrency helpers the collectors use to
// keep a refresh to a few seconds.
//
// The collectors are I/O bound on `gh` and `git` subprocesses, and they used to
// run every one of them in series. Fetching them concurrently — with a bounded
// fan-out so a large portfolio cannot spawn hundreds of processes at once —
// turns a multi-minute refresh into a short one without changing what any
// collector produces.

import (
	"runtime"
	"sync"

	"claude-dispatcher/internal/gh"
)

// maxCheckedPRs bounds how many of a repo's open PRs we fetch check runs for.
// The CI badge only needs to know whether anything is failing, running or
// green, and the newest PRs decide that in practice.
const maxCheckedPRs = 8

// fanOut caps concurrent subprocesses. Collectors are I/O bound, so this sits
// above core count, but low enough to stay a well-behaved API client.
func fanOut() int {
	n := runtime.NumCPU() * 2
	if n < 4 {
		return 4
	}
	if n > 12 {
		return 12
	}
	return n
}

// forEach runs fn over items with a bounded number of goroutines, returning
// once every item has been processed. fn must be safe to call concurrently.
func forEach[T any](items []T, fn func(int, T)) {
	if len(items) == 0 {
		return
	}
	sem := make(chan struct{}, fanOut())
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Add(1)
		go func(i int, it T) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fn(i, it)
		}(i, it)
	}
	wg.Wait()
}

// prChecksFor fetches check runs for up to limit of the given PRs, in parallel,
// returning them keyed by PR number.
func prChecksFor(repoPath string, prs []gh.OpenPR, limit int) map[int]gh.Checks {
	if len(prs) > limit {
		prs = prs[:limit]
	}
	out := make(map[int]gh.Checks, len(prs))
	var mu sync.Mutex
	forEach(prs, func(_ int, pr gh.OpenPR) {
		c := gh.PRChecksFor(repoPath, pr.Number)
		mu.Lock()
		out[pr.Number] = c
		mu.Unlock()
	})
	return out
}
