// Package hookcmd is the receiving end of the global Claude Code lifecycle
// hook. Claude Code invokes `claude-dispatcher hook <event>` with JSON on
// stdin; we translate events into dispatch status transitions:
//
//	SessionStart                   -> working (binds session_id/transcript)
//	UserPromptSubmit               -> working
//	PostToolUse                    -> working, only to clear "blocked"
//	Stop                           -> needs-input (turn complete)
//	Notification:idle_prompt       -> needs-input
//	Notification:permission_prompt -> blocked
//	SessionEnd                     -> exited (unless already done)
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
	"io"
	"os"
	"path/filepath"

	"claude-dispatcher/internal/state"
)

type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
}

func Run(args []string) int {
	event := "unknown"
	if len(args) > 0 {
		event = args[0]
	}
	raw, _ := io.ReadAll(os.Stdin)
	var in hookInput
	json.Unmarshal(raw, &in)

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
	if apply(d, event, in) {
		state.Save(d)
	}
	return 0
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
	// SessionStart arriving from its repo. Records are sorted newest-first
	// within a status, so the most recent launch wins.
	if event == "SessionStart" && in.Cwd != "" {
		for _, d := range all {
			if d.Status == state.StatusLaunching && samePath(d.RepoPath, in.Cwd) {
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
	case "UserPromptSubmit":
		d.Status = state.StatusWorking
		d.StatusReason = "processing your prompt"
	case "PostToolUse":
		// A tool completing means any permission prompt was approved.
		if d.Status != state.StatusBlocked {
			return false
		}
		d.Status = state.StatusWorking
		d.StatusReason = "permission approved, working"
	case "Stop":
		d.Status = state.StatusNeedsInput
		d.StatusReason = "turn complete — waiting on you"
	case "Notification:idle_prompt":
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

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
