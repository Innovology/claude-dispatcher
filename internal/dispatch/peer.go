package dispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PeerNames is the name each live Claude session on the machine goes by among
// the others — what one session messages another by — keyed by its session id.
//
// A steward coordinates dispatchers that are still working, and `reply` cannot
// reach those: it types at a prompt, and a working session has none. Claude
// Code's own session messaging can, by name, and its registry
// (`<config dir>/sessions/<pid>.json`, the file adoption already reads) holds
// the name beside the session id, which a dispatch record has. It is read under
// ~/.claude and every further config dir named — an account's sessions are
// registered under their own (ADR 0022).
//
// The registry is Claude Code's, like the transcript: best-effort, never status
// truth. A file whose process is gone is skipped where the kernel can be asked,
// because a dead session's name would send a message nowhere.
func PeerNames(configDirs ...string) map[string]string {
	home, _ := os.UserHomeDir()
	dirs := map[string]bool{filepath.Join(home, ".claude"): true}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		dirs[d] = true
	}
	for _, d := range configDirs {
		if d != "" {
			dirs[d] = true
		}
	}
	_, procErr := os.Stat(procRoot)
	out := map[string]string{}
	for dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var reg struct {
				PID       int    `json:"pid"`
				SessionID string `json:"sessionId"`
				Name      string `json:"name"`
				ProcStart string `json:"procStart"`
			}
			if json.Unmarshal(raw, &reg) != nil || reg.SessionID == "" || strings.TrimSpace(reg.Name) == "" {
				continue
			}
			if procErr == nil {
				if _, err := os.Stat(filepath.Join(procRoot, strconv.Itoa(reg.PID))); err != nil {
					continue // the process is gone
				}
				if started := procStartTime(reg.PID); started != "" && reg.ProcStart != "" && started != reg.ProcStart {
					continue // the pid is someone else's now
				}
			}
			out[reg.SessionID] = reg.Name
		}
	}
	return out
}
