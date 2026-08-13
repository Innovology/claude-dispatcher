package cockpit

// cq.go is the triage lens's derive layer: the small, testable functions that
// turn one real dispatch record — plus the transcript tail, the diff and the
// forge reads collectFloor already paid for — into the facts a fleet row
// carries. fleet.go assembles the rows and ranks them; fleet_view.go draws
// them.
//
// The discipline here is the whole point of the file. Every function below
// either quotes something the user's own tooling produced or says nothing at
// all: a clause whose source is unavailable is dropped rather than filled in,
// and a signal nothing measures (what a blocked dispatcher wants permission
// FOR, whether a run is converging, what "done" means for a dispatch) is named
// in a comment as absent rather than approximated.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
)

// cqAct is one offered action. keep means acting does not clear the row —
// attach hands the terminal over and comes back to the same ask. An act with an
// empty ok is not actionable on its own; skip is table rotation, not work.
type cqAct struct {
	k    string
	d    string
	ok   string
	keep bool
}

// ---- what it is working towards ----------------------------------------------

// cqGoal is what the dispatcher is trying to achieve, and what that text
// actually is.
//
// There is no completion criterion on a dispatch record: the dispatch form's
// "DONE WHEN" field is folded into the prompt and not persisted separately, so
// nothing here can honestly be labelled a goal. The prompt is the nearest real
// thing, and it is a different claim — a brief, not a definition of done — so
// it is labelled "prompt" and never dressed up as one. When state.Dispatch
// grows a Goal field this becomes `if rec.Goal != "" { return rec.Goal, "goal" }`
// ahead of the prompt.
func cqGoal(rec *state.Dispatch) (text, label string) {
	if p := cqFirstSentence(rec.Prompt); p != "" {
		return p, "prompt"
	}
	return "", "goal"
}

// cqFirstSentence is the opening sentence of a prompt, so a paragraph-long
// brief still fits one line. It quotes, never paraphrases.
func cqFirstSentence(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, ".!?"); i > 0 {
		s = s[:i+1]
	}
	return strings.Join(strings.Fields(s), " ")
}

// cqPhase infers which segment of plan → act → observe → ship the dispatcher is
// in. "" lights nothing, which is the honest answer for a session we have read
// nothing from.
//
// Claude Code has no phase state machine and no hook that reports one, so this
// is our inference from the last tool the transcript shows — with one exception
// that is not an inference at all: an open PR means the work has left the
// dispatcher and is waiting on the forge, which is what "ship" names.
//
// The known limit: transcript.Tail keeps a tool's name and not its input, so
// "observe" fires for every Bash call — running the suite, `git status` and
// `mkdir` alike. It means "ran a shell command", not "checked its work".
func cqPhase(tail []string, rec *state.Dispatch) string {
	if rec.PRNumber > 0 && rec.PRState == "OPEN" && rec.DeployedAt == nil {
		return "ship"
	}
	for i := len(tail) - 1; i >= 0; i-- {
		if !strings.HasPrefix(tail[i], "⚙ ") {
			continue
		}
		switch strings.TrimPrefix(tail[i], "⚙ ") {
		case "Edit", "Write", "NotebookEdit":
			return "act"
		case "Bash", "BashOutput":
			return "observe"
		case "Read", "Grep", "Glob", "WebFetch", "WebSearch", "TodoWrite", "Task":
			return "plan"
		}
		// An unrecognised tool is a tool we cannot place. Light nothing rather
		// than file it under the nearest guess.
		return ""
	}
	return ""
}

// cqShortModel trims a raw model id down to something a status line can carry:
// "claude-opus-5-20260401" → "opus-5". An empty id stays empty — the caller
// drops the clause rather than printing a placeholder.
func cqShortModel(id string) string {
	m := strings.ToLower(strings.TrimSpace(id))
	m = strings.TrimPrefix(m, "claude-")
	// Drop a trailing 8-digit date stamp, which is noise in a status line.
	if i := strings.LastIndex(m, "-"); i > 0 && len(m)-i == 9 && cqAllDigits(m[i+1:]) {
		m = m[:i]
	}
	return m
}

func cqAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// ---- what it wants ------------------------------------------------------------

// cqKind classifies what the dispatcher is asking for. The reasons matched here
// are exactly the strings hookcmd.apply writes; anything else falls through to
// the honest "it stopped".
func cqKind(rec *state.Dispatch, st string) string {
	if st == "blocked" {
		return "permission"
	}
	if st == "review" {
		return "review"
	}
	switch rec.StatusReason {
	case "turn complete — waiting on you":
		return "turn-done"
	case "waiting for your next prompt":
		return "idle"
	}
	return "needs"
}

func cqWant(kind string) string {
	switch kind {
	case "permission":
		return "approve a permission"
	case "review":
		return "approve a merge"
	case "turn-done":
		return "it finished a turn"
	case "idle":
		return "it is waiting on you"
	}
	return "it stopped"
}

// cqRef names the thing you would look at: the PR when there is one, else the
// branch it is working on.
func cqRef(forge string, rec *state.Dispatch) string {
	if rec.PRNumber > 0 {
		return prID(forge, rec.PRNumber)
	}
	return rec.Branch
}

// cqToneOf picks the row's tone from real signals only. Age is deliberately not
// an input: there is no evidence a thirty-minute wait is worse than a
// five-minute one, and any threshold would be invented.
func cqToneOf(st string, checks gh.Checks, review gh.Review, clash *cqClash) string {
	switch {
	case checks.Failing > 0:
		return "red"
	case review.ChangesRequested > 0:
		return "red"
	case clash != nil:
		return "red"
	case st == "blocked":
		// Stopped, and unable to recover on its own.
		return "amber"
	}
	return "normal"
}

// cqLeadOf is the one sentence saying what the dispatcher wants. For a finished
// turn the truest answer is the last thing it actually said, used verbatim — if
// it asked a question, the lead reads as a question.
func cqLeadOf(s *snapshot, rec *state.Dispatch, kind string) string {
	if kind != "permission" {
		if said := s.saidBy[rec.Feature]; said != "" {
			return said
		}
	}
	if sr := cqSentence(rec.StatusReason); sr != "" {
		return sr
	}
	if kind == "permission" {
		return "It is blocked and waiting on you."
	}
	return "It stopped and is waiting on you."
}

// cqWhy is the sentence the detail panel leads with.
//
// On a red row the second-order fact is the better answer, because it is the
// reason the row is red: another dispatcher editing the same file, or a
// workflow that failed on this branch. Everywhere else the truest answer is
// what the dispatcher itself last said, which is what cqLeadOf returns.
func cqWhy(s *snapshot, rec *state.Dispatch, kind, tone string, clash *cqClash) string {
	if tone == "red" {
		if note := cqClashNote(rec, clash); note != "" {
			return note
		}
	}
	return cqLeadOf(s, rec, kind)
}

// cqClashNote is the second-order fact behind a red row, and is often empty.
// Only two things have a real source: another dispatcher editing the same file,
// and a workflow run that failed on this branch. The design's own note is an
// editorial reading of the diff ("graceWindow still returns a flat 7 days"),
// which nothing here can produce — these two facts are what we actually have.
//
// What a blocked dispatcher is asking permission FOR is not available and must
// not be guessed: hookcmd discards the Notification message body (it keeps only
// session id, transcript path, cwd, event name and background tasks), and the
// permission prompt fires before PostToolUse, so neither the record nor the
// transcript knows which tool or path is pending.
func cqClashNote(rec *state.Dispatch, clash *cqClash) string {
	if clash != nil {
		line := "Touches " + clash.path + ", also being edited by \"" + clash.by + "\""
		if clash.others > 0 {
			line += " and " + cqPlural(clash.others, "other dispatcher", "other dispatchers")
		}
		return line + "."
	}
	// Warm in gh's memo cache from floorRuns in this same load.
	for _, r := range gh.RunsForBranch(rec.RepoPath, rec.Branch) {
		if strings.EqualFold(r.Status, "completed") && r.Conclusion == "failure" {
			// gh.Run carries no run id or number, so the workflow name is the
			// only handle there is.
			return "Workflow \"" + r.Name + "\" failed " + floorAge(r.CreatedAt) + " ago."
		}
	}
	return ""
}

// cqActs offers only acts wired to a real action today.
//
// Deliberately absent, and not to be added without the action behind them:
// y approve / n deny (nothing in this repo answers a Claude Code permission
// prompt — v2's n only set a notice and denied nothing), o open pr (nothing
// opens a URL), R retry (no rerun action, and gh.Run carries no id to target),
// F follow (nothing here tails a session without attaching to it), and r reply
// (replyCmd exists, but this screen has one text affordance and it belongs to
// the dispatch form).  Attaching is the honest answer to all of them: it hands
// you the session where the prompt actually is.
func cqActs(rec *state.Dispatch, kind string) []cqAct {
	acts := []cqAct{
		{k: "⏎", d: "attach", ok: "attaching to " + rec.RepoName + " session…", keep: true},
	}
	// A running dispatcher is not asking for anything, so there is nothing to
	// answer and nothing to skip past: it can be watched or stopped, and that is
	// the whole list.
	if kind == "running" {
		return append(acts, cqAct{k: "x", d: "kill", ok: "killed \"" + rec.Feature + "\""})
	}
	// Marking a dispatcher shipped that produced nothing would be a lie about
	// "done means live", so the act only appears once there are commits. The key
	// is y, not d: on this screen d always opens the dispatch form, and an act
	// the footer advertises but the key handler never reaches is worse than none.
	switch {
	case kind == "review":
		// The PR is open and the ask IS the merge, so y squash-merges it for
		// real (shipCmd) rather than just marking the record done.
		acts = append(acts, cqAct{k: "y", d: "approve merge",
			ok: "merging #" + itoa(rec.PRNumber) + " into " + rec.RepoName})
	case kind != "permission" && len(rec.Commits) > 0:
		acts = append(acts, cqAct{k: "y", d: "mark shipped",
			ok: "\"" + rec.Feature + "\" marked shipped"})
	}
	acts = append(acts,
		cqAct{k: "x", d: "kill", ok: "killed \"" + rec.Feature + "\""},
		cqAct{k: "s", d: "skip"})
	return acts
}

// ---- collisions ---------------------------------------------------------------------

// cqTouch is one in-flight record's set of edited paths within a repo.
type cqTouch struct {
	feature string
	paths   map[string]bool
}

// cqClash is a file two or more in-flight dispatchers are editing at once.
type cqClash struct {
	path   string
	by     string
	others int
}

// cqTouchedPaths indexes, per repo, which in-flight dispatcher is editing which
// files. The path sets come from collectFloor's numstat, so this costs no git.
func cqTouchedPaths(ctx *collectCtx, s *snapshot) map[string][]cqTouch {
	out := map[string][]cqTouch{}
	for _, rec := range ctx.records {
		switch rec.Status {
		case state.StatusWorking, state.StatusLaunching,
			state.StatusBlocked, state.StatusNeedsInput:
		default:
			continue
		}
		files := s.diffsBy[rec.Feature].files
		if len(files) == 0 {
			continue
		}
		paths := make(map[string]bool, len(files))
		for _, f := range files {
			paths[f.path] = true
		}
		out[rec.RepoName] = append(out[rec.RepoName], cqTouch{feature: rec.Feature, paths: paths})
	}
	return out
}

// cqCollision reports the first path this record shares with another in-flight
// dispatcher in the same repo, or nil. Paths and features are scanned in sorted
// order so the sentence does not shuffle between two identical snapshots.
func cqCollision(touched map[string][]cqTouch, rec *state.Dispatch) *cqClash {
	var mine map[string]bool
	for _, t := range touched[rec.RepoName] {
		if t.feature == rec.Feature {
			mine = t.paths
			break
		}
	}
	if len(mine) == 0 {
		return nil
	}
	paths := make([]string, 0, len(mine))
	for p := range mine {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		var by []string
		for _, t := range touched[rec.RepoName] {
			if t.feature != rec.Feature && t.paths[p] {
				by = append(by, t.feature)
			}
		}
		if len(by) == 0 {
			continue
		}
		sort.Strings(by)
		return &cqClash{path: p, by: by[0], others: len(by) - 1}
	}
	return nil
}

// ---- passes -----------------------------------------------------------------------

// cqPassCounts tallies the prompts submitted to each dispatcher from the
// lifecycle event log, keyed by dispatcher id.
//
// This counts turns, not "repair rounds": nothing records how many times a
// dispatcher went back to fix its own work, so the view must not claim it does.
// It is why the column is headed TURN and not the design's P. A dispatcher
// launched without CLAUDE_DISPATCHER_ID has no attributed events and counts
// zero, which the view renders by leaving the cell empty.
//
// The log is append-only and grows without bound, so the parse is memoised on
// the file's size and mtime: a poll that finds nothing new re-reads nothing.
func cqPassCounts() map[string]int {
	path := filepath.Join(state.Dir(), "events.jsonl")
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	key := itoa(int(fi.Size())) + ":" + itoa(int(fi.ModTime().UnixNano()))

	passMu.Lock()
	defer passMu.Unlock()
	if key == passKey {
		return passCache
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	counts := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		row := strings.TrimSpace(sc.Text())
		if row == "" || row[0] != '{' {
			continue
		}
		var ev state.Event
		if json.Unmarshal([]byte(row), &ev) != nil || ev.DispatcherID == "" {
			continue
		}
		if ev.Event == "UserPromptSubmit" {
			counts[ev.DispatcherID]++
		}
	}
	passKey, passCache = key, counts
	return counts
}

var (
	passMu    sync.Mutex
	passKey   string
	passCache map[string]int
)

// ---- running dispatchers -------------------------------------------------------------

// cqShipDetail says where a dispatcher's PR stands, and whether that amounts to
// "green and not merging" — the one non-convergence this cockpit can actually
// demonstrate.
//
// The design's other trigger, thrash, needs a check result sampled twice over
// time; gh.Checks is a point sample, so no trend is claimed, no arrow is drawn
// and no row is glyphed ◆ for it. A row with no PR has no detail rather than a
// filler one.
func cqShipDetail(forge string, rec *state.Dispatch) (detail string, stalled bool) {
	if rec.PRNumber > 0 && rec.PRState == "MERGED" {
		return "merged, deploying", false
	}
	if rec.PRNumber == 0 || rec.PRState != "OPEN" || forge != "gh" {
		return "", false
	}
	c := gh.PRChecksFor(rec.RepoPath, rec.PRNumber)
	switch {
	case c.Total == 0:
		return "pr open, no checks", false
	case c.Failing > 0:
		return "ci · " + itoa(c.Failing) + " of " + itoa(c.Total) + " red", false
	case c.Running > 0:
		return "ci · " + itoa(c.Passed) + " of " + itoa(c.Total) + " green", false
	}
	return "green, unmerged", true
}

// cqLastWrite is when a session last emitted anything, taken from its
// transcript's mtime.
//
// UpdatedAt is not a substitute and must not be used alone here: it only moves
// when hookcmd.apply saves, and PostToolUse does not save unless it is clearing
// a block — so a dispatcher that has been editing files for ten minutes has an
// UpdatedAt ten minutes old, and showing that under an age column would answer a
// different question. The transcript file, which Claude Code appends to as the
// session emits, is the one honest liveness signal. Stat it, never parse it:
// the JSONL format is internal and version-unstable.
func cqLastWrite(path string) time.Time {
	if path == "" {
		return time.Time{}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// ---- small helpers -------------------------------------------------------------------

// cqAge is floorAge with a seconds branch. The AGE column's whole point is
// sub-minute freshness, and floorAge collapses everything under a minute to
// "now" — which would render as "now" on every healthy row.
func cqAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h"
	default:
		return itoa(int(d/(24*time.Hour))) + "d"
	}
}

func cqPlural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// cqSentence renders a status reason as a sentence. Capitalising and closing a
// string the hook already wrote is a display transform, not new content.
func cqSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	switch r[len(r)-1] {
	case '.', '!', '?', ':', '…':
		return string(r)
	}
	return string(r) + "."
}
