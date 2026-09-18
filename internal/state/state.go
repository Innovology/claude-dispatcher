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
	// Root is the ref Branch was cut from, short — "origin/main". It is the
	// answer to "what is this work on top of", which nothing else on the record
	// gives: BaseSHA is the commit, and a commit does not say which branch it
	// was the tip of. Empty when the branch already existed and this dispatch
	// picked it up rather than cutting it, and on records written before a
	// dispatch recorded where it started.
	Root string `json:"root,omitempty"`
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
	FanOut      bool   `json:"fan_out,omitempty"`
	TmuxSession string `json:"tmux_session"`
	// TmuxSocket is the supervisor server the session lives on — a repo names
	// one to keep its own toolchain (see repos.Repo.Socket). Empty is the
	// default server, which is every dispatch made before this existed.
	//
	// It is on the record rather than worked out from the repo's config at the
	// time of asking, for the reason Root and Mode are: config changes, and a
	// session looked for on the wrong server answers "no such session" with
	// complete confidence. Together with TmuxSession it is the whole address.
	TmuxSocket string `json:"tmux_socket,omitempty"`
	// EnvCommand is the command prefix the tmux client was run under, so that
	// the session sees the repo's own binaries — "nix develop --command" for a
	// flake repo, empty for everywhere else.
	//
	// Recorded for the same reason: a dispatch launched before nix was on the
	// machine, resumed after it arrived, would otherwise come back in a
	// different world than it went out in — and the record is the only account
	// of which world that was.
	EnvCommand     string `json:"env_command,omitempty"`
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
	// SessionStartedAt is the supervisor accepting the session: new-session
	// returned, so at that instant the session existed. It is what makes a
	// launching record's missing session evidence. Without it, a launching
	// record is in the window where its session may not exist yet, and absence
	// proves nothing; with it, the session was there and has gone before claude
	// fired a single hook, which is a launch that died — a pane whose command
	// could not run — and nothing else will ever report it. Stamped by Launch
	// and Resume after the supervisor says yes, never before.
	SessionStartedAt *time.Time `json:"session_started_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	// FinishedAt is the instant this dispatcher's status first said it was over
	// — stamped by Stop, at the transition, and nowhere else. UpdatedAt cannot
	// answer that question: Save stamps it on every write, and a finished record
	// goes on being written for as long as it exists (the tracker catching a
	// merge, a reconcile marking a ghost exited, a PR field catching up), so a
	// dispatcher whose last act was a week ago routinely carries an UpdatedAt
	// from this morning.
	//
	// It is also what tells a dispatcher that finished under this build from one
	// that finished before it existed: only the former is held on the triage
	// table (see DismissedAt), so shipping this cannot flush a year of history
	// onto the fleet.
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// DismissedAt is the human taking a finished dispatcher off the triage
	// table. Like parking it is an annotation, never a Status: it says the
	// finish has been read, not that anything about the work changed. Until it
	// is set, a dispatcher that finished under this build keeps its row — a
	// dispatch that ends is a thing that happened, and a table that clears
	// itself is a table that never told anyone.
	DismissedAt *time.Time `json:"dismissed_at,omitempty"`
}

// Parked reports whether the human has shelved this dispatch — see ParkedReason.
func (d *Dispatch) Parked() bool { return d.ParkedAt != nil || d.ParkedReason != "" }

// Finished reports whether this dispatcher's session is over for good.
func (d *Dispatch) Finished() bool {
	return d.Status == StatusDone || d.Status == StatusExited
}

// Held reports whether the triage table should still be carrying this finished
// dispatcher: it ended while this build was the one running (FinishedAt), and
// nobody has said they have seen it (DismissedAt).
func (d *Dispatch) Held() bool {
	return d.Finished() && d.FinishedAt != nil && d.DismissedAt == nil
}

// Stop moves a dispatcher into a finished status and stamps when it got there.
//
// Every place that ends a dispatch goes through here — the SessionEnd hook, the
// ghost sweep, the cockpit's kill and its two ship keys, the tracker's deploy
// flip — because FinishedAt has to mean "the moment it stopped" and only the
// transition knows that. A record already finished keeps the stamp it has: the
// tracker re-writing a merged PR's fields months later is not a second ending.
func (d *Dispatch) Stop(status Status, reason string, now time.Time) {
	was := d.Finished()
	d.Status, d.StatusReason = status, reason
	if !was && d.Finished() && d.FinishedAt == nil {
		d.FinishedAt = &now
	}
}

// Dismiss takes a finished dispatcher off the triage table.
func (d *Dispatch) Dismiss(now time.Time) { d.DismissedAt = &now }

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

// PromptsDir holds the prompt each dispatch was launched with, one file per
// dispatch. The file is the transport, not a copy: the launch command reads it
// rather than carrying the prompt inside itself, because the command is one
// argument to the supervisor and every supervisor caps that (tmux at 16KB, a
// Windows console at 8191 characters) — see dispatch.launchCommand.
func PromptsDir() string { return filepath.Join(Dir(), "prompts") }

// PromptPath is where a dispatch's prompt file lives. name is the dispatch id,
// optionally suffixed for a prompt that is not the launch one (a resume's
// opening message), so a resume can never overwrite the record of what the
// dispatch was originally sent.
func PromptPath(name string) string { return filepath.Join(PromptsDir(), name+".txt") }

// WritePrompt puts prompt on disk and returns the path the launch command
// should read it from. Unlike AppendEvent this does NOT swallow its error: the
// prompt is the whole of what the dispatcher was asked to do, and a session
// started against a file that is not there would open on an empty prompt and
// sit waiting, which is worse than not starting at all.
func WritePrompt(name, prompt string) (string, error) {
	if err := os.MkdirAll(PromptsDir(), 0o755); err != nil {
		return "", err
	}
	path := PromptPath(name)
	if err := os.WriteFile(path, []byte(prompt), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

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
	// An invariant, not a guess: a record whose status is not a finished one has
	// not finished, so it can carry neither the instant it stopped nor the
	// human's reading of that. This is what clears both when a dispatcher is
	// resumed or reopened — the next event to save it puts it back in the live
	// table with no stale ending attached.
	if !d.Finished() {
		d.FinishedAt, d.DismissedAt = nil, nil
	}
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

	// Feature, Repo and Reason belong to the dispatch audit (the Dispatch*
	// events below), which is the only writer that has no dispatcher id to
	// offer: a launch that fails before the record is written has nothing else
	// to name itself by. Reason is why it failed, verbatim.
	Feature string `json:"feature,omitempty"`
	Repo    string `json:"repo,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// The dispatch audit's event names. Every attempt to dispatch writes one of
// these, whether or not it produces a record, because a launch that fails
// before state.Save leaves nothing else behind — no record, no worktree, no
// session — and "it disappeared" was the whole account the human got.
//
// They are deliberately not lifecycle events: nothing derives status from them,
// and velDwellState leaves them unbilled, so they can be added to a log that
// other readers already walk without changing a single figure.
const (
	// EventDispatchAsked is written before anything is created, so the log
	// carries the ask even if the process dies in the middle of serving it.
	EventDispatchAsked = "DispatchAsked"
	// EventDispatchFailed is a launch that produced no running session, with
	// the reason it gave.
	EventDispatchFailed = "DispatchFailed"
	// EventDispatchLaunched is a session actually started.
	EventDispatchLaunched = "DispatchLaunched"
)

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
