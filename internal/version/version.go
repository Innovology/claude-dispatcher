// Package version reports which build of claude-dispatcher is running and,
// best-effort, whether a newer one has been published.
//
// Version is stamped at release time by goreleaser (see .goreleaser.yml), so a
// plain `go build` reports "dev" — and a dev build is never told to upgrade,
// because there is no released version it can meaningfully be behind.
package version

import (
	"strconv"
	"strings"
)

// Version is the running build's version. Goreleaser's {{.Version}} is the tag
// with the leading "v" stripped ("2.1.1"), so both forms are accepted
// everywhere and normalised for display.
var Version = "dev"

// Display is the running build as shown to the human: "v2.1.1", or "dev".
func Display() string { return Label(Version) }

// Label normalises a version string to its display form — "2.1.1" and "v2.1.1"
// both render "v2.1.1". Anything unparseable (a dev build) is shown untouched
// rather than dressed up as a release.
func Label(v string) string {
	if _, ok := parse(v); !ok {
		return v
	}
	return "v" + strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// IsRelease reports whether this build carries a stamped version. Only a
// release build can be out of date, so only a release build ever checks.
func IsRelease() bool {
	_, ok := parse(Version)
	return ok
}

// IsOutdated reports whether latest is a newer release than the running build.
// Anything it cannot parse — a dev build, an empty or malformed latest —
// answers false: a version it does not understand must never nag the user.
func IsOutdated(latest string) bool { return newer(Version, latest) }

// semver is a version parsed far enough to order two of them.
type semver struct {
	nums [3]int
	pre  string // pre-release tag; "" for a final release
}

// parse reads "v2.1.1", "2.1.1", "2.2.0-rc1" or "2.2.0-rc1+abc123". ok=false
// for anything else, including "dev".
func parse(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	// Build metadata is not ordered; the pre-release tag is, so keep it.
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v semver
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre, s = s[i+1:], s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		v.nums[i] = n
	}
	return v, true
}

// newer reports whether latest is ahead of cur.
func newer(cur, latest string) bool {
	c, okc := parse(cur)
	l, okl := parse(latest)
	if !okc || !okl {
		return false
	}
	for i := range c.nums {
		if l.nums[i] != c.nums[i] {
			return l.nums[i] > c.nums[i]
		}
	}
	// Same x.y.z: a pre-release is behind the final release of that version
	// (2.2.0-rc1 → 2.2.0), never the other way round.
	return c.pre != "" && l.pre == ""
}
