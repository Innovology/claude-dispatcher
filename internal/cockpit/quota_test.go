package cockpit

// quota_test.go holds the one invariant that keeps the cockpit inside GitHub's
// hourly API quota, and it lives here because this is the only package that can
// see both halves of it: gh owns the cache TTLs, cockpit owns the poll.

import (
	"testing"
	"time"

	"claude-dispatcher/internal/gh"
)

// A forge TTL shorter than the poll interval is not a freshness setting, it is
// a leak.
//
// The poll is what asks the forge anything, so a TTL below it cannot make the
// human's screen any newer — it can only let the rebuilds *between* polls pay
// for the same answer a second time. And rebuilds between polls are the normal
// case, not the exception: the cockpit reloads on every dispatch-record write,
// which with a few live sessions is many times a minute.
//
// PRTTL was 45s against a 60s poll. Every entry in the biggest key class the
// cache has — per-PR checks and reviews — was therefore guaranteed to be cold
// when the poll arrived, and to expire again before the next one, so the cache
// bought nothing on the path it existed for.
func TestForgeTTLsOutlastThePoll(t *testing.T) {
	for _, c := range []struct {
		name string
		ttl  time.Duration
	}{
		{"PRTTL", gh.PRTTL},
		{"RepoTTL", gh.RepoTTL},
		{"SearchTTL", gh.SearchTTL},
	} {
		if c.ttl < refreshEvery {
			t.Errorf("gh.%s is %s, shorter than the %s poll: every refresh finds it "+
				"cold and every rebuild in between refetches it",
				c.name, c.ttl, refreshEvery)
		}
	}
}
