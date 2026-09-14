// Package initcmd performs first-run setup: config file, state directories,
// environment checks, and — with explicit consent — installing the global
// lifecycle hook into ~/.claude/settings.json. Hooks cannot be injected at
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
	"strings"

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

	return installHook()
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
		{event: "SessionEnd", arg: "SessionEnd"},
		// The fan-out annotation: which subagents a session has spun out.
		// A claude too old to know these event names ignores the entries.
		{event: "SubagentStart", arg: "SubagentStart"},
		{event: "SubagentStop", arg: "SubagentStop"},
		{event: "Notification", matcher: "idle_prompt", arg: "Notification:idle_prompt"},
		{event: "Notification", matcher: "permission_prompt", arg: "Notification:permission_prompt"},
	}
}

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
	if resolved, err := eval(cand); err != nil || !(resolved == exe || wraps(resolved, exe)) {
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

func installHook() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if strings.Contains(exe, "go-build") {
		fmt.Println("\n! running via `go run` — install a real binary first (make install), then re-run init,")
		fmt.Println("  otherwise the hook would point at a temporary build path.")
		return nil
	}
	exe = hookExe(exe, os.Args[0], exec.LookPath, filepath.EvalSymlinks)
	if strings.HasPrefix(exe, nixStore) {
		fmt.Println("\n! this build was run straight out of /nix/store, so the hook can only point at")
		fmt.Println("  this exact build — it stops firing at the next garbage collection. Install it")
		fmt.Println("  into a profile (nix profile install / systemPackages) and re-run init.")
	}

	settingsPath := filepath.Join(claudeConfigDir(), "settings.json")
	root := map[string]any{}
	raw, readErr := os.ReadFile(settingsPath)
	if readErr == nil {
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("%s is not valid JSON — refusing to touch it: %w", settingsPath, err)
		}
	}

	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	ch := reconcileHooks(hooks, exe)
	if ch == (hookChanges{}) {
		fmt.Println("✓ lifecycle hook already installed in", settingsPath)
		return nil
	}
	root["hooks"] = hooks

	fmt.Printf("\nAbout to update %s:\n", settingsPath)
	if ch.added > 0 {
		fmt.Printf("  add %d hook %s\n", ch.added, plural(ch.added, "entry", "entries"))
	}
	if ch.repointed > 0 {
		fmt.Printf("  repoint %d hook %s at this binary (they named another path)\n", ch.repointed, plural(ch.repointed, "entry", "entries"))
	}
	if ch.dropped > 0 {
		fmt.Printf("  remove %d duplicate hook %s left by earlier installs\n", ch.dropped, plural(ch.dropped, "entry", "entries"))
	}
	fmt.Printf("Each runs: %s hook <event>\n", exe)
	fmt.Println("This fires on every Claude Code session machine-wide (that is how status")
	fmt.Println("tracking works; sessions started outside the cockpit are logged too).")
	fmt.Print("Proceed? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
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
	fmt.Println("✓ hook installed in", settingsPath)
	fmt.Println("\nAll set — run `claude-dispatcher` to open the cockpit.")
	return nil
}

// hookChanges counts what reconcileHooks did to the settings.
type hookChanges struct {
	added, repointed, dropped int
}

// reconcileHooks leaves hooks holding exactly one hook of ours per hookSpec,
// each running exe. It used to add a spec only when no entry of ours was
// there, which could not repair anything: a hook naming a build that has since
// been garbage-collected was "already installed", and one naming
// .claude-dispatcher-wrapped was not recognised as ours at all, so every re-run
// of init appended another full set beside the dead ones. Ours is recognised by
// what the command runs, never by its exact path, so a hook left by any earlier
// install is repointed or removed; hooks that are not ours, including ones
// sharing an entry with ours, are left exactly as they were.
func reconcileHooks(hooks map[string]any, exe string) hookChanges {
	var ch hookChanges
	for _, spec := range hookSpecs() {
		want := fmt.Sprintf("%s hook %s", exe, spec.arg)
		entries, _ := hooks[spec.event].([]any)
		kept := make([]any, 0, len(entries))
		have, stale := false, 0
		for _, e := range entries {
			entry, ok := e.(map[string]any)
			if m, _ := entry["matcher"].(string); !ok || m != spec.matcher {
				kept = append(kept, e)
				continue
			}
			inner, _ := entry["hooks"].([]any)
			rest := make([]any, 0, len(inner))
			for _, h := range inner {
				hm, _ := h.(map[string]any)
				cmd, _ := hm["command"].(string)
				switch {
				case !isOurCommand(cmd, spec.arg):
					rest = append(rest, h)
				case cmd == want && !have:
					have = true
					rest = append(rest, h)
				default:
					stale++
				}
			}
			switch {
			case len(rest) == len(inner):
				kept = append(kept, e)
			case len(rest) > 0:
				entry["hooks"] = rest
				kept = append(kept, entry)
			}
		}
		if !have {
			entry := map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": want}},
			}
			if spec.matcher != "" {
				entry["matcher"] = spec.matcher
			}
			kept = append(kept, any(entry))
			if stale > 0 {
				ch.repointed++
				stale--
			} else {
				ch.added++
			}
		}
		ch.dropped += stale
		hooks[spec.event] = kept
	}
	return ch
}

// isOurCommand reports whether cmd is this dispatcher's hook for arg, whatever
// install wrote it: `<path>/claude-dispatcher hook <arg>`, the Nix-wrapped
// `.claude-dispatcher-wrapped`, or Windows' `claude-dispatcher.exe`. The path
// is everything before " hook ", so a directory with a space in it does not
// hide an entry.
func isOurCommand(cmd, arg string) bool {
	bin, ok := strings.CutSuffix(cmd, " hook "+arg)
	if !ok {
		return false
	}
	name := bin[strings.LastIndexAny(bin, `/\`)+1:]
	name = strings.TrimSuffix(name, ".exe")
	name = strings.TrimSuffix(strings.TrimPrefix(name, "."), "-wrapped")
	return name == "claude-dispatcher"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}
