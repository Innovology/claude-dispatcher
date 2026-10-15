package repos

import (
	"os"
	"path/filepath"
	"strings"
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

// The flake that decides a repo's environment is the checkout's, and a session
// runs in a worktree cut from somewhere else. Reported on playerpulse: the
// flake is on `dev`, the dispatch was cut from main, and `nix develop` stood in
// a worktree with no flake.nix and found nothing. Where the session's own
// directory has a flake it is still the one used; where it has none, the
// checkout's is named outright.
func TestEnvInNamesTheCheckoutsFlakeWhereTheWorktreeHasNone(t *testing.T) {
	flake := func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "flake.nix"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	checkout, worktree := t.TempDir(), t.TempDir()
	flake(checkout)

	if got, want := EnvIn(NixDevelop, checkout, worktree), "nix develop "+checkout+" --no-write-lock-file --command"; got != want {
		t.Errorf("EnvIn(no flake in the worktree) = %q, want %q", got, want)
	}
	flake(worktree)
	if got := EnvIn(NixDevelop, checkout, worktree); got != NixDevelop {
		t.Errorf("EnvIn(the worktree's own flake) = %q, want it searched for where it stands", got)
	}
	if got := EnvIn("devbox run --", checkout, t.TempDir()); got != "devbox run --" {
		t.Errorf("an explicit [session_env] was rewritten: %q", got)
	}

	// The env string is split on whitespace, so a path holding a space goes as
	// a URL with the space escaped.
	spaced := filepath.Join(t.TempDir(), "my repo")
	if err := os.MkdirAll(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	flake(spaced)
	got := EnvIn(NixDevelop, spaced, t.TempDir())
	if len(strings.Fields(got)) != 5 || !strings.Contains(got, "git+file://") || !strings.Contains(got, "my%20repo") {
		t.Errorf("EnvIn(spaced checkout) = %q", got)
	}
}

// A nix that cannot run flakes is no nix for a dispatch: `nix develop` would
// refuse before the session started, on the installers that leave the features
// off by default. Determinate Nix ships flakes as stable, whatever the list says.
func TestNixCountsOnlyWhereFlakesRun(t *testing.T) {
	for _, tc := range []struct {
		features, version string
		want              bool
	}{
		{"fetch-tree flakes nix-command\n", "nix (Nix) 2.34.8\n", true},
		{"nix-command\n", "nix (Nix) 2.24.0\n", false},
		{"flakes\n", "nix (Nix) 2.24.0\n", false},
		{"", "nix (Nix) 2.24.0\n", false},
		{"nix-command\n", "nix (Determinate Nix 3.8.2) 2.30.1\n", true},
	} {
		if got := flakesOn(tc.features, tc.version); got != tc.want {
			t.Errorf("flakesOn(%q, %q) = %v, want %v", tc.features, tc.version, got, tc.want)
		}
	}
}
