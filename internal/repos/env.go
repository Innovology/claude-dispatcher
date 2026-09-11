package repos

// env.go answers the two questions that decide where a repository's dispatchers
// run and what they can see: which supervisor server their sessions live on,
// and what command those sessions are launched under.
//
// Both are properties of the REPOSITORY, not of the machine and not of the
// product. A product is a grouping lens over repos that may have nothing in
// common but a goal; its members can be a Go service, a Node app and a flake,
// and each has to bring its own. See docs/adr/0013.

import (
	"os"
	"os/exec"
	"path/filepath"
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
// A checkout with a flake.nix, on a machine with nix, is a repo that has said
// where its toolchain comes from — so we use it, and nobody types anything. The
// second half of that is not optional: plenty of repos carry a flake as an
// optional convenience, and on a machine without nix, launching under one would
// turn every dispatch into a launch that dies. This is the same rule the mode
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

func hasFlake(checkout string) bool {
	_, err := os.Stat(filepath.Join(checkout, "flake.nix"))
	return err == nil
}

// nixAvailable is read once. Discover runs on every load, over every repo in
// the portfolio, and this answer cannot change while the cockpit is open in any
// way that matters more than the PATH lookups would cost.
var nixAvailable = sync.OnceValue(func() bool {
	_, err := exec.LookPath("nix")
	return err == nil
})
