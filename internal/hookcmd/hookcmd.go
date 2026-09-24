// Package hookcmd is the receiving end of the global Claude Code lifecycle
// hook. Claude Code invokes `claude-dispatcher hook <event>` with JSON on
// stdin; we translate events into dispatch status transitions:
//
//	SessionStart                   -> working (binds session_id/transcript)
//	UserPromptSubmit               -> working
//	PostToolUse                    -> working, only to clear "blocked"
//	Stop                           -> needs-input (turn complete), or working
//	                                  if background tasks are still in flight
//	StopFailure                    -> needs-input, with the API error that
//	                                  ended the turn recorded as its Failure
//	Notification:idle_prompt       -> needs-input (unless waiting on tasks)
//	Notification:permission_prompt -> blocked
//	SessionEnd                     -> exited (unless already done)
//	SubagentStart/SubagentStop     -> no status change; the fan-out is an
//	                                  annotation on the record (state.Subagent)
//
// A Stop with a non-empty background_tasks payload means the session is
// paused waiting for background work to wake it, not waiting on the human;
// the idle_prompt payload carries no task info, so the Stop's verdict is
// persisted (WaitingOnTasks) and idle_prompt defers to it.
//
// A done record is terminal for every event but the two that prove its session
// is still going — see reopensDone.
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
	"time"

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
	// SubagentStart/SubagentStop carry which agent the event is about; other
	// events fired from within a subagent carry them too, and are handled the
	// same as from the main thread.
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
	// StopFailure only: Claude Code's category for the API error that ended
	// the turn, and whatever detail it sent with it. error_details is decoded
	// loosely — its shape is Claude Code's business, and a detail we cannot
	// read must not cost us the category beside it.
	Error        string          `json:"error"`
	ErrorDetails json.RawMessage `json:"error_details"`
	// Stop/StopFailure: the text of the turn's last message, whole.
	LastAssistantMessage string `json:"last_assistant_message"`
}

// detail renders error_details as one line: a JSON string is unquoted, any
// other shape is kept as the JSON it arrived as.
func (in hookInput) detail() string {
	raw := strings.TrimSpace(string(in.ErrorDetails))
	if raw == "" || raw == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(in.ErrorDetails, &s) == nil {
		raw = s
	}
	raw = strings.Join(strings.Fields(raw), " ")
	if len(raw) > 300 {
		raw = raw[:300] + "…"
	}
	return raw
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
			Reason:       in.Error, // StopFailure's category; empty for the rest
		})
	}

	// A fan-out fires subagent events from parallel agents, so two hookcmds
	// routinely race the same record; unserialised, the loser's whole-record
	// save wins and the winner's event is gone. The lock is best-effort — a
	// lock that cannot be taken must never stall the session.
	release := state.Lock()
	defer release()

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

// reopensDone names the two events that outrank "done means live".
//
// internal/track flips a record to done the moment its PR merges, and a
// dispatcher told to open and merge its own PR and keep working routinely
// merges mid-run — so "done" can land on a session that is still going. When
// that happened, this guard used to swallow everything the session said
// afterwards, including the permission prompt it was stuck on: the record
// froze at done, and since the triage table only admits blocked/needs/review/
// working rows, a live dispatcher waiting on an approval vanished from the
// cockpit entirely, which then showed the empty-fleet dispatch form.
//
// A permission prompt and a human's new prompt are proof the session is not
// finished, and both mean it wants something — and anything that wants
// something belongs on the table. Every other event (a Stop, an idle prompt, a
// session ending) is what a shipped feature looks like and still cannot
// downgrade done. track re-flips it once the turn ends; see track.midWork.
func reopensDone(event string) bool {
	return event == "Notification:permission_prompt" || event == "UserPromptSubmit"
}

// apply mutates the dispatch for the event; it reports whether anything
// changed and a save is warranted.
func apply(d *state.Dispatch, event string, in hookInput) bool {
	// The fan-out annotation is bookkeeping, not status, so it applies even
	// where the guard below holds a done record still — the same way
	// refreshCommits keeps attributing commits to a shipped record.
	changed := applyFanOut(d, event, in)
	if d.Status == state.StatusDone && !reopensDone(event) {
		return changed // done means live; only proof of life downgrades it
	}
	wasWaiting := d.Waiting()
	statusChanged := applyStatus(d, event, in)
	return stampWait(d, event, wasWaiting) || statusChanged || changed
}

// stampWait keeps WaitingSince: set the moment a session stops to wait on
// someone, and cleared while it works. Every Stop and StopFailure is a new wait
// even from needs-input — a turn that ended again has said something new —
// while the idle prompt that trails a stop a minute later is the same wait.
// The steward's note is cleared with the wait it was written in.
func stampWait(d *state.Dispatch, event string, wasWaiting bool) bool {
	switch {
	case !d.Waiting():
		if d.WaitingSince == nil && d.StewardNote == "" {
			return false
		}
		d.WaitingSince = nil
		d.StewardNote, d.StewardNoteAt = "", nil
		return true
	case !wasWaiting || event == "Stop" || event == "StopFailure":
		now := time.Now()
		d.WaitingSince = &now
		return true
	}
	return false
}

// applyFanOut keeps the Subagents annotation current. Every path a session
// can end a turn — or end — settles the fan-out, because a subagent cannot
// outlive the turn (unless it is a background task) and cannot outlive the
// session at all; an entry left claiming to run would be a ghost in
// miniature, a row saying "3 live" forever with nothing behind it. The two
// non-hook ways a session dies (a cockpit kill, a tmux taken down with the
// machine) sweep at their own retirement sites: cockpit killCmd and
// dispatch.ReconcileSessions.
func applyFanOut(d *state.Dispatch, event string, in hookInput) bool {
	switch event {
	case "SubagentStart":
		return d.SubagentStarted(in.AgentID, in.AgentType, time.Now())
	case "SubagentStop":
		return d.SubagentStopped(in.AgentID, in.AgentType, time.Now())
	case "SessionStart":
		// A new session starts with no fan-out: no subagent survives the
		// session that spun it out.
		if len(d.Subagents) == 0 {
			return false
		}
		d.Subagents = nil
		return true
	case "UserPromptSubmit":
		// Each turn tells its own fan-out story: the finished subagents of
		// the last turn go, background ones still running carry over.
		return d.DropStoppedSubagents()
	case "Stop":
		if len(in.BackgroundTasks) > 0 {
			return false // background agents legitimately outlive the turn
		}
		// The turn is over and nothing is in flight, so a subagent still
		// marked live is a SubagentStop that never arrived, not a subagent
		// still running.
		return d.SweepSubagents(time.Now())
	case "SessionEnd":
		// Background tasks do not survive their session either: whatever the
		// last Stop said, the process everything ran in is gone.
		return d.SweepSubagents(time.Now())
	}
	return false
}

// applyStatus is the status state machine; it reports whether the record
// changed. The fan-out never appears here — what a session does with its
// subagents changes nothing about whether it is working or waiting.
func applyStatus(d *state.Dispatch, event string, in hookInput) bool {
	switch event {
	case "SessionStart":
		d.Failure = nil // a new session has not failed at anything yet
		d.Said = ""
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
		d.Said = "" // whatever it last asked has just been answered
		// A prompt reaching the session answers the question it was parked on:
		// the park said "I cannot answer that right now", and someone just did.
		// No other event clears the shelf — a Stop, an idle prompt or a session
		// dying with the machine are all things a parked dispatcher is allowed
		// to do while it waits.
		d.ParkedReason, d.ParkedAt = "", nil
	case "PostToolUse":
		// A tool completing means any permission prompt was approved.
		if d.Status != state.StatusBlocked {
			return false
		}
		d.Status = state.StatusWorking
		d.StatusReason = "permission approved, working"
	case "Stop":
		// A turn that completed is the proof whatever last failed is past it,
		// and the only thing that resets the retry count.
		d.Failure = nil
		d.SetSaid(in.LastAssistantMessage)
		d.WaitingOnTasks = len(in.BackgroundTasks) > 0
		if d.WaitingOnTasks {
			d.Status = state.StatusWorking
			d.StatusReason = fmt.Sprintf("waiting on %d background %s",
				len(in.BackgroundTasks), plural(len(in.BackgroundTasks), "task"))
			break
		}
		d.Status = state.StatusNeedsInput
		d.StatusReason = "turn complete — waiting on you"
	case "StopFailure":
		// Fired instead of Stop, so it is the only word the session gets out:
		// without it the record went on saying "working". The retry count
		// carries over from an earlier failure in the same stretch — the retry
		// that led here is exactly what it is counting.
		f := &state.Failure{Error: in.Error, Detail: in.detail(), At: time.Now()}
		if f.Error == "" {
			f.Error = "unknown"
		}
		if d.Failure != nil {
			f.Retries, f.RetriedAt = d.Failure.Retries, d.Failure.RetriedAt
		}
		d.Failure = f
		d.SetSaid(in.LastAssistantMessage)
		d.WaitingOnTasks = false
		d.Status = state.StatusNeedsInput
		d.StatusReason = "stopped on an API error: " + strings.ReplaceAll(f.Error, "_", " ")
	case "Notification:idle_prompt":
		if d.WaitingOnTasks {
			return false // paused on background work, not on the human
		}
		if d.Failure != nil && d.Status == state.StatusNeedsInput {
			// The idle prompt a failed turn leaves behind a minute later. It
			// is the same stop, and the failure is the better account of it.
			return false
		}
		d.Status = state.StatusNeedsInput
		d.StatusReason = "waiting for your next prompt"
	case "Notification:permission_prompt":
		d.Status = state.StatusBlocked
		d.StatusReason = "waiting on a permission approval"
	case "SessionEnd":
		d.Failure = nil // nothing left to retry
		d.Stop(state.StatusExited, "session ended", time.Now())
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
