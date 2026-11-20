// Package binpath is the path to name this binary by in anything that outlives
// the process that wrote it: the hook command `init` bakes into settings.json,
// and the command a dispatch's prompt tells its session to run.
package binpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Durable returns the path to name the binary exe by, given argv0, the name it
// was invoked as.
//
// os.Executable answers with the real file — on Linux /proc/self/exe is
// already resolved — which for a Nix install is /nix/store/<hash>-…: a path
// that changes with every upgrade and disappears at the next garbage
// collection, leaving a hook that silently never fires again. Homebrew has the
// same shape, one Cellar version deep. The durable name is the one the human
// invoked through (/run/current-system/sw/bin/…, ~/.nix-profile/bin/…, the
// brew shim, ~/.local/bin/…), so prefer that — but only once it is proven to
// resolve to this very binary. PATH may hold a second, older copy (the
// brew-vs-~/.local/bin trap in the README); a hook aimed at the wrong build
// would be worse than one pinned to the right one.
func Durable(exe, argv0 string, lookPath, eval func(string) (string, error)) string {
	cand := argv0
	if !strings.ContainsRune(cand, filepath.Separator) {
		found, err := lookPath(cand)
		if err != nil {
			return exe
		}
		cand = found
	}
	cand, err := filepath.Abs(cand)
	if err != nil {
		return exe
	}
	if resolved, err := eval(cand); err != nil || (resolved != exe && !wraps(resolved, exe)) {
		return exe
	}
	return cand
}

// wraps reports whether wrapper is the makeWrapper script in front of exe. The
// Nix package wraps the binary to put git and tmux on its PATH, and wrapProgram
// does it by moving bin/claude-dispatcher to bin/.claude-dispatcher-wrapped and
// writing a script that execs it in its place. So the name on PATH resolves to
// the script and os.Executable to the moved binary: the two never compare
// equal, and without this every Nix install pinned its hook to the store path
// — which the next garbage collection deletes. Same directory is the proof: a
// store path's hash names exactly one build.
func wraps(wrapper, exe string) bool {
	return filepath.Dir(wrapper) == filepath.Dir(exe) &&
		filepath.Base(exe) == "."+filepath.Base(wrapper)+"-wrapped"
}

// Name is what this binary is called when there is no path worth naming.
const Name = "claude-dispatcher"

// executable is os.Executable, as a seam.
var executable = os.Executable

// Self is the durable path of the running binary (Durable), or Name when there
// is none: a `go run` or `go test` build lives in a temporary directory the
// toolchain deletes, and a command naming it would outlive it. The bare name is
// then the honest answer — whatever PATH finds is what the human would run.
func Self() string {
	exe, err := executable()
	if err != nil {
		return Name
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if Temporary(exe) {
		return Name
	}
	return Durable(exe, os.Args[0], exec.LookPath, filepath.EvalSymlinks)
}

// Temporary reports a binary the Go toolchain built into its own scratch space
// (`go run`, `go test`), which is gone once the command that built it ends.
func Temporary(exe string) bool { return strings.Contains(exe, "go-build") }
