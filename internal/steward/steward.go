// Package steward runs the fleet's lead: one Claude Code session of the
// dispatcher's own that watches every dispatcher, pushes along what is plainly
// inside its brief, and leaves the human's calls to the human with a note that
// says why.
//
// It is a session, not a loop in the cockpit, because what it does is
// judgement — "is this question one the brief already answers?" — and the
// cockpit's rule is that it never guesses. It is not a dispatcher: it has no
// record, no branch and no worktree, runs without CLAUDE_DISPATCHER_ID so its
// hooks are attributed to nothing, and never appears on the table it reads.
//
// Everything it knows and does goes through the fleet verbs (internal/fleetcmd):
// `status --next` to sleep until a wait nobody has handled, `status --json` to
// look, `reply`/`note`/`park` to act. It sleeps as a Claude Code background
// task, so an idle fleet costs it nothing, and "handled" lives on the records,
// so a restart misses nothing.
package steward

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// Session is the steward's supervisor session. The underscore is the point: a
// dispatcher's session is "disp-" plus a slug, and slugs are [a-z0-9-], so no
// feature — not even one named "steward" — can ever be given this name.
const Session = "disp_steward"

// Opening is the steward's first message. The brief itself is CLAUDE.md in its
// folder, which claude loads on every start and keeps through compaction.
const Opening = "Start stewarding the fleet, as your CLAUDE.md describes."

// Dir is the steward's folder: under the dispatcher's own state, holding
// nothing but its brief and its settings.
func Dir() string { return filepath.Join(state.Dir(), "steward") }

// The supervisor calls, as seams for tests.
var (
	hasSession  = supervisor.HasSession
	newSession  = supervisor.NewSession
	killSession = supervisor.KillSession
	trustDir    = dispatch.TrustOwnDir
	executable  = os.Executable
)

// Running reports whether the steward's session is up.
func Running() bool { return hasSession(Session) }

// ErrRunning is Start finding the steward already up — not a failure; the
// caller attaches to it.
var ErrRunning = errors.New("the steward is already running")

// Start writes the steward's folder fresh — its brief and settings follow the
// binary, so an upgrade changes how it works on its next start — trusts it,
// and starts the session. One already up is left alone (ErrRunning).
func Start() error {
	if !supervisor.Available() {
		return errors.New(supervisor.Backend() + " is not available")
	}
	if Running() {
		return ErrRunning
	}
	exe, err := binary()
	if err != nil {
		return err
	}
	dir := Dir()
	if err := write(dir, exe); err != nil {
		return err
	}
	trusted := trustDir(dir)
	if err := newSession(Session, dir, dispatch.StewardCommand(Opening)); err != nil {
		return err
	}
	if !trusted {
		// Not fatal: the session is up, and the trust dialog is one keypress
		// on attach. Saying so beats a steward that looks idle.
		return ErrUntrusted
	}
	return nil
}

// ErrUntrusted is a steward that started but could not be marked trusted, so
// it is sitting on Claude Code's trust dialog until someone attaches.
var ErrUntrusted = errors.New("started, but its folder could not be marked trusted — attach and accept the trust prompt once")

// Stop ends the steward's session. Its folder stays: it holds nothing that
// matters, and the next Start rewrites it.
func Stop() error {
	if !Running() {
		return errors.New("the steward is not running")
	}
	return killSession(Session)
}

// binary is the dispatcher's own path, which the brief and the permission
// rules name: a steward calling whatever `claude-dispatcher` PATH resolves to
// could be driving a different build from the one that started it.
func binary() (string, error) {
	exe, err := executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if strings.Contains(exe, "go-build") {
		return "", errors.New("running via `go run` — install a real binary first (make install); the steward would be pointed at a temporary build")
	}
	return exe, nil
}

// write lays down CLAUDE.md (the brief) and .claude/settings.json (what the
// steward may run without asking, and what it may never do).
func write(dir, exe string) error {
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(Brief(exe)), 0o644); err != nil {
		return err
	}
	b, err := json.MarshalIndent(Settings(exe), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), append(b, '\n'), 0o644)
}

// Settings is the steward's project settings: its own verbs and the read-only
// forge and git reads it gathers facts with are allowed outright, so it never
// stops on a permission prompt nobody is watching; editing anything is denied,
// because a lead that starts writing code is a second dispatcher nobody
// dispatched.
func Settings(exe string) map[string]any {
	allow := []string{"Read", "Grep", "Glob"}
	for _, verb := range []string{"status", "reply", "note", "park", "unpark"} {
		allow = append(allow, "Bash("+exe+" "+verb+":*)")
	}
	for _, read := range []string{
		"gh pr view", "gh pr checks", "gh pr diff", "gh pr list",
		"gh run list", "gh run view", "git log", "git diff", "git status", "git show",
	} {
		allow = append(allow, "Bash("+read+":*)")
	}
	return map[string]any{
		"permissions": map[string]any{
			"allow": allow,
			"deny":  []string{"Edit", "Write", "NotebookEdit"},
		},
	}
}
