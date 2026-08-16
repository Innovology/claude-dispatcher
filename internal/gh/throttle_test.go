package gh

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// The refusal itself is the signal, and nothing was reading it. Every collector
// degrades to "no signal" on error, so a locked-out client looked exactly like
// a quiet portfolio and kept asking at full rate — holding the quota at zero
// and taking the human's own `gh` down with it for the rest of the hour.
func TestAQuotaRefusalStopsEveryRead(t *testing.T) {
	clearThrottle()
	t.Cleanup(clearThrottle)
	calls := fakeGH(t, `case "$*" in
  *"api rate_limit"*) echo '{"resources":{"graphql":{"remaining":0,"reset":`+
		strconv.FormatInt(time.Now().Add(20*time.Minute).Unix(), 10)+`}}}' ;;
  *) echo "GraphQL: API rate limit exceeded for user ID 1." >&2; exit 1 ;;
esac`)

	repo := t.TempDir()
	Issues(repo) // the read that gets refused

	until, throttled := Throttled()
	if !throttled {
		t.Fatal("a quota refusal left the package still willing to ask")
	}
	if d := time.Until(until); d < 15*time.Minute || d > 25*time.Minute {
		t.Errorf("parked until %v (%s away) — wanted the reset gh reported", until, d)
	}

	// Everything after it is answered without spawning anything.
	before := len(calls())
	for i := 0; i < 20; i++ {
		Issues(t.TempDir())
		PRChecksFor(t.TempDir(), i)
		RunsForBranch(t.TempDir(), "feature/x")
	}
	if after := len(calls()); after != before {
		t.Errorf("kept asking while locked out: %d more requests", after-before)
	}
}

// A refusal that GitHub will not date — a secondary rate limit, which
// /rate_limit does not report — still stands us down, just blindly.
func TestASecondaryLimitParksBlind(t *testing.T) {
	clearThrottle()
	t.Cleanup(clearThrottle)
	fakeGH(t, `case "$*" in
  *"api rate_limit"*) echo '{"resources":{"graphql":{"remaining":4000,"reset":1}}}' ;;
  *) echo "You have exceeded a secondary rate limit" >&2; exit 1 ;;
esac`)

	Issues(t.TempDir())

	until, throttled := Throttled()
	if !throttled {
		t.Fatal("a secondary rate limit left the package still willing to ask")
	}
	if d := time.Until(until); d < blindPark-time.Minute || d > blindPark {
		t.Errorf("parked %s, want about %s", d, blindPark)
	}
}

// An ordinary failure — no remote, not a repo, gh missing a flag — is not a
// quota refusal and must not stand the whole package down.
func TestAnOrdinaryFailureDoesNotPark(t *testing.T) {
	clearThrottle()
	t.Cleanup(clearThrottle)
	fakeGH(t, `echo "no git remotes found" >&2; exit 1`)

	Issues(t.TempDir())

	if _, throttled := Throttled(); throttled {
		t.Error("one repo without a remote stood down every read in the package")
	}
}

// Nothing gathered while locked out may outlive the lockout: the quota comes
// back, and a cached "no signal" would go on being served for a whole TTL with
// no request in flight to correct it.
func TestNothingCollectedWhileParkedIsCached(t *testing.T) {
	clearThrottle()
	t.Cleanup(clearThrottle)
	fakeGH(t, `case "$*" in
  *"api rate_limit"*) echo '{"resources":{}}' ;;
  *) echo "API rate limit exceeded" >&2; exit 1 ;;
esac`)

	repo := t.TempDir()
	Issues(repo)

	mu.Lock()
	n := len(cache)
	mu.Unlock()
	if n != 0 {
		var keys []string
		mu.Lock()
		for k := range cache {
			keys = append(keys, k)
		}
		mu.Unlock()
		t.Errorf("cached %d locked-out answers: %s", n, strings.Join(keys, ", "))
	}
}
