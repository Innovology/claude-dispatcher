package gh

// cache.go bounds how often the cockpit talks to GitHub.
//
// The cockpit rebuilds its whole snapshot on every dispatch-record change and
// again on a timer. Without a cache that meant one `gh` invocation per repo per
// rebuild — with 57 repos and a 15s poll, roughly 13,000 API calls an hour,
// which exhausts the 5,000/hour GraphQL quota within minutes. Once the quota is
// gone every collector fails, so the cockpit shows nothing and the user's own
// `gh` stops working too.
//
// Every expensive read goes through memo, which collapses concurrent callers
// onto one in-flight request and serves a result for ttl afterwards.

import (
	"sync"
	"time"
)

// TTLs are per query class: issue/PR lists move slowly, check runs move fast.
//
// Every one of them must be at least the cockpit's poll interval. A TTL shorter
// than the poll does not buy the human fresher data — the poll is what asks —
// it only lets the rebuilds *between* polls pay for the same answer again, and
// the cockpit rebuilds on every dispatch-record write, which with a handful of
// live sessions is far more often than once a minute. PRTTL was 45s against a
// 60s poll, so the busiest class in the cache was guaranteed to be cold on
// arrival at every poll and to refetch a second time in between. See
// cockpit.TestForgeTTLsOutlastThePoll, which holds this the right way round.
const (
	// SearchTTL covers the whole-account searches (assigned issues, open PRs).
	SearchTTL = 3 * time.Minute
	// RepoTTL covers per-repo lists.
	RepoTTL = 2 * time.Minute
	// PRTTL covers per-PR checks and reviews, which change while CI runs, so
	// it sits exactly on the poll: fresh once per poll, free in between.
	PRTTL = 60 * time.Second
)

type entry struct {
	val   any
	at    time.Time
	ready chan struct{} // closed once the first fetch completes
}

var (
	mu    sync.Mutex
	cache = map[string]*entry{}
)

// memo returns the cached value for key, calling fetch only when there is no
// value newer than ttl. Concurrent callers for the same key wait on the single
// in-flight fetch rather than each starting their own.
func memo[T any](key string, ttl time.Duration, fetch func() T) T {
	mu.Lock()
	if e, ok := cache[key]; ok {
		if e.ready != nil {
			// A fetch is in flight; wait for it rather than duplicating it.
			ready := e.ready
			mu.Unlock()
			<-ready
			mu.Lock()
			if v, ok := cache[key]; ok && v.ready == nil {
				val := v.val
				mu.Unlock()
				return val.(T)
			}
			mu.Unlock()
			return fetch()
		}
		if time.Since(e.at) < ttl {
			val := e.val
			mu.Unlock()
			return val.(T)
		}
	}
	pending := &entry{ready: make(chan struct{})}
	cache[key] = pending
	mu.Unlock()

	val := fetch()

	mu.Lock()
	cache[key] = &entry{val: val, at: time.Now()}
	mu.Unlock()
	close(pending.ready)
	return val
}

// InvalidateCache drops every cached response. Call it after an action that
// changes state on the forge (a merge, a push) so the next read is fresh.
func InvalidateCache() {
	mu.Lock()
	cache = map[string]*entry{}
	mu.Unlock()
}
