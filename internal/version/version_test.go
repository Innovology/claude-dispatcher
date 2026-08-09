package version

import (
	"strings"
	"testing"
)

func TestLabel(t *testing.T) {
	cases := map[string]string{
		"2.1.1":      "v2.1.1",
		"v2.1.1":     "v2.1.1",
		" 2.1.1 ":    "v2.1.1",
		"2.2.0-rc1":  "v2.2.0-rc1",
		"dev":        "dev",
		"":           "",
		"not.a.vers": "not.a.vers",
	}
	for in, want := range cases {
		if got := Label(in); got != want {
			t.Errorf("Label(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"2.1.1", "2.1.2", true},
		{"2.1.1", "2.2.0", true},
		{"2.1.1", "3.0.0", true},
		{"2.1.1", "v2.1.2", true}, // tags arrive with the v, builds without
		{"v2.1.1", "2.1.2", true},
		{"2.1.1", "2.1.1", false},
		{"2.1.2", "2.1.1", false}, // ahead of the release, e.g. a local build
		{"2.10.0", "2.9.0", false},
		{"2.9.0", "2.10.0", true},
		// A pre-release is behind the final release of the same version, and a
		// final release is never behind its own pre-release.
		{"2.2.0-rc1", "2.2.0", true},
		{"2.2.0", "2.2.0-rc1", false},
		{"2.2.0-rc1", "2.2.0-rc2", false}, // rc ordering is not modelled
		// Nothing unparseable may ever produce an upgrade nag.
		{"dev", "2.1.1", false},
		{"2.1.1", "", false},
		{"2.1.1", "nightly", false},
		{"", "", false},
		{"2.1", "2.2", false},
		{"2.1.1.1", "2.1.2.1", false},
	}
	for _, c := range cases {
		if got := newer(c.cur, c.latest); got != c.want {
			t.Errorf("newer(%q, %q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestIsReleaseAndDisplay(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()

	Version = "dev"
	if IsRelease() {
		t.Error("dev build reported as a release")
	}
	if got := Display(); got != "dev" {
		t.Errorf("Display() = %q, want %q", got, "dev")
	}
	if IsOutdated("99.0.0") {
		t.Error("dev build reported as outdated — it has no release to be behind")
	}

	Version = "2.1.1"
	if !IsRelease() {
		t.Error("stamped build not reported as a release")
	}
	if got := Display(); got != "v2.1.1" {
		t.Errorf("Display() = %q, want %q", got, "v2.1.1")
	}
	if !IsOutdated("v2.2.0") {
		t.Error("2.1.1 should be outdated against v2.2.0")
	}
	if IsOutdated("v2.1.1") {
		t.Error("2.1.1 should not be outdated against itself")
	}
}

func TestUpgradeHintNamesTheBinary(t *testing.T) {
	// Whichever package manager the platform ships through, the hint has to be
	// a runnable command naming this tool.
	if got := UpgradeHint(); !strings.Contains(got, "claude-dispatcher") {
		t.Errorf("UpgradeHint() = %q, want a command naming claude-dispatcher", got)
	}
}
