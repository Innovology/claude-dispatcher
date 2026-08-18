// Package state persists dispatch records and the lifecycle event log.
//
// Layout under ~/.local/state/claude-dispatcher/ (override with
// CLAUDE_DISPATCHER_STATE):
//
//	dispatches/<id>.json   one record per dispatch, atomically replaced
//	events.jsonl           append-only lifecycle event log
//
// The cockpit is a stateless viewer over this directory; the hook subcommand
// is its writer. tmux, not the cockpit, owns the session processes.
package state

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	StatusLaunching  Status = "launching"
	StatusWorking    Status = "working"
	StatusNeedsInput Status = "needs-input" // turn complete, waiting on the human
	StatusBlocked    Status = "blocked"     // waiting on a permission approval
	StatusDone       Status = "done"        // shipped ("done means live")
	StatusExited     Status = "exited"
)

// Priority orders statuses by how urgently they need the human's attention.
func (s Status) Priority() int {
	switch s {
	case StatusBlocked:
		return 0
	case StatusNeedsInput:
		return 1
	case StatusLaunching:
		return 2
	case StatusWorking:
		return 3
	case StatusExited:
		return 4
	case StatusDone:
		return 5
	}
	return 6
}

type Dispatch struct {
	ID       string `json:"id"`
	Feature  string `json:"feature"`
	Slug     string `json:"slug"`
	RepoPath string `json:"repo_path"`
	RepoName string `json:"repo_name"`
	Product  string `json:"product,omitempty"`
	Branch   string `json:"branch"`
	// WorktreePath is the dispatch's own checkout of Branch (a git worktree
	// under WorktreesDir); the claude session runs there so concurrent
	// dispatches never fight over the repo's working copy. RepoPath stays the
	// main repo — refs, PRs, and commit provenance are read from there.
	WorktreePath string `json:"worktree_path,omitempty"`
	Prompt       string `json:"prompt"`
	// Mode is the permission mode the session was dispatched in — one of
	// dispatch.Mode's values. It is on the record because a resumed session has
	// to reopen the way it went out and the transcript does not carry it, and
	// because every screen that says a dispatcher is unattended should be
	// reading the mode it was actually launched with. Empty on records written
	// before the mode was a choice: those ran in whatever claude defaulted to,
	// which is not the same fact as "auto".
	Mode string `json:"mode,omitempty"`
	// Model is the model the session was asked to run — one of
	// dispatch.Models(): "default" for "no --model passed", or a claude alias
	// like "opus". On the record for the same reason Mode is: Resume reopens
	// the session the way it went out. Empty on records written before the
	// model was a choice — those sessions ran whatever claude defaulted to,
	// which is the same behaviour as "default" but not the same fact.
	Model string `json:"model,omitempty"`
	// FanOut records that the prompt closed with the ultracode fan-out
	// sentence (see dispatch/fanout.go): the session was invited to spread the
	// work across multiple agents where the task warranted it. The sentence
	// itself lives in Prompt; this flag is what lets screens say so without
	// grepping the prompt for a keyword.
	FanOut         bool   `json:"fan_out,omitempty"`
	TmuxSession    string `json:"tmux_session"`
	SessionID      string `json:"session_id,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	// BaseSHA is the branch tip at launch; commits in BaseSHA..Branch were
	// produced under this dispatch. That provenance — not commit trailers —
	// is how work is attributed to dispatchers.
	BaseSHA      string     `json:"base_sha,omitempty"`
	Commits      []string   `json:"commits,omitempty"`
	PRNumber     int        `json:"pr_number,omitempty"`
	PRState      string     `json:"pr_state,omitempty"` // OPEN, MERGED, CLOSED
	PRURL        string     `json:"pr_url,omitempty"`
	PRMergedAt   *time.Time `json:"pr_merged_at,omitempty"`
	DeployedAt   *time.Time `json:"deployed_at,omitempty"`
	Status       Status     `json:"status"`
	StatusReason string     `json:"status_reason,omitempty"`
	// WaitingOnTasks records that the last Stop event carried in-flight
	// background tasks: the session is paused, not waiting on the human, and
	// will wake itself. Guards against a later idle_prompt notification (whose
	// payload has no task info) downgrading the status to needs-input.
	WaitingOnTasks bool `json:"waiting_on_tasks,omitempty"`
	// ParkedReason and ParkedAt are the human's shelf, not the machine's truth:
	// the session asked something they cannot answer right now, and they said
	// why. Parking is an annotation rather than a Status because Status belongs
	// to the lifecycle hooks — a "parked" status would be overwritten by the
	// next event the session emitted, and guarded everywhere one is applied.
	// The cockpit sets and clears the pair together; hookcmd clears it on
	// UserPromptSubmit, because a prompt reaching the session means the
	// question it was parked on got its answer.
	ParkedReason string     `json:"parked_reason,omitempty"`
	ParkedAt     *time.Time `json:"parked_at,omitempty"`
	// Subagents is the session's fan-out as the SubagentStart/SubagentStop
	// hooks reported it: the agents the session has spun out through its Agent
	// tool, each with the type name Claude Code gave it. Like parking it is an
	// annotation, never a Status — a fleet of subagents changes nothing about
	// whether the session is working or waiting. hookcmd owns the lifecycle:
	// cleared on SessionStart, stopped entries dropped on UserPromptSubmit
	// (each turn tells its own story), and any entry still live at a Stop with
	// no background tasks is swept, because a subagent cannot outlive the turn
	// unless it is one of those tasks.
	Subagents []Subagent `json:"subagents,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Parked reports whether the human has shelved this dispatch — see ParkedReason.
func (d *Dispatch) Parked() bool { return d.ParkedAt != nil || d.ParkedReason != "" }

// Subagent is one agent the session fanned out, as the hooks named it. The
// hooks are the source, not the transcript: transcript JSONL is best-effort
// preview only, and a count read from an internal format would be a guess
// wearing a number.
type Subagent struct {
	ID   string `json:"id"`
	Type string `json:"type,omitempty"` // e.g. "Explore", "general-purpose"
	// StartedAt/StoppedAt are stamped at hook receipt. A nil StoppedAt is a
	// subagent still running.
	StartedAt time.Time  `json:"started_at"`
	StoppedAt *time.Time `json:"stopped_at,omitempty"`
}

// maxSubagents bounds the annotation. The record is rewritten on every hook
// event and a runaway loop can spawn agents by the hundred; past the cap the
// oldest stopped entry makes room, so the live picture stays exact and only
// deep history is shed. The done count then reads low, which the cap accepts:
// a bounded record that undercounts a marathon beats an unbounded one.
const maxSubagents = 256

// SubagentStarted records a subagent spinning up. A restarted id is reset
// rather than duplicated. It reports whether anything changed.
func (d *Dispatch) SubagentStarted(id, typ string, at time.Time) bool {
	if id == "" {
		return false // cannot track what nothing names
	}
	for i := range d.Subagents {
		if d.Subagents[i].ID == id {
			d.Subagents[i] = Subagent{ID: id, Type: typ, StartedAt: at}
			return true
		}
	}
	if len(d.Subagents) >= maxSubagents && !d.shedStoppedSubagent() {
		return false // cap reached and everything is live; drop the event
	}
	d.Subagents = append(d.Subagents, Subagent{ID: id, Type: typ, StartedAt: at})
	return true
}

// SubagentStopped records a subagent finishing. A stop whose start was never
// seen (the hook landed mid-turn) still appends, already stopped, so the done
// count stays honest. It reports whether anything changed.
func (d *Dispatch) SubagentStopped(id, typ string, at time.Time) bool {
	if id == "" {
		return false
	}
	for i := range d.Subagents {
		if d.Subagents[i].ID == id {
			if d.Subagents[i].StoppedAt != nil {
				return false
			}
			d.Subagents[i].StoppedAt = &at
			return true
		}
	}
	if len(d.Subagents) >= maxSubagents && !d.shedStoppedSubagent() {
		return false
	}
	d.Subagents = append(d.Subagents, Subagent{ID: id, Type: typ, StartedAt: at, StoppedAt: &at})
	return true
}

// SweepSubagents marks every still-live subagent stopped. hookcmd calls it on
// a Stop with no background tasks: the turn is over and nothing is in flight,
// so an entry still claiming to run is a stop event that never arrived.
func (d *Dispatch) SweepSubagents(at time.Time) bool {
	changed := false
	for i := range d.Subagents {
		if d.Subagents[i].StoppedAt == nil {
			t := at
			d.Subagents[i].StoppedAt = &t
			changed = true
		}
	}
	return changed
}

// DropStoppedSubagents clears the finished entries; live ones (background
// agents crossing the turn boundary) stay. hookcmd calls it on
// UserPromptSubmit so each turn reports its own fan-out.
func (d *Dispatch) DropStoppedSubagents() bool {
	kept := d.Subagents[:0]
	for _, a := range d.Subagents {
		if a.StoppedAt == nil {
			kept = append(kept, a)
		}
	}
	changed := len(kept) != len(d.Subagents)
	d.Subagents = kept
	if len(d.Subagents) == 0 {
		d.Subagents = nil
	}
	return changed
}

// shedStoppedSubagent drops the oldest stopped entry to make room at the cap.
func (d *Dispatch) shedStoppedSubagent() bool {
	for i := range d.Subagents {
		if d.Subagents[i].StoppedAt != nil {
			d.Subagents = append(d.Subagents[:i], d.Subagents[i+1:]...)
			return true
		}
	}
	return false
}

// SubagentsLive counts the fan-out still running.
func (d *Dispatch) SubagentsLive() int {
	n := 0
	for _, a := range d.Subagents {
		if a.StoppedAt == nil {
			n++
		}
	}
	return n
}

// SubagentsDone counts the fan-out that has finished this turn.
func (d *Dispatch) SubagentsDone() int {
	n := 0
	for _, a := range d.Subagents {
		if a.StoppedAt != nil {
			n++
		}
	}
	return n
}

func Dir() string {
	if d := os.Getenv("CLAUDE_DISPATCHER_STATE"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "claude-dispatcher")
}

func DispatchesDir() string { return filepath.Join(Dir(), "dispatches") }

// WorktreesDir holds per-dispatch git worktrees, worktrees/<repo>/<slug>.
func WorktreesDir() string { return filepath.Join(Dir(), "worktrees") }

func EnsureDirs() error { return os.MkdirAll(DispatchesDir(), 0o755) }

func NewID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Save atomically writes the record (tmp file + rename).
func Save(d *Dispatch) error {
	if err := EnsureDirs(); err != nil {
		return err
	}
	d.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(DispatchesDir(), d.ID+".json")
	// A per-call temp name, not final+".tmp": concurrent savers sharing one
	// temp path can interleave write and rename and install a torn file. With
	// unique names the loser only overwrites the winner whole — last-writer-
	// wins, never half-of-each. (Subagent hook events made concurrent savers
	// the routine case; see Lock.)
	tmp, err := os.CreateTemp(DispatchesDir(), d.ID+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), final)
}

// LoadAll returns every dispatch record, most urgent first, most recent within
// equal urgency.
func LoadAll() []*Dispatch {
	entries, err := os.ReadDir(DispatchesDir())
	if err != nil {
		return nil
	}
	var out []*Dispatch
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(DispatchesDir(), e.Name()))
		if err != nil {
			continue
		}
		var d Dispatch
		if json.Unmarshal(b, &d) != nil || d.ID == "" {
			continue
		}
		out = append(out, &d)
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := out[i].Status.Priority(), out[j].Status.Priority()
		if pi != pj {
			return pi < pj
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

type Event struct {
	Time         time.Time `json:"time"`
	Event        string    `json:"event"`
	DispatcherID string    `json:"dispatcher_id,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	Cwd          string    `json:"cwd,omitempty"`
}

// AppendEvent appends one line to events.jsonl; failures are swallowed because
// event logging must never disturb a live session.
func AppendEvent(ev Event) {
	if err := EnsureDirs(); err != nil {
		return
	}
	ev.Time = time.Now()
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(Dir(), "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(b, '\n'))
}

// LoadEvents reads events.jsonl, oldest first (the order it was appended in).
// A missing log and a malformed line are both non-events: the writer swallows
// its own failures so it can never disturb a session, which means a truncated
// last line is normal and must not cost the reader the rest of the log.
//
// Callers that need the log grouped or ordered per dispatcher should do that
// themselves — this returns the file as written, one Event per line.
func LoadEvents() []Event {
	f, err := os.Open(filepath.Join(Dir(), "events.jsonl"))
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var out []Event
	sc := bufio.NewScanner(f)
	// Lines carry a cwd, so the default 64K token limit is generous already;
	// raise the cap anyway rather than have one long path end the scan.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		out = append(out, ev)
	}
	return out
}
