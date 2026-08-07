// Package hookcmd is the receiving end of the global Claude Code lifecycle
// hook. Claude Code invokes `claude-dispatcher hook <event>` with JSON on
// stdin; we translate events into dispatch status transitions:
//
//	SessionStart                   -> working (binds session_id/transcript)
//	UserPromptSubmit               -> working
//	PostToolUse                    -> working, only to clear "blocked"
//	Stop                           -> needs-input (turn complete), or working
//	                                  if background tasks are still in flight
//	Notification:idle_prompt       -> needs-input (unless waiting on tasks)
//	Notification:permission_prompt -> blocked
//	SessionEnd                     -> exited (unless already done)
//
// A Stop with a non-empty background_tasks payload means the session is
// paused waiting for background work to wake it, not waiting on the human;
// the idle_prompt payload carries no task info, so the Stop's verdict is
// persisted (WaitingOnTasks) and idle_prompt defers to it.
//
// Events are attributed to a dispatch by, in order: the CLAUDE_DISPATCHER_ID
// env var (set at launch, inherited through tmux -> claude -> hook), the
// session_id, or — for SessionStart only — the newest still-launching
// dispatch for the event's cwd. Unattributed events (sessions started outside
// the cockpit) are logged to events.jsonl and otherwise ignored for now.
//
// This process must never disturb the Claude session: it always exits 0.
package hookcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"claude-dispatcher/internal/state"
)

type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	// Stop/SubagentStop only; item shape is Claude Code's business — only the
	// count matters here.
	BackgroundTasks []json.RawMessage `json:"background_tasks"`
}

func Run(args []string) int {
	event := "unknown"
	if len(args) > 0 {
		event = args[0]
	}
	raw, _ := io.ReadAll(os.Stdin)
	var in hookInput
	_ = json.Unmarshal(raw, &in)

	dispatcherID := os.Getenv("CLAUDE_DISPATCHER_ID")
	if event != "PostToolUse" { // PostToolUse is too chatty for the log
		state.AppendEvent(state.Event{
			Event:        event,
			DispatcherID: dispatcherID,
			SessionID:    in.SessionID,
			Cwd:          in.Cwd,
		})
	}

	d := resolve(dispatcherID, event, in)
	if d == nil {
		return 0
	}
	changed := apply(d, event, in)
	if event == "Stop" || event == "SessionEnd" {
		changed = refreshCommits(d) || changed
	}
	if changed {
		_ = state.Save(d)
	}
	return 0
}

// refreshCommits records the SHAs produced on the feature branch since
// launch. Provenance ("this dispatch made these commits") is the attribution
// signal — no trailers or markers in the repo's public history.
func refreshCommits(d *state.Dispatch) bool {
	if d.BaseSHA == "" || d.Branch == "" {
		return false
	}
	out, err := exec.Command("git", "-C", d.RepoPath, "rev-list",
		d.BaseSHA+"..refs/heads/"+d.Branch).Output()
	if err != nil {
		return false
	}
	shas := strings.Fields(string(out))
	if slices.Equal(shas, d.Commits) {
		return false
	}
	d.Commits = shas
	return true
}

func resolve(dispatcherID, event string, in hookInput) *state.Dispatch {
	all := state.LoadAll()
	if dispatcherID != "" {
		for _, d := range all {
			if d.ID == dispatcherID {
				return d
			}
		}
	}
	if in.SessionID != "" {
		for _, d := range all {
			if d.SessionID == in.SessionID {
				return d
			}
		}
	}
	// A freshly launched dispatch has no session id yet; bind the first
	// SessionStart arriving from its worktree (or repo, for records from
	// before per-dispatch worktrees). Records are sorted newest-first within
	// a status, so the most recent launch wins.
	if event == "SessionStart" && in.Cwd != "" {
		for _, d := range all {
			if d.Status != state.StatusLaunching {
				continue
			}
			if d.WorktreePath != "" && samePath(d.WorktreePath, in.Cwd) {
				return d
			}
			if d.WorktreePath == "" && samePath(d.RepoPath, in.Cwd) {
				return d
			}
		}
	}
	return nil
}

// apply mutates the dispatch for the event; it reports whether anything
// changed and a save is warranted.
func apply(d *state.Dispatch, event string, in hookInput) bool {
	if d.Status == state.StatusDone {
		return false // done means live; nothing downgrades it
	}
	switch event {
	case "SessionStart":
		if in.SessionID != "" {
			d.SessionID = in.SessionID
		}
		if in.TranscriptPath != "" {
			d.TranscriptPath = in.TranscriptPath
		}
		d.Status = state.StatusWorking
		d.StatusReason = "session started"
		d.WaitingOnTasks = false
	case "UserPromptSubmit":
		d.Status = state.StatusWorking
		d.StatusReason = "processing your prompt"
		d.WaitingOnTasks = false
	case "PostToolUse":
		// A tool completing means any permission prompt was approved.
		if d.Status != state.StatusBlocked {
			return false
		}
		d.Status = state.StatusWorking
		d.StatusReason = "permission approved, working"
	case "Stop":
		d.WaitingOnTasks = len(in.BackgroundTasks) > 0
		if d.WaitingOnTasks {
			d.Status = state.StatusWorking
			d.StatusReason = fmt.Sprintf("waiting on %d background %s",
				len(in.BackgroundTasks), plural(len(in.BackgroundTasks), "task"))
			break
		}
		d.Status = state.StatusNeedsInput
		d.StatusReason = "turn complete — waiting on you"
	case "Notification:idle_prompt":
		if d.WaitingOnTasks {
			return false // paused on background work, not on the human
		}
		d.Status = state.StatusNeedsInput
		d.StatusReason = "waiting for your next prompt"
	case "Notification:permission_prompt":
		d.Status = state.StatusBlocked
		d.StatusReason = "waiting on a permission approval"
	case "SessionEnd":
		d.Status = state.StatusExited
		d.StatusReason = "session ended"
	default:
		return false
	}
	return true
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
