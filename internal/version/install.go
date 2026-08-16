package version

// install.go answers "how did this binary get here, and what would replace
// it?" so the cockpit can offer to upgrade in place instead of printing a
// command and asking the human to quit, run it, and come back.
//
// The answer is read off the running binary's own path, resolved through its
// symlinks — that is the only evidence on the machine that cannot disagree
// with itself. A guess by GOOS (what UpgradeHint used to do) tells a Nix user
// to run brew.
//
// Nothing here shells out. Detection must be cheap and must never hang: it
// runs behind the footer, which redraws on every frame.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// binName is the installed command, and — for every package manager below —
// also the package name. wingetID is the one identifier that differs.
const (
	binName  = "claude-dispatcher"
	wingetID = "Innovology.claude-dispatcher"
	relPage  = "github.com/Innovology/claude-dispatcher/releases"
)

// Method names how this build got onto the machine. The empty method is an
// install we could not place — a copied binary, a `go build` output, a package
// manager we do not know.
type Method string

const (
	MethodUnknown     Method = ""
	MethodBrewCask    Method = "homebrew cask"
	MethodBrewFormula Method = "homebrew formula"
	MethodNixProfile  Method = "nix profile"
	MethodNixManaged  Method = "nix"
	MethodScoop       Method = "scoop"
	MethodWinget      Method = "winget"
)

// Install is what we know about the running build: where it is, how it was
// installed, and the one command that would replace it with the published
// release. Cmd is nil when we will not run anything — either because we cannot
// place the install, or because placing it told us an imperative upgrade would
// be wrong (see MethodNixManaged). Note says why, in the words the footer uses.
type Install struct {
	Method Method
	Path   string
	Cmd    []string
	Note   string
}

// CanUpgrade reports whether we know a command that upgrades this build, and
// so whether the cockpit offers the key at all.
func (i Install) CanUpgrade() bool { return len(i.Cmd) > 0 }

// Hint is the tail of the footer's version clause: the command we would run,
// or why there is none.
func (i Install) Hint() string {
	if !i.CanUpgrade() {
		return i.Note
	}
	return strings.Join(i.Cmd, " ")
}

// Env is the environment the upgrade command should run in.
//
// Homebrew only refreshes its taps if HOMEBREW_AUTO_UPDATE_SECS have passed
// since the last fetch (a day, by default), and skips the refresh entirely
// when HOMEBREW_NO_AUTO_UPDATE is set. Either one turns "upgrade me now" into
// "already installed" on a machine that has not fetched since the release went
// out — which is every machine, seconds after a release. Forcing the refresh
// for this one child process is what makes the key mean what it says; it is
// also why there is no separate `brew update` step, which would fetch every
// tap the human has rather than the one we need.
func (i Install) Env() []string {
	env := os.Environ()
	if i.Method != MethodBrewCask && i.Method != MethodBrewFormula {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOMEBREW_NO_AUTO_UPDATE=") ||
			strings.HasPrefix(kv, "HOMEBREW_AUTO_UPDATE_SECS=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HOMEBREW_AUTO_UPDATE_SECS=0")
}

// Detect places the running build. The result cannot change while the process
// lives — the binary behind an upgrade is a new process — so it is computed
// once and reused; the footer asks for it on every frame.
func Detect() Install { detectOnce.Do(func() { detected = detect() }); return detected }

var (
	detectOnce sync.Once
	detected   Install
)

func detect() Install {
	exe, err := os.Executable()
	if err != nil {
		return Install{Note: "cannot locate the running binary — see " + relPage}
	}
	// The invoking path is usually a symlink into the package manager's store
	// (/opt/homebrew/bin/x → …/Caskroom/x/3.1.1/x). The store path is the one
	// that names the manager, so resolve before classifying.
	resolved := exe
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		resolved = r
	}
	return classify(resolved, nixProfileSelector)
}

// classify is the whole decision, taking its one filesystem question as an
// argument so the table test can drive every branch without a real install.
func classify(path string, nixSelector func(string) (string, bool)) Install {
	// Backslashes are folded unconditionally rather than through
	// filepath.ToSlash, which only knows the separator of the host it is
	// compiled for: on a unix build it leaves a Windows path untouched, so the
	// scoop and winget branches below would only ever match on Windows and
	// could not be tested anywhere else. A path is classified by what it says,
	// not by who is reading it.
	p := strings.ReplaceAll(path, `\`, "/")
	lower := strings.ToLower(p)
	switch {
	// Homebrew. The tap ships a cask (see .goreleaser.yml homebrew_casks), so
	// Caskroom is the expected case; Cellar is handled because a formula is
	// what a hand-written tap or an older install would have left.
	case strings.Contains(p, "/Caskroom/"):
		return Install{Method: MethodBrewCask, Path: path,
			Cmd: []string{"brew", "upgrade", "--cask", binName}}
	case strings.Contains(p, "/Cellar/"):
		return Install{Method: MethodBrewFormula, Path: path,
			Cmd: []string{"brew", "upgrade", binName}}

	// Nix. A store path alone does not say whether the install is imperative
	// (`nix profile install`, upgradable in place) or declarative
	// (home-manager, a NixOS module, a flake input — where the version is
	// pinned in a file and an imperative upgrade would either fail or install
	// a second copy that shadows the declared one). Only the profile manifest
	// settles it; without that proof we say so and offer nothing.
	//
	// Matched anywhere in the path, not just at the front: a chroot or
	// single-user Nix keeps its store under the user's home. Over-matching is
	// harmless here because it cannot produce a command — only the manifest
	// below can do that — so the worst case is telling someone their install is
	// Nix-managed, which is what a path holding a Nix store says it is.
	case strings.Contains(p, "/nix/store/"):
		if sel, ok := nixSelector(path); ok {
			return Install{Method: MethodNixProfile, Path: path,
				Cmd: []string{"nix", "profile", "upgrade", sel}}
		}
		return Install{Method: MethodNixManaged, Path: path,
			Note: "nix-managed · upgrade it where it is declared"}

	case strings.Contains(lower, "/scoop/"):
		return Install{Method: MethodScoop, Path: path,
			Cmd: []string{"scoop", "update", binName}}
	case strings.Contains(lower, "/winget/"):
		return Install{Method: MethodWinget, Path: path,
			Cmd: []string{"winget", "upgrade", "--id", wingetID}}
	}
	return Install{Path: path, Note: "see " + relPage}
}

// nixProfileSelector reports whether the running store path is registered in
// the user's imperative `nix profile`, and returns the selector that names it
// there.
//
// The manifest is the proof. `nix profile` writes manifest.json; nix-env and
// home-manager write a manifest.nix instead, so a store path with no entry in
// a manifest.json is one we must not try to upgrade. Reading it also answers
// the question we would otherwise have to guess at — what the element is
// called — which is what `nix profile upgrade` takes as its argument.
func nixProfileSelector(resolved string) (string, bool) {
	for _, dir := range nixProfileDirs() {
		raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			continue
		}
		if sel, ok := manifestSelector(raw, resolved); ok {
			return sel, true
		}
	}
	return "", false
}

func nixProfileDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".nix-profile"))
		dirs = append(dirs, filepath.Join(home, ".local", "state", "nix", "profiles", "profile"))
	}
	if st := os.Getenv("XDG_STATE_HOME"); st != "" {
		dirs = append(dirs, filepath.Join(st, "nix", "profiles", "profile"))
	}
	return dirs
}

// manifestSelector finds the element whose store paths contain the running
// binary and returns how to name it to `nix profile upgrade`.
//
// The manifest has had two shapes: version 1 and 2 hold an array, where an
// element is named by its index, and version 3 an object keyed by name. Both
// are read, because which one is on disk depends on the human's Nix, not ours.
func manifestSelector(raw []byte, resolved string) (string, bool) {
	type element struct {
		StorePaths []string `json:"storePaths"`
	}
	holds := func(e element) bool {
		for _, sp := range e.StorePaths {
			if resolved == sp || strings.HasPrefix(resolved, strings.TrimSuffix(sp, "/")+"/") {
				return true
			}
		}
		return false
	}

	var byName struct {
		Elements map[string]element `json:"elements"`
	}
	if json.Unmarshal(raw, &byName) == nil && byName.Elements != nil {
		for name, e := range byName.Elements {
			if holds(e) {
				return name, true
			}
		}
		return "", false
	}

	var byIndex struct {
		Elements []element `json:"elements"`
	}
	if json.Unmarshal(raw, &byIndex) == nil {
		for i, e := range byIndex.Elements {
			if holds(e) {
				return strconv.Itoa(i), true
			}
		}
	}
	return "", false
}

// UpgradeHint is the command that upgrades this build in place, or why there
// is none. It used to answer from GOOS alone, which told every non-Homebrew
// unix install to run brew; it now answers from the binary's own path.
func UpgradeHint() string { return Detect().Hint() }
