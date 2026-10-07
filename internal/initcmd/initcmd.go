// Package initcmd performs first-run setup: config file, state directories,
// environment checks, and — with explicit consent — installing the global
// lifecycle hook and the usage status line into ~/.claude/settings.json and
// every account's own settings.json (internal/account). Hooks cannot be injected at
// launch time (Claude Code only reads them from settings files), which is why
// a single machine-wide hook is the mechanism.
package initcmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

func Run() error {
	path, created, err := config.WriteDefault()
	if err != nil {
		return err
	}
	if created {
		fmt.Println("✓ wrote config:", path)
	} else {
		fmt.Println("✓ config exists:", path)
	}
	if err := state.EnsureDirs(); err != nil {
		return err
	}
	fmt.Println("✓ state dir:", state.Dir())

	for _, tool := range []string{"tmux", "claude", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			fmt.Printf("✗ %s not found on PATH — required\n", tool)
		} else {
			fmt.Printf("✓ %s found\n", tool)
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Println("- gh not found (optional; needed later for PR/deploy tracking)")
	} else {
		fmt.Println("✓ gh found")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	rs := repos.Discover(cfg)
	fmt.Printf("✓ discovered %d repos under %s\n", len(rs), strings.Join(cfg.Roots, ", "))

	return installAll(cfg)
}

// hookSpec describes one settings.json hook entry we need.
type hookSpec struct {
	event   string
	matcher string
	arg     string
}

func hookSpecs() []hookSpec {
	return []hookSpec{
		{event: "SessionStart", arg: "SessionStart"},
		{event: "UserPromptSubmit", arg: "UserPromptSubmit"},
		{event: "PostToolUse", arg: "PostToolUse"},
		{event: "Stop", arg: "Stop"},
		// Fires instead of Stop when an API error ends the turn; without it
		// such a session reads as working forever.
		{event: "StopFailure", arg: "StopFailure"},
		{event: "SessionEnd", arg: "SessionEnd"},
		// The fan-out annotation: which subagents a session has spun out.
		// A claude too old to know these event names ignores the entries.
		{event: "SubagentStart", arg: "SubagentStart"},
		{event: "SubagentStop", arg: "SubagentStop"},
		{event: "Notification", matcher: "idle_prompt", arg: "Notification:idle_prompt"},
		{event: "Notification", matcher: "permission_prompt", arg: "Notification:permission_prompt"},
	}
}

const hookMarker = "claude-dispatcher hook"

const nixStore = "/nix/store/"

// hookExe returns the path to bake into the hook command.
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
func hookExe(exe, argv0 string, lookPath, eval func(string) (string, error)) string {
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
	if resolved, err := eval(cand); err != nil || resolved != exe {
		return exe
	}
	return cand
}

// installExe is the binary the hooks and the status line are baked to, or ""
// when there is no durable one to bake (a `go run` build) — said, not hidden.
func installExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if strings.Contains(exe, "go-build") {
		fmt.Println("\n! running via `go run` — install a real binary first (make install), then re-run init,")
		fmt.Println("  otherwise the hook would point at a temporary build path.")
		return "", nil
	}
	exe = hookExe(exe, os.Args[0], exec.LookPath, filepath.EvalSymlinks)
	if strings.HasPrefix(exe, nixStore) {
		fmt.Println("\n! this build was run straight out of /nix/store, so the hook can only point at")
		fmt.Println("  this exact build — it stops firing at the next garbage collection. Install it")
		fmt.Println("  into a profile (nix profile install / systemPackages) and re-run init.")
	}
	return exe, nil
}

// installAll installs into the human's own config directory and into every
// account's. Each directory reads only its own settings.json, so a dispatch
// under an account whose directory has no hooks reports nothing at all: it
// would sit at "launching" until a sweep retired it, a ghost from birth.
func installAll(cfg *config.Config) error {
	exe, err := installExe()
	if err != nil || exe == "" {
		return err
	}
	for _, a := range account.List(cfg) {
		if err := installInto(a.ConfigDir(), exe, a.Name); err != nil {
			return err
		}
	}
	return nil
}

// InstallFor installs the hooks and the status line into one account's
// config directory — `account add`, which has just made it.
func InstallFor(a account.Account) error {
	exe, err := installExe()
	if err != nil || exe == "" {
		return err
	}
	return installInto(a.ConfigDir(), exe, a.Name)
}

// installInto adds whatever of ours configDir's settings.json lacks, after
// asking: the lifecycle hooks, and the status line that records how much of
// the account's limits are left (account/limits.go).
func installInto(configDir, exe, name string) error {
	settingsPath := filepath.Join(configDir, "settings.json")
	root := map[string]any{}
	raw, readErr := os.ReadFile(settingsPath)
	if readErr == nil {
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("%s is not valid JSON — refusing to touch it: %w", settingsPath, err)
		}
	}

	added := addHooks(root, exe)
	line := addStatusLine(root, exe)
	if added == 0 && line == "" {
		fmt.Printf("✓ hooks and status line already installed in %s (%s)\n", settingsPath, name)
		return nil
	}

	fmt.Printf("\nAbout to change %s (account %s):\n", settingsPath, name)
	if added > 0 {
		fmt.Printf("  · add %d hook entries, each running: %s hook <event>\n", added, exe)
		fmt.Println("    They fire on every Claude Code session using this config (that is how")
		fmt.Println("    status tracking works; sessions started outside the cockpit are logged too).")
	}
	if line != "" {
		fmt.Println("  · " + line)
		fmt.Println("    It records how much of this subscription's 5-hour and weekly limits is")
		fmt.Println("    left, which the dispatch forms show beside each account.")
	}
	fmt.Print("Proceed? [y/N] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		fmt.Println("Skipped. Status tracking will not work until the hook is installed;")
		fmt.Println("re-run `claude-dispatcher init` when ready.")
		return nil
	}

	if readErr == nil {
		backup := settingsPath + ".claude-dispatcher.bak"
		if err := os.WriteFile(backup, raw, 0o644); err != nil {
			return fmt.Errorf("writing backup: %w", err)
		}
		fmt.Println("✓ backed up existing settings to", backup)
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, append(out, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println("✓ installed in", settingsPath)
	return nil
}

// addHooks adds every hook entry root lacks and says how many it added.
func addHooks(root map[string]any, exe string) int {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	added := 0
	for _, spec := range hookSpecs() {
		entries, _ := hooks[spec.event].([]any)
		if hasOurHook(entries, spec.matcher) {
			continue
		}
		entry := map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": fmt.Sprintf("%s hook %s", exe, spec.arg),
			}},
		}
		if spec.matcher != "" {
			entry["matcher"] = spec.matcher
		}
		hooks[spec.event] = append(entries, any(entry))
		added++
	}
	if added > 0 {
		root["hooks"] = hooks
	}
	return added
}

// addStatusLine installs our status line in root, and describes the change —
// "" when there is none to make.
//
// A status line the human already has is wrapped, not replaced: ours runs
// theirs on the same input and prints what it prints (`--then`), so their
// line looks exactly as it did. Every other key of theirs (padding and the
// like) is kept. Windows is left alone where there is one already, because
// the command there is not run by a POSIX shell and quoting it for one would
// be a guess; that account's forms then say they have no reading.
func addStatusLine(root map[string]any, exe string) string {
	ours := exe + " statusline"
	sl, _ := root["statusLine"].(map[string]any)
	if sl == nil {
		root["statusLine"] = map[string]any{"type": "command", "command": ours}
		return "add a status line running: " + ours + " (it draws nothing)"
	}
	cmd, _ := sl["command"].(string)
	if strings.Contains(cmd, account.StatusLineMarker) {
		return ""
	}
	if strings.TrimSpace(cmd) == "" || runtime.GOOS == "windows" {
		if strings.TrimSpace(cmd) == "" {
			sl["type"], sl["command"] = "command", ours
			return "add a status line running: " + ours + " (it draws nothing)"
		}
		return ""
	}
	sl["command"] = ours + " --then " + shellQuote(cmd)
	return "wrap your status line so it also records usage: " + sl["command"].(string)
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func hasOurHook(entries []any, matcher string) bool {
	for _, e := range entries {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if m, _ := entry["matcher"].(string); m != matcher {
			continue
		}
		inner, _ := entry["hooks"].([]any)
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, hookMarker) {
				return true
			}
		}
	}
	return false
}
