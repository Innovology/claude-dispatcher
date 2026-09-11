package repos

import (
	"os"
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/config"
)

// The socket a repo's sessions live on defaults to the repo's own name, so that
// every repo is its own server with nobody configuring anything. A human who
// already keeps servers under names of their own says so, because a socket name
// is chosen outside the repository and no amount of reading the repo finds it.
func TestSocketDefaultsToTheRepoName(t *testing.T) {
	cfg := &config.Config{Sockets: map[string]string{
		"pp-calendar-sync": "pp_calendar_isolated",
		"legacy":           "",
	}}
	for _, tc := range []struct{ name, repo, want string }{
		{"unlisted repos are their own server", "playerpulse", "playerpulse"},
		{"a named socket wins", "pp-calendar-sync", "pp_calendar_isolated"},
		{"an empty entry means the default server", "legacy", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := socketFor(cfg, tc.repo); got != tc.want {
				t.Errorf("socketFor(%q) = %q, want %q", tc.repo, got, tc.want)
			}
		})
	}
	if got := socketFor(nil, "playerpulse"); got != "playerpulse" {
		t.Errorf("socketFor(no config) = %q, want the repo name", got)
	}
}

// A repo that has said where its toolchain comes from is taken at its word, and
// one that has not is left exactly as it was before any of this existed.
func TestEnvIsReadFromTheRepoNotConfigured(t *testing.T) {
	withNix(t, true)
	flake := t.TempDir()
	if err := os.WriteFile(filepath.Join(flake, "flake.nix"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	bare := t.TempDir()

	if got := envFor(nil, "playerpulse", flake); got != NixDevelop {
		t.Errorf("envFor(flake repo) = %q, want %q", got, NixDevelop)
	}
	if got := envFor(nil, "design-system", bare); got != "" {
		t.Errorf("envFor(no flake) = %q, want nothing", got)
	}
	if got := envFor(nil, "nowhere", ""); got != "" {
		t.Errorf("envFor(no checkout) = %q, want nothing", got)
	}

	// The flag says what a repo declares; whether we can honour it is a fact
	// about the machine. A flake repo on a host with no nix must launch the way
	// it always did rather than die on a command that is not there — the same
	// rule the mode and model flags follow.
	withNix(t, false)
	if got := envFor(nil, "playerpulse", flake); got != "" {
		t.Errorf("envFor(flake, no nix on the host) = %q, want nothing", got)
	}
}

// The override exists for the one failure the sniff cannot see coming: a flake
// with no devShell, which nix develop refuses to open. An empty entry is a
// decision, not an absent one.
func TestSessionEnvOverrideIncludingOff(t *testing.T) {
	withNix(t, true)
	flake := t.TempDir()
	if err := os.WriteFile(filepath.Join(flake, "flake.nix"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{SessionEnv: map[string]string{
		"no-devshell": "",
		"custom":      "devbox run --",
	}}
	if got := envFor(cfg, "no-devshell", flake); got != "" {
		t.Errorf("an explicit empty entry = %q, want it turned off", got)
	}
	if got := envFor(cfg, "custom", flake); got != "devbox run --" {
		t.Errorf("envFor(custom) = %q, want the configured command", got)
	}
	if got := envFor(cfg, "unlisted", flake); got != NixDevelop {
		t.Errorf("envFor(unlisted) = %q, want the read answer %q", got, NixDevelop)
	}
}

func withNix(t *testing.T, present bool) {
	t.Helper()
	prev := nixAvailable
	nixAvailable = func() bool { return present }
	t.Cleanup(func() { nixAvailable = prev })
}
