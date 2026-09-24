// Package fleetcmd is the fleet from the command line: what every live
// dispatcher is doing and wants (`status`), and the things a human does to one
// from the triage table without attaching — answer it (`reply`), shelve it
// (`park`) and take it back up (`unpark`) — plus the steward's own act, saying
// what it makes of a wait (`note`), and its wake-up (`status --next`).
//
// It exists so a Claude Code session can steward the fleet. The cockpit is a
// TUI, and a session cannot read one; these print what the triage table says,
// from the same records and by the same rules (the ask is ask.Of, the retry is
// dispatch.RetryPending), and act through the same writes the cockpit makes.
// Nothing here decides anything — a steward's judgement is its own; this is
// only its eyes and hands.
package fleetcmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/ask"
	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// The supervisor calls, as seams for tests.
var (
	sessionAlive = supervisor.HasSession
	sessionIdle  = supervisor.SessionIdle
	sendKeys     = supervisor.SendKeys
)

// Entry is one dispatcher as `status --json` prints it. The field names are the
// contract a steward's prompt is written against, so they only ever grow.
type Entry struct {
	ID      string `json:"id"`
	Feature string `json:"feature"`
	Repo    string `json:"repo"`
	Product string `json:"product,omitempty"`
	Branch  string `json:"branch,omitempty"`
	// Prompt is the brief it was dispatched with — what "inside the brief"
	// is measured against.
	Prompt string `json:"prompt,omitempty"`
	// State is the triage table's reading: "wants-you" (stopped on something
	// only the human can answer), "running" (working, or a retry on its way),
	// "parked" (the human shelved it) or "finished" (ended, not yet dismissed).
	State  string `json:"state"`
	Status string `json:"status"` // the record's own, from the hooks
	Reason string `json:"reason,omitempty"`
	// Ask is the question its last turn closed on, verbatim; Said is that
	// whole last message. Both are empty while it is working.
	Ask  string `json:"ask,omitempty"`
	Said string `json:"said,omitempty"`
	// Failure is the API error that ended its last turn, with the retry the
	// cockpit has scheduled when it has one.
	Failure  *state.Failure `json:"failure,omitempty"`
	Retrying bool           `json:"retrying,omitempty"`
	Parked   string         `json:"parked,omitempty"`
	PR       int            `json:"pr,omitempty"`
	PRState  string         `json:"pr_state,omitempty"`
	PRURL    string         `json:"pr_url,omitempty"`
	Mode     string         `json:"mode,omitempty"`
	// SubagentsLive is the fan-out the hooks report running right now.
	SubagentsLive int `json:"subagents_live,omitempty"`
	// LastActivity is when the session last wrote its transcript — the one
	// clock the session itself keeps — falling back to the record's.
	LastActivity time.Time `json:"last_activity"`
	Started      time.Time `json:"started"`
	Session      string    `json:"session,omitempty"`
	Worktree     string    `json:"worktree,omitempty"`
	// WaitingSince is when it stopped to wait; Note is the steward's note on
	// that wait, if it has written one. Handled is a wait with a note: it has
	// been looked at, and waking the steward for it again would be noise.
	WaitingSince *time.Time `json:"waiting_since,omitempty"`
	Note         string     `json:"note,omitempty"`
	Handled      bool       `json:"handled,omitempty"`

	failure string // the row's failure clause, for the text form
}

// Run is the entry point for the four verbs; it returns the exit code.
func Run(verb string, args []string, out, errOut io.Writer) int {
	var err error
	switch verb {
	case "status":
		err = runStatus(args, out)
	case "reply":
		err = runReply(args, out)
	case "park":
		err = runPark(args, out)
	case "note":
		err = runNote(args, out)
	case "unpark":
		err = runUnpark(args, out)
	default:
		err = fmt.Errorf("unknown command %q", verb)
	}
	if err != nil {
		_, _ = fmt.Fprintln(errOut, verb+":", err)
		return 1
	}
	return 0
}

func runStatus(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the fleet as JSON")
	next := fs.Bool("next", false, "wait until a dispatcher is waiting unhandled, then print those as JSON")
	timeout := fs.Duration("timeout", 30*time.Minute, "with --next: give up after this long and say so")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *next {
		return runNext(out, *timeout, time.Now)
	}
	entries := Fleet(state.LoadAll(), time.Now())
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, "nothing in flight")
		return nil
	}
	for _, e := range entries {
		_, _ = fmt.Fprintf(out, "%-10s %-28s %-22s %s\n", e.State, clip(e.Feature, 28), clip(e.Repo, 22), e.headline())
	}
	return nil
}

// NextPoll is how often `status --next` re-reads the records. A read is a
// directory of small JSON files; the steward it wakes costs far more per wake
// than this costs per minute.
var NextPoll = 3 * time.Second

// Next is what `status --next` prints: the waits nobody has handled, or a
// timeout with none — the steward's cue to look over the running rows.
type Next struct {
	Waiting  []Entry `json:"waiting"`
	TimedOut bool    `json:"timed_out,omitempty"`
}

// runNext blocks until some dispatcher is waiting with no steward note on its
// current wait, then prints every such one and returns.
//
// It is how a steward session sleeps: run as a Claude Code background task, the
// session's turn ends waiting on it (the Stop hook reads that as working, not as
// wanting anyone), and its exit is what wakes the session. No baseline is kept
// between runs, so nothing that happened between two runs can be missed: a
// wait is pending until a reply ends it or a note answers it, and both are on
// the record.
func runNext(out io.Writer, timeout time.Duration, now func() time.Time) error {
	deadline := now().Add(timeout)
	for {
		var waiting []Entry
		for _, e := range Fleet(state.LoadAll(), now()) {
			if e.State == "wants-you" && !e.Handled {
				waiting = append(waiting, e)
			}
		}
		timedOut := !now().Before(deadline)
		if len(waiting) > 0 || timedOut {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(Next{Waiting: waiting, TimedOut: len(waiting) == 0 && timedOut})
		}
		time.Sleep(NextPoll)
	}
}

// headline is the text form's last column: the ask, else the failure, else the
// reason — the same precedence the triage row's SIGNAL uses.
func (e Entry) headline() string {
	switch {
	case e.Note != "":
		return "steward: " + e.Note
	case e.Ask != "":
		return e.Ask
	case e.Parked != "":
		return "parked · " + e.Parked
	case e.failure != "":
		return e.failure
	}
	return e.Reason
}

// Fleet is the triage table's live rows, most urgent first: every dispatcher
// that is not over, and every finished one nobody has dismissed.
func Fleet(ds []*state.Dispatch, now time.Time) []Entry {
	var out []Entry
	for _, d := range ds {
		st := stateOf(d, now)
		if st == "" {
			continue
		}
		e := Entry{
			ID: d.ID, Feature: d.Feature, Repo: d.RepoName, Product: d.Product,
			Branch: d.Branch, Prompt: d.Prompt, State: st, Status: string(d.Status), Reason: d.StatusReason,
			Failure: d.Failure, Retrying: dispatch.RetryPending(d, now),
			Parked: d.ParkedReason, PR: d.PRNumber, PRState: d.PRState, PRURL: d.PRURL,
			Mode: d.Mode, SubagentsLive: d.SubagentsLive(),
			LastActivity: lastActivity(d), Started: d.CreatedAt,
			Session: d.TmuxSession, Worktree: d.WorktreePath,
			failure:      dispatch.FailureSummary(d, now),
			WaitingSince: d.WaitingSince,
			Note:         d.Note(),
		}
		e.Handled = e.Note != ""
		if d.Status == state.StatusNeedsInput || d.Finished() {
			e.Said, e.Ask = d.Said, ask.Of(d.Said)
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i].State), rank(out[j].State); ri != rj {
			return ri < rj
		}
		return out[i].LastActivity.After(out[j].LastActivity)
	})
	return out
}

// stateOf is floorState's reading in the four words a steward acts on, or ""
// for a dispatcher that is over and has been read.
func stateOf(d *state.Dispatch, now time.Time) string {
	switch {
	case d.Held():
		return "finished"
	case d.Finished():
		return ""
	case d.Parked():
		return "parked"
	case d.Status == state.StatusBlocked:
		return "wants-you"
	case d.Status == state.StatusNeedsInput && !dispatch.RetryPending(d, now):
		return "wants-you"
	}
	return "running"
}

func rank(st string) int {
	return map[string]int{"wants-you": 0, "running": 1, "finished": 2, "parked": 3}[st]
}

func lastActivity(d *state.Dispatch) time.Time {
	if fi, err := os.Stat(d.TranscriptPath); err == nil && d.TranscriptPath != "" && fi.ModTime().After(d.UpdatedAt) {
		return fi.ModTime()
	}
	return d.UpdatedAt
}

func runReply(args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New("usage: reply <id|feature> <text…>")
	}
	d, err := find(args[0])
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(args[1:], " "))
	if text == "" {
		return errors.New("nothing to send")
	}
	// The same refusals the cockpit's r makes, for the same reasons.
	if d.Status == state.StatusBlocked {
		return fmt.Errorf("%q is on a permission prompt — a menu, which typed text would answer; attach to it", d.Feature)
	}
	if d.Finished() || !sessionAlive(d.TmuxSession) {
		return fmt.Errorf("%q has no live session", d.Feature)
	}
	if idle, known := sessionIdle(d.TmuxSession); idle && known {
		return fmt.Errorf("claude has exited in %q's session", d.Feature)
	}
	if err := sendKeys(d.TmuxSession, text); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "replied to %q\n", d.Feature)
	return nil
}

func runPark(args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New("usage: park <id|feature> <reason…>")
	}
	reason := strings.Join(strings.Fields(strings.Join(args[1:], " ")), " ")
	if reason == "" {
		return errors.New("say why — the reason is what the parked group shows")
	}
	return edit(args[0], func(d *state.Dispatch) (string, error) {
		now := time.Now()
		d.ParkedReason, d.ParkedAt = reason, &now
		return "parked " + quote(d.Feature) + " · " + reason, nil
	}, out)
}

// runNote records the steward's reading of a dispatcher's current wait. It is
// refused on one that is not waiting: a note on a working dispatcher would be
// about nothing, and would be cleared by the next prompt anyway.
func runNote(args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New("usage: note <id|feature> <text…>")
	}
	text := strings.Join(strings.Fields(strings.Join(args[1:], " ")), " ")
	if text == "" {
		return errors.New("nothing to note")
	}
	return edit(args[0], func(d *state.Dispatch) (string, error) {
		if !d.Waiting() {
			return "", fmt.Errorf("%q is not waiting on anyone", d.Feature)
		}
		now := time.Now()
		d.StewardNote, d.StewardNoteAt = text, &now
		return "noted " + quote(d.Feature) + " · " + text, nil
	}, out)
}

func runUnpark(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: unpark <id|feature>")
	}
	return edit(args[0], func(d *state.Dispatch) (string, error) {
		d.ParkedReason, d.ParkedAt = "", nil
		return quote(d.Feature) + " back on the fleet", nil
	}, out)
}

// edit applies an annotation under the hook lock, against a fresh read, so it
// cannot drop a hook's write that landed in between.
func edit(key string, apply func(*state.Dispatch) (string, error), out io.Writer) error {
	release := state.Lock()
	defer release()
	d, err := find(key)
	if err != nil {
		return err
	}
	msg, err := apply(d)
	if err != nil {
		return err
	}
	if err := state.Save(d); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, msg)
	return nil
}

// find resolves a record id, or a feature name among the dispatchers that are
// not over. A feature can belong to several finished records — re-dispatching
// a shipped feature reuses the name — so a name that matches more than one
// live record is refused rather than guessed at.
func find(key string) (*state.Dispatch, error) {
	all := state.LoadAll()
	for _, d := range all {
		if d.ID == key {
			return d, nil
		}
	}
	var hits []*state.Dispatch
	for _, d := range all {
		if d.Feature == key && !d.Finished() {
			hits = append(hits, d)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return nil, fmt.Errorf("no live dispatcher %q (try its id from `status`)", key)
	}
	return nil, fmt.Errorf("%q names %d live dispatchers — use the id", key, len(hits))
}

func quote(s string) string { return "\"" + s + "\"" }

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
