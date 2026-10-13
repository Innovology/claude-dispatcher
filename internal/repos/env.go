package repos

// env.go answers the two questions that decide where a repository's dispatchers
// run and what they can see: which supervisor server their sessions live on,
// and what command those sessions are launched under.
//
// Both are properties of the REPOSITORY, not of the machine and not of the
// product. A product is a grouping lens over repos that may have nothing in
// common but a goal; its members can be a Go service, a Node app and a flake,
// and each has to bring its own. See docs/adr/0014.

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"claude-dispatcher/internal/config"
)

// NixDevelop is the command a flake repo's sessions are launched under.
//
// --command runs the rest INSIDE the dev shell, which is what puts the flake's
// binaries on the session's PATH. --no-write-lock-file is there because a repo
// with a flake and no flake.lock would otherwise have one written by whichever
// dispatch happened to start first — into that dispatch's worktree, where it
// lands in the feature's diff and in the provenance every effort and shipping
// figure is read from. A dispatch may read a repo's inputs; it does not get to
// decide them.
const NixDevelop = "nix develop --no-write-lock-file --command"

// socketFor is the supervisor server a repo's sessions live on.
//
// The default is the repo's own name — which is the name git knows it by, not
// its folder (see repoName) — so that every repo is its own server without
// anybody configuring anything. A human who already keeps per-project servers
// of their own names theirs in [sockets], because a socket name is a label
// chosen outside the repository and there is nothing inside it to read.
func socketFor(cfg *config.Config, name string) string {
	if cfg != nil {
		if s, ok := cfg.Sockets[name]; ok {
			return strings.TrimSpace(s)
		}
	}
	return name
}

// envFor is the command prefix a repo's sessions are launched under.
//
// A checkout with a flake.nix, on a machine whose nix runs flakes, is a repo
// that has said where its toolchain comes from — so we use it, and nobody types
// anything. The second half of that is not optional: plenty of repos carry a
// flake as an optional convenience, and on a machine without a nix that runs
// flakes (nixAvailable), launching under one would turn every dispatch into a
// launch that dies. This is the same rule the mode
// and model flags follow — what we cannot vouch for, we do not pass.
//
// An explicit [session_env] entry wins, including an empty one, which is how a
// flake with no devShell (nix develop fails outright) is told to stop trying.
func envFor(cfg *config.Config, name, checkout string) string {
	if cfg != nil {
		if e, ok := cfg.SessionEnv[name]; ok {
			return strings.TrimSpace(e)
		}
	}
	if checkout == "" || !hasFlake(checkout) || !nixAvailable() {
		return ""
	}
	return NixDevelop
}

// EnvIn is env made to work from dir, the directory a session actually runs
// in, given that it was resolved for checkout.
//
// The flake that decided env was the CHECKOUT's — the one the repo's row reads
// — and a session runs somewhere else: a worktree cut from the remote's
// default, or from a ROOT the human named. Those need not carry the same
// files. Reported on playerpulse, whose flake is on `dev` and not on `main`:
// the dispatch was cut from origin/main, `nix develop` stood in a worktree
// with no flake.nix, searched up, found nothing, and the session never
// started. A flake in dir is still the one used — it is the code being worked
// on, and a branch may change its own toolchain — but where dir has none, the
// checkout's is named outright rather than searched for, so the dispatch gets
// the repo's environment as the human runs it.
//
// Only the bare NixDevelop is rewritten: an explicit [session_env] says what
// it means and is passed as written. The reference is a plain path unless it
// holds whitespace, which the env string is split on (tmux.cmdIn), and then a
// git+file URL with the path escaped.
func EnvIn(env, checkout, dir string) string {
	if env != NixDevelop || dir == "" || checkout == "" || hasFlake(dir) || !hasFlake(checkout) {
		return env
	}
	ref := checkout
	if strings.ContainsAny(ref, " \t\n") {
		ref = (&url.URL{Scheme: "git+file", Path: checkout}).String()
	}
	return "nix develop " + ref + " --no-write-lock-file --command"
}

// shellFor is the interactive shell a dispatch pane drops to once claude
// exits, or "" for "ask the session's own tmux server".
//
// It is global rather than per-repo, and deliberately so: a shell is a property
// of the human at the keyboard, not of the code they are dispatching into. The
// repo carries it anyway (Repo.Shell) because the launch reads everything it
// needs off the repo, which is also what makes a [shells] table a one-line
// change here if a repo ever needs its own.
func shellFor(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Shell)
}

// worktreeRootFor is the directory a dispatch's own checkout is cut in, or ""
// for the state directory this tool keeps its own under.
//
// A project here is the folder the checkouts share — the one holding the
// common git dir, `<project>/.bare`, with `main` and every branch beside it.
// That is the only layout where "put it with the others" names a real place.
// An ordinary clone carries its git dir at `<clone>/.git`, so the parent is
// wherever the human happens to keep their repositories, and cutting dispatch
// checkouts into it would be scattering folders through somebody's source
// directory rather than joining a convention they already keep. Those stay
// where they were, which is also what every repo does until this is asked for.
func worktreeRootFor(cfg *config.Config, gitDir string) string {
	if !cfg.WorktreesBesideProject() || gitDir == "" {
		return ""
	}
	if filepath.Base(gitDir) == ".git" {
		return ""
	}
	return filepath.Dir(gitDir)
}

// branchPrefixFor is what this repo's dispatch branches start with, or nil for
// the default. nil rather than a resolved string, so a Repo nobody configured
// — a test's, a demo's — still cuts `feature/…` rather than a bare slug: the
// two answers must not share the zero value when one of them is "no prefix".
func branchPrefixFor(cfg *config.Config) *string {
	if cfg == nil || cfg.BranchPrefix == nil {
		return nil
	}
	p := strings.TrimSpace(*cfg.BranchPrefix)
	return &p
}

func hasFlake(checkout string) bool {
	_, err := os.Stat(filepath.Join(checkout, "flake.nix"))
	return err == nil
}

// nixAvailable reports a nix that can run `nix develop` on a flake, read once.
// Discover runs on every load, over every repo in the portfolio, and this
// answer cannot change while the cockpit is open in any way that matters more
// than asking would cost.
//
// A nix binary is not enough. Flakes and the `nix` command are experimental
// features, off by default in the installer most people use on macOS and on
// Linux outside NixOS, and there `nix develop` refuses before it starts — every
// dispatch and every jump-in for a repo carrying a flake.nix would fail, on a
// machine whose owner never asked for nix to be involved. So the features are
// asked for (`nix config show experimental-features`, which itself fails when
// the nix command is off). Determinate Nix ships flakes as stable rather than
// experimental, so its build counts whatever that list says.
var nixAvailable = sync.OnceValue(func() bool {
	if _, err := exec.LookPath("nix"); err != nil {
		return false
	}
	out, err := exec.Command("nix", "config", "show", "experimental-features").Output()
	if err != nil {
		return false
	}
	version, _ := exec.Command("nix", "--version").Output()
	return flakesOn(string(out), string(version))
})

// flakesOn reads the two answers nixAvailable asks for: the enabled features,
// and the version line, which names Determinate Nix where it is that build.
func flakesOn(features, version string) bool {
	if strings.Contains(version, "Determinate") {
		return true
	}
	f := strings.Fields(features)
	return slices.Contains(f, "flakes") && slices.Contains(f, "nix-command")
}
