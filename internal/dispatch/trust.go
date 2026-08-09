package dispatch

// trust.go carries a repo's Claude Code trust decision across to the worktree a
// dispatch runs in.
//
// Every dispatch materialises a brand-new directory, and Claude Code asks "do
// you trust this folder?" the first time it starts in one. Nothing answers it —
// the session is unattended by design — so claude sits on the prompt, the work
// never begins, and the record still reports "working" because the lifecycle
// hook fired on session start. Every dispatch silently did nothing.
//
// Trust is inherited, never invented: the worktree is marked trusted only when
// the repo it was cut from already is. If the user has not vouched for the repo
// then neither do we, and they answer the prompt once as they would anywhere
// else. Failing to write is never fatal — a dispatch that shows a trust prompt
// is worse than one that does not, but far better than no dispatch at all.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// claudeConfigPath is Claude Code's own config, where project trust lives.
func claudeConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude.json")
}

// InheritTrust marks worktree as trusted when repo already is. It reports
// whether the worktree will start without a trust prompt, so the caller can say
// so rather than leaving the user to wonder why a session is idle.
func InheritTrust(repoPath, worktree string) bool {
	path := claudeConfigPath()
	if path == "" || repoPath == "" || worktree == "" {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	// Decoded into a generic map on purpose: this file is Claude Code's, not
	// ours, and it carries far more than trust. Round-tripping every key we do
	// not understand is the only safe way to edit it.
	var cfg map[string]json.RawMessage
	if json.Unmarshal(raw, &cfg) != nil {
		return false
	}
	var projects map[string]map[string]any
	if p, ok := cfg["projects"]; ok {
		if json.Unmarshal(p, &projects) != nil {
			return false
		}
	}
	if projects == nil {
		projects = map[string]map[string]any{}
	}

	if trusted, _ := projects[worktree]["hasTrustDialogAccepted"].(bool); trusted {
		return true // already inherited by an earlier dispatch of this feature
	}
	if trusted, _ := projects[repoPath]["hasTrustDialogAccepted"].(bool); !trusted {
		return false // the repo itself is not trusted — do not invent it
	}

	// Clone the repo's own project entry rather than inventing one. Claude Code
	// ignores a half-populated entry and asks anyway, and the repo's settings —
	// its allowed tools, its MCP servers — are exactly what this worktree should
	// run with, since it is the same codebase in a different directory.
	entry := map[string]any{}
	for k, v := range projects[repoPath] {
		entry[k] = v
	}
	for k, v := range projects[worktree] {
		entry[k] = v // anything already recorded for the worktree wins
	}
	entry["hasTrustDialogAccepted"] = true
	// Onboarding is per-directory and has not happened here; claiming it has is
	// what made Claude Code fall back to asking.
	delete(entry, "hasCompletedProjectOnboarding")
	entry["projectOnboardingSeenCount"] = 0
	projects[worktree] = entry

	encoded, err := json.Marshal(projects)
	if err != nil {
		return false
	}
	cfg["projects"] = encoded
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false
	}
	return writeAtomic(path, out) == nil
}

// writeAtomic replaces path in one rename, so a crash mid-write cannot leave
// Claude Code with a truncated config.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".claude-dispatcher-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp, fi.Mode())
	}
	return os.Rename(tmp, path)
}
