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
	ID             string `json:"id"`
	Feature        string `json:"feature"`
	Slug           string `json:"slug"`
	RepoPath       string `json:"repo_path"`
	RepoName       string `json:"repo_name"`
	Product        string `json:"product,omitempty"`
	Branch         string `json:"branch"`
	Prompt         string `json:"prompt"`
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
	WaitingOnTasks bool      `json:"waiting_on_tasks,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func Dir() string {
	if d := os.Getenv("CLAUDE_DISPATCHER_STATE"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "claude-dispatcher")
}

func DispatchesDir() string { return filepath.Join(Dir(), "dispatches") }

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
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
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
