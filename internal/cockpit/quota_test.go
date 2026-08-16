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

// The lockout line is ambient: it fills a silent footer, never talks over what
// the human just did, and clears itself when the quota comes back. A cockpit
// still reporting a spent quota after the window reset is asserting something
// it stopped knowing — the same failure as the empty check column it exists to
// explain.
func TestTheQuotaLineYieldsAndRetires(t *testing.T) {
	for _, tc := range []struct {
		name      string
		notice    string
		throttled bool
		want      string
	}{
		{"fills a silent footer", "", true, quotaNotice + "12m"},
		{"never talks over the human", "merging login-fix…", true, "merging login-fix…"},
		{"refreshes its own countdown", quotaNotice + "30m", true, quotaNotice + "12m"},
		{"retires itself on recovery", quotaNotice + "12m", false, ""},
		{"leaves other messages alone", "attach failed: no session", false, "attach failed: no session"},
		{"says nothing when there is nothing to say", "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := quotaLine(tc.notice, tc.throttled, "12m"); got != tc.want {
				t.Errorf("quotaLine(%q, %v) = %q, want %q", tc.notice, tc.throttled, got, tc.want)
			}
		})
	}
}
