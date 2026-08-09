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

// seedCache writes the on-disk answer as if it had been fetched `age` ago.
func seedCache(t *testing.T, tag string, age time.Duration) {
	t.Helper()
	writeCache(checkCache{Latest: tag, CheckedAt: time.Now().Add(-age)})
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
