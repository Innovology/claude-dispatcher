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
	return InheritTrustFor("", repoPath, worktree)
}

// InheritTrustFor is InheritTrust for a session running under another
// account's config directory ("" is the human's own). Trust lives in that
// directory's .claude.json, so a worktree trusted for the default login is a
// stranger to every other one, and a second subscription's session would sit
// on the trust dialog with nobody to answer it.
//
// The repo's trust is read from the account's own file first and then from
// the default one. Trusting a folder is the human's judgement about the code,
// not about which subscription pays for reading it, so a repo they vouched for
// under their own login is a repo they vouched for — what is still never done
// is trusting a repo nobody vouched for under any login.
func InheritTrustFor(configDir, repoPath, worktree string) bool {
	path := claudeConfigPath()
	if configDir != "" {
		path = filepath.Join(configDir, ".claude.json")
	}
	if path == "" || repoPath == "" || worktree == "" {
		return false
	}
	cfg, projects, ok := readClaudeProjects(path)
	if !ok {
		return false
	}

	if trusted, _ := projects[worktree]["hasTrustDialogAccepted"].(bool); trusted {
		return true // already inherited by an earlier dispatch of this feature
	}
	source := projects[repoPath]
	if trusted, _ := source["hasTrustDialogAccepted"].(bool); !trusted && configDir != "" {
		if _, own, ok := readClaudeProjects(claudeConfigPath()); ok {
			source = own[repoPath]
		}
	}
	if trusted, _ := source["hasTrustDialogAccepted"].(bool); !trusted {
		return false // the repo itself is not trusted — do not invent it
	}

	// Clone the repo's own project entry rather than inventing one. Claude Code
	// ignores a half-populated entry and asks anyway, and the repo's settings —
	// its allowed tools, its MCP servers — are exactly what this worktree should
	// run with, since it is the same codebase in a different directory.
	entry := map[string]any{}
	for k, v := range source {
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

// TrustOwnDir marks a directory the dispatcher itself created as trusted, so a
// session started there does not stop on Claude Code's trust dialog before it
// reads its first word. It is the one place trust is written rather than
// inherited, and it is only for a directory under the dispatcher's own state
// (the steward's): InheritTrust will not invent trust for a human's repo, and
// nothing should — but a folder we made, holding nothing but our own brief, has
// no one else to ask.
func TrustOwnDir(dir string) bool {
	path := claudeConfigPath()
	if path == "" || dir == "" {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
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
	if trusted, _ := projects[dir]["hasTrustDialogAccepted"].(bool); trusted {
		return true
	}
	entry := projects[dir]
	if entry == nil {
		entry = map[string]any{}
	}
	entry["hasTrustDialogAccepted"] = true
	projects[dir] = entry
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

// readClaudeProjects reads one Claude Code config file as a generic map — this
// file is Claude Code's, not ours, and it carries far more than trust, so
// round-tripping every key we do not understand is the only safe way to edit
// it — along with its projects table decoded. A missing projects table is an
// empty one; an unreadable or malformed file is not ok.
func readClaudeProjects(path string) (map[string]json.RawMessage, map[string]map[string]any, bool) {
	if path == "" {
		return nil, nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	var cfg map[string]json.RawMessage
	if json.Unmarshal(raw, &cfg) != nil {
		return nil, nil, false
	}
	var projects map[string]map[string]any
	if p, ok := cfg["projects"]; ok {
		if json.Unmarshal(p, &projects) != nil {
			return nil, nil, false
		}
	}
	if projects == nil {
		projects = map[string]map[string]any{}
	}
	return cfg, projects, true
}
