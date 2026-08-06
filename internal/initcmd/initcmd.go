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
		{event: "Notification", matcher: "idle_prompt", arg: "Notification:idle_prompt"},
		{event: "Notification", matcher: "permission_prompt", arg: "Notification:permission_prompt"},
	}
}

const hookMarker = "claude-dispatcher hook"

func installHook() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if strings.Contains(exe, "go-build") {
		fmt.Println("\n! running via `go run` — install a real binary first (make install), then re-run init,")
		fmt.Println("  otherwise the hook would point at a temporary build path.")
		return nil
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
	if added == 0 {
		fmt.Println("✓ lifecycle hook already installed in", settingsPath)
		return nil
	}
	root["hooks"] = hooks

	fmt.Printf("\nAbout to add %d hook entries to %s\n", added, settingsPath)
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

func claudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}
