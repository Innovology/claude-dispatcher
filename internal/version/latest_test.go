package version

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// serveTag stands in for the GitHub releases endpoint and counts how often it
// is asked, which is what the cache exists to keep small.
func serveTag(t *testing.T, tag string, status int) *int32 {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag})
	}))
	t.Cleanup(srv.Close)

	orig := releasesURL
	releasesURL = srv.URL
	t.Cleanup(func() { releasesURL = orig })
	return &hits
}

// seedCache writes the on-disk answer as if this build had fetched it `age`
// ago. Stamping the running Version is what makes it a cache we are entitled to
// read back; seedCacheFrom seeds one another build left behind.
func seedCache(t *testing.T, tag string, age time.Duration) {
	t.Helper()
	seedCacheFrom(t, tag, age, Version)
}

func seedCacheFrom(t *testing.T, tag string, age time.Duration, build string) {
	t.Helper()
	writeCache(checkCache{Latest: tag, CheckedAt: time.Now().Add(-age), Build: build})
}

// stampVersion runs the package as a released build for the duration of a test.
func stampVersion(t *testing.T, v string) {
	t.Helper()
	orig := Version
	Version = v
	t.Cleanup(func() { Version = orig })
}

func TestLatestFetchesThenServesFromCache(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	hits := serveTag(t, "v2.5.0", http.StatusOK)

	if got := Latest(); got != "v2.5.0" {
		t.Fatalf("first Latest() = %q, want v2.5.0", got)
	}
	if got := Latest(); got != "v2.5.0" {
		t.Fatalf("second Latest() = %q, want v2.5.0", got)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("hit GitHub %d times, want 1 — the cache is not holding", n)
	}
}

func TestLatestRefetchesOnceTheCacheIsStale(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	hits := serveTag(t, "v3.0.0", http.StatusOK)
	seedCache(t, "v2.5.0", checkTTL+time.Minute)

	if got := Latest(); got != "v3.0.0" {
		t.Fatalf("Latest() = %q, want the refetched v3.0.0", got)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("hit GitHub %d times, want 1", n)
	}
}

func TestLatestKeepsTheLastKnownAnswerWhenTheCheckFails(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	hits := serveTag(t, "", http.StatusInternalServerError)
	seedCache(t, "v2.5.0", checkTTL+time.Minute)

	if got := Latest(); got != "v2.5.0" {
		t.Fatalf("Latest() = %q, want the last known v2.5.0", got)
	}
	// The failed check is still stamped, so a broken network is retried on the
	// TTL rather than on every poll.
	if got := Latest(); got != "v2.5.0" {
		t.Fatalf("second Latest() = %q, want v2.5.0", got)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("hit GitHub %d times, want 1 — a failure is not being backed off", n)
	}
}

func TestLatestIsEmptyWhenNothingIsKnown(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	serveTag(t, "", http.StatusNotFound)

	if got := Latest(); got != "" {
		t.Fatalf("Latest() = %q, want \"\" — an unknown answer must not nag", got)
	}
	if IsOutdated(Latest()) {
		t.Fatal("an unknown latest reported the build as outdated")
	}
}

// TestUpgradingDoesNotBlindTheNextBuild is the bug the U key introduced. The
// cockpit upgrades in place and re-execs, so the build that comes back finds a
// cached answer naming the release it was just upgraded *from* — v3.1.3 read by
// v3.2.3, which compares as "you are current" and hides everything published
// since. The answer belongs to the build that recorded it.
func TestUpgradingDoesNotBlindTheNextBuild(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	hits := serveTag(t, "v3.2.5", http.StatusOK)
	// What the pre-upgrade cockpit left behind, well inside the TTL.
	seedCacheFrom(t, "v3.1.3", time.Minute, "3.1.3")
	stampVersion(t, "3.2.3")

	if got := Latest(); got != "v3.2.5" {
		t.Fatalf("Latest() = %q, want v3.2.5 — the new build inherited the old build's answer", got)
	}
	if !IsOutdated(Latest()) {
		t.Fatal("two releases went out and the cockpit would show no upgrade")
	}
	// Having asked once, this build owns the answer and stops asking.
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("hit GitHub %d times, want 1 — the re-read is not being cached", n)
	}
}

// TestAnOlderBuildsAnswerIsNotKeptOnFailure: offline after an upgrade, the
// stale-by-a-release answer is dropped rather than carried forward. "No signal"
// shows nothing; keeping v3.1.3 would have the cockpit assert we are current.
func TestAnOlderBuildsAnswerIsNotKeptOnFailure(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	hits := serveTag(t, "", http.StatusInternalServerError)
	seedCacheFrom(t, "v3.1.3", time.Minute, "3.1.3")
	stampVersion(t, "3.2.3")

	if got := Latest(); got != "" {
		t.Fatalf("Latest() = %q, want \"\" — an answer from a build ago is not last-known-good", got)
	}
	// And the failure is still stamped by this build, so the retry waits for the
	// TTL instead of firing on every poll.
	if got := Latest(); got != "" {
		t.Fatalf("second Latest() = %q, want \"\"", got)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("hit GitHub %d times, want 1 — a failed check is not being backed off", n)
	}
}

// TestRecheckStepsOverAFreshCache: U asks us to look. A cache written a second
// ago is exactly the case where the human knows something we do not.
func TestRecheckStepsOverAFreshCache(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	hits := serveTag(t, "v3.2.5", http.StatusOK)
	stampVersion(t, "3.2.3")
	seedCache(t, "v3.2.3", time.Second)

	if got := Latest(); got != "v3.2.3" {
		t.Fatalf("Latest() = %q, want the cached v3.2.3", got)
	}
	if got := Recheck(); got != "v3.2.5" {
		t.Fatalf("Recheck() = %q, want the refetched v3.2.5", got)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("hit GitHub %d times, want 1 — only the forced check should have gone out", n)
	}
	// What it found is now the cached answer, so the poll behind it agrees.
	if got := Latest(); got != "v3.2.5" {
		t.Fatalf("Latest() after Recheck() = %q, want v3.2.5", got)
	}
}

func TestReadCacheIgnoresGarbage(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	hits := serveTag(t, "v4.0.0", http.StatusOK)
	writeCache(checkCache{Latest: "v1.0.0", CheckedAt: time.Now()})
	if err := os.WriteFile(cachePath(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := Latest(); got != "v4.0.0" {
		t.Fatalf("Latest() = %q, want v4.0.0 — a corrupt cache should be refetched", got)
	}
	if n := atomic.LoadInt32(hits); n != 1 {
		t.Fatalf("hit GitHub %d times, want 1", n)
	}
}
