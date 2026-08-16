package version

// latest.go answers "is there a newer release?" without turning the cockpit
// into GitHub chatter.
//
// The answer changes a few times a week at most, while the cockpit polls every
// minute, so the reply is cached on disk under the state dir and refetched only
// once the cache is older than checkTTL — or once a different build asks, an
// answer being the property of whoever recorded it (see checkCache.Build).
//
// The call is unauthenticated (the repo is public), short-timeout and
// best-effort: offline, rate-limited or firewalled all degrade to "no signal" —
// the cockpit then shows the version with no upgrade hint, never an error.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-dispatcher/internal/state"
)

const (
	checkTTL     = 6 * time.Hour
	fetchTimeout = 4 * time.Second
)

// releasesURL is a var so the tests can point it at an httptest server.
var releasesURL = "https://api.github.com/repos/Innovology/claude-dispatcher/releases/latest"

// Latest returns the newest published release tag, or "" when it is not known
// (never checked, offline, or the check failed). It reads and writes a file and
// may make a network call, so callers must run it off the UI goroutine.
func Latest() string { return check(false) }

// Recheck asks the same question as Latest but goes to GitHub even when the
// cached answer is still inside the TTL. It is what the U key runs: a human who
// presses it is asking us to look, and answering "you are on the latest" from a
// file written hours ago is a claim we have not checked.
func Recheck() string { return check(true) }

func check(force bool) string {
	cached, ok := readCache()
	if ok && !force && cached.wroteBy(Version) && time.Since(cached.CheckedAt) < checkTTL {
		return cached.Latest
	}
	tag := fetchLatest()
	if tag == "" && cached.wroteBy(Version) {
		// A failed call teaches nothing new: keep whatever this build last knew,
		// but still stamp the check so a broken network is retried on the TTL
		// rather than on every poll.
		tag = cached.Latest
	}
	writeCache(checkCache{Latest: tag, CheckedAt: time.Now(), Build: Version})
	return tag
}

// checkCache is the answer on disk, stamped with the build that recorded it.
//
// Build is what stops the cockpit's own upgrade key blinding it. `U` installs
// the release the cache pointed at and re-execs, so the new build's very first
// read finds an answer naming a release it is now ahead of — "latest v3.1.3"
// read by v3.2.3 — which compares as "you are current" and hides the two
// releases that shipped while the cockpit was open. The answer belongs to the
// build that asked the question: a different build has to ask again.
type checkCache struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
	Build     string    `json:"build"`
}

// wroteBy reports whether v is the build that recorded this answer. A cache
// written before this field existed answers false and is re-fetched once, which
// is the same thing it means: we cannot tell who wrote it.
func (c checkCache) wroteBy(v string) bool { return c.Build != "" && c.Build == v }

func cachePath() string { return filepath.Join(state.Dir(), "version-check.json") }

func readCache() (checkCache, bool) {
	b, err := os.ReadFile(cachePath())
	if err != nil {
		return checkCache{}, false
	}
	var c checkCache
	if json.Unmarshal(b, &c) != nil {
		return checkCache{}, false
	}
	return c, true
}

// writeCache is best-effort and atomic (tmp + rename), like every other file
// this tool owns: a half-written cache must never be read back as an answer.
func writeCache(c checkCache) {
	b, err := json.Marshal(c)
	if err != nil || os.MkdirAll(state.Dir(), 0o755) != nil {
		return
	}
	tmp := cachePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) != nil {
		return
	}
	if os.Rename(tmp, cachePath()) != nil {
		_ = os.Remove(tmp)
	}
}

// fetchLatest asks GitHub for the newest release tag. The /releases/latest
// endpoint already excludes drafts and pre-releases, so nothing unpublished can
// ever prompt an upgrade.
func fetchLatest() string {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub rejects requests without a User-Agent.
	req.Header.Set("User-Agent", "claude-dispatcher/"+Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel) != nil {
		return ""
	}
	return strings.TrimSpace(rel.TagName)
}
