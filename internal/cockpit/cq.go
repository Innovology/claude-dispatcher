package cockpit

// cq.go is the v3 triage lens's data layer: the command queue's view-model
// types plus the collector that fills them from the user's real dispatch
// records. The queue is one dispatcher at a time — what it wants, the evidence
// behind the ask, and the acts that answer it — with everything still running
// summarised beneath. cq_view.go renders it.
//
// It derives from the snapshot collectFloor has already assembled (per-record
// forge, diff totals, per-file diffs, transcript tails) and from gh reads still
// warm in gh's memo cache, so the queue costs no extra round-trips. That
// ordering is a requirement, not an optimisation: collectCQ must run AFTER
// collectFloor in loadSnapshot. It reads ctx and the snapshot only — never the
// package data vars, which applySnapshot has not published yet on the first
// load (see the comment on collectCtx.productFor).
//
// Like every collector it is best-effort and never fatal. A signal that cannot
// be read is left out of the sentence rather than guessed at: a queue that
// invents evidence is worse than one that says less, so every clause below
// disappears when its source is unavailable.

import (
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
)

// ---- view-model types ---------------------------------------------------------

// cqItem is one entry in the command queue. Items are 1:1 with dispatch
// records: a queue entry always names a real dispatcher you can act on, so
// there is no synthetic "two dispatchers, one branch" row — a collision is
// carried as tone plus detail2 on the record it actually affects.
//
// Presentation is the view's job. This type holds the facts (product key, PR
// ref, tone name); cq_view.go composes them into labels and colours.
type cqItem struct {
	// id is the record's own key. Skip and undo must be keyed by it rather than
	// by feature: the snapshot is rebuilt on every poll and every fsnotify
	// event, and that UI state has to survive the rebuild.
	id string
	// kind classifies the ask: permission | turn-done | idle | needs.
	kind string
	// product is the raw product key, the same one collectProducts groups by.
	product string
	title   string
	repo    string
	// ref is the PR id when there is one, else the branch, else "" — a dispatch
	// that failed before branching has neither and says so by omission.
	ref string
	age string
	// want is the short label the rest-of-queue rows show.
	want string
	// tone is normal | red | amber and drives the lead's colour.
	tone    string
	lead    string
	detail  string
	detail2 string
	acts    []cqAct
	// waited is the record's UpdatedAt, kept as the queue's tie-break sort key
	// so the ask that has waited longest surfaces first.
	waited time.Time
}

// cqAct is one offered action. keep means acting does not clear the item —
// attach hands the terminal over and comes back to the same ask. An act with an
// empty ok is not actionable on its own; skip is queue rotation, not work.
type cqAct struct {
	k    string
	d    string
	ok   string
	keep bool
}

// cqWorkRow is one running dispatcher in the working view. out is a bare age
// ("6s", "2m") or "" when the session has written nothing we can read.
type cqWorkRow struct {
	feature, repo, doing, out string
}

// cqGroup is the working view's product grouping; name is the raw product key.
type cqGroup struct {
	name string
	rows []cqWorkRow
}

// ---- collector ------------------------------------------------------------------

// collectCQ fills the command queue and the working view.
//
// The integration step must add these fields to snapshot (live.go), the
// matching package vars to data.go, the nil/"" guards to applySnapshot, and
// register collectCQ in loadSnapshot after collectFloor:
//
//	cqItems      []cqItem
//	cqWorking    []cqGroup
//	cqLastOutput string
func collectCQ(ctx *collectCtx, s *snapshot) {
	// Floor rows keyed by feature. collectFloor resolved the forge and the diff
	// totals for every record in this same load; re-deriving them here would
	// mean a second `git remote get-url` and `git diff` per dispatcher.
	floorBy := make(map[string]dispatch, len(s.dispatches))
	for _, d := range s.dispatches {
		floorBy[d.feature] = d
	}

	touched := cqTouchedPaths(ctx, s)

	items := []cqItem{}
	var running []cqRunRow
	var lastOut time.Time // freshest transcript write across everything running

	for _, rec := range ctx.records {
		switch floorState(rec) {
		// "review" belongs in the queue, not outside it: a finished turn with a
		// green unreviewed PR is the single most actionable thing the cockpit
		// can show, and it is the design's own headline case. Omitting it made
		// those dispatchers vanish from triage entirely — neither queued nor
		// running — with a merge sitting there waiting.
		case "blocked", "needs", "review":
			items = append(items, cqBuildItem(ctx, s, floorBy, touched, rec))
		case "working":
			r, mt := cqBuildRunning(ctx, s, rec)
			running = append(running, r)
			if mt.After(lastOut) {
				lastOut = mt
			}
		}
	}

	cqSort(items)
	s.cqItems = items
	s.cqWorking = cqGroupRunning(running, items)
	if !lastOut.IsZero() {
		s.cqLastOutput = cqAge(lastOut)
	}
}

// cqRunRow is a working row plus the product it groups under.
type cqRunRow struct {
	product string
	row     cqWorkRow
}

// ---- queue order ------------------------------------------------------------------

// cqSort orders the queue: most urgent status first, then by tone, then oldest
// wait first.
//
// state.LoadAll's order is deliberately not reused. It sorts UpdatedAt
// descending, which is right for a recency list and wrong for a queue — it puts
// the ask that has waited longest at the bottom.
func cqSort(items []cqItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ua, ub := cqUrgency(a), cqUrgency(b); ua != ub {
			return ua < ub
		}
		if ta, tb := cqToneRank(a.tone), cqToneRank(b.tone); ta != tb {
			return ta < tb
		}
		return a.waited.Before(b.waited)
	})
}

// cqUrgency mirrors state.Status.Priority across the two statuses the queue
// admits: a permission prompt (blocked) outranks a finished turn, because
// nothing moves at all until it is answered.
func cqUrgency(it cqItem) int {
	if it.kind == "permission" {
		return 0
	}
	return 1
}

func cqToneRank(tone string) int {
	switch tone {
	case "red":
		return 0
	case "amber":
		return 1
	}
	return 2
}

// ---- one queue item ------------------------------------------------------------------

func cqBuildItem(ctx *collectCtx, s *snapshot, floorBy map[string]dispatch,
	touched map[string][]cqTouch, rec *state.Dispatch) cqItem {

	st := floorState(rec)
	fr, onFloor := floorBy[rec.Feature]
	forge := fr.forge
	if !onFloor {
		forge = ctx.forge(rec.RepoPath)
	}

	// PR signals. buildFloorRow fetched these moments ago in this same load, so
	// gh's memo cache serves them without another request.
	var checks gh.Checks
	var review gh.Review
	if forge == "gh" && rec.PRNumber > 0 {
		checks = gh.PRChecksFor(rec.RepoPath, rec.PRNumber)
		review = gh.PRReviewFor(rec.RepoPath, rec.PRNumber)
	}

	clash := cqCollision(touched, rec)
	kind := cqKind(rec, st)

	return cqItem{
		id:      rec.ID,
		kind:    kind,
		product: ctx.productFor(rec),
		title:   rec.Feature,
		repo:    rec.RepoName,
		ref:     cqRef(forge, rec),
		age:     floorAge(rec.UpdatedAt),
		want:    cqWant(kind),
		tone:    cqToneOf(st, checks, review, clash),
		lead:    cqLeadOf(s, rec, kind),
		detail:  cqDetail(fr, onFloor, forge, rec, checks, review),
		detail2: cqDetail2(rec, clash),
		acts:    cqActs(rec, kind),
		waited:  rec.UpdatedAt,
	}
}

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

// cqToneOf picks the item's tone from real signals only. Age is deliberately
// not an input: there is no evidence a thirty-minute wait is worse than a
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

// cqDetail composes the supporting evidence from clauses, dropping any clause
// whose source is unavailable. "0 reviews" and "+0 −0 across 0 files" are
// claims about an unreachable forge or an unrunnable git rather than
// observations, so they are omitted instead of printed.
func cqDetail(fr dispatch, onFloor bool, forge string, rec *state.Dispatch,
	checks gh.Checks, review gh.Review) string {

	parts := []string{cqCommits(len(rec.Commits))}
	if onFloor && fr.files > 0 {
		parts = append(parts, "+"+itoa(fr.plus)+" −"+itoa(fr.minus)+
			" across "+cqPlural(fr.files, "file", "files"))
	}
	if forge == "gh" && rec.PRNumber > 0 && review.Approvals > 0 {
		parts = append(parts, floorApprovals(review.Approvals))
	}
	if cm, _ := floorChecksMeta(checks); cm != "" {
		parts = append(parts, cm)
	}
	return strings.Join(parts, " · ")
}

// cqDetail2 is the second evidence line, and is often empty. Only two things
// have a real source behind them: another dispatcher editing the same file, and
// a workflow run that failed on this branch.
//
// What a blocked dispatcher is asking permission FOR is not available and must
// not be guessed: hookcmd discards the Notification message body (it keeps only
// session id, transcript path, cwd, event name and background tasks), and the
// permission prompt fires before PostToolUse, so neither the record nor the
// transcript knows which tool or path is pending.
func cqDetail2(rec *state.Dispatch, clash *cqClash) string {
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
// and r reply (replyCmd exists, but the v3 floor has one text affordance and it
// belongs to the dispatch draft). Attaching is the honest answer to all of
// them: it hands you the session where the prompt actually is.
func cqActs(rec *state.Dispatch, kind string) []cqAct {
	acts := []cqAct{
		{k: "⏎", d: "attach", ok: "attaching to " + rec.RepoName + " session…", keep: true},
	}
	// Marking a dispatcher shipped that produced nothing would be a lie about
	// "done means live", so the act only appears once there are commits. The key
	// is y, not d: on this screen d always opens the dispatch prompt, and an act
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

// ---- working view -------------------------------------------------------------------

// cqBuildRunning maps one working/launching record to its row, and returns its
// transcript's last-write time so the caller can report the freshest output.
func cqBuildRunning(ctx *collectCtx, s *snapshot, rec *state.Dispatch) (cqRunRow, time.Time) {
	mt := cqLastWrite(rec.TranscriptPath)
	row := cqWorkRow{
		feature: rec.Feature,
		repo:    rec.RepoName,
		doing:   cqDoing(s.tailLines[rec.Feature], rec),
	}
	if !mt.IsZero() {
		row.out = cqAge(mt)
	}
	return cqRunRow{product: ctx.productFor(rec), row: row}, mt
}

// cqDoing says, in plain language, what a running dispatcher is doing right
// now — from the last tool it used, falling back to the status the hook wrote.
func cqDoing(tail []string, rec *state.Dispatch) string {
	for i := len(tail) - 1; i >= 0; i-- {
		if !strings.HasPrefix(tail[i], "⚙ ") {
			continue
		}
		tool := strings.TrimPrefix(tail[i], "⚙ ")
		switch tool {
		case "Edit", "Write", "NotebookEdit":
			return "writing code"
		case "Bash", "BashOutput":
			return "running a command"
		case "Read", "Grep", "Glob":
			return "reading the repo"
		case "Task":
			return "running a subagent"
		case "WebFetch", "WebSearch":
			return "searching the web"
		case "TodoWrite":
			return "planning"
		}
		return strings.ToLower(tool)
	}
	if rec.StatusReason != "" {
		return rec.StatusReason
	}
	if rec.Status == state.StatusLaunching {
		return "starting up"
	}
	return ""
}

// cqGroupRunning groups the running dispatchers by product, most demanding
// product first.
//
// The order cannot come from productOrder: collectProducts runs after this, so
// that var is stale or empty on the first load. It is derived here instead —
// the product with the most asks in the queue leads, then the busiest, then by
// name so the list is stable between polls.
func cqGroupRunning(running []cqRunRow, items []cqItem) []cqGroup {
	queued := map[string]int{}
	for _, it := range items {
		queued[it.product]++
	}
	byProduct := map[string][]cqWorkRow{}
	var names []string
	for _, r := range running {
		if _, seen := byProduct[r.product]; !seen {
			names = append(names, r.product)
		}
		byProduct[r.product] = append(byProduct[r.product], r.row)
	}
	sort.SliceStable(names, func(i, j int) bool {
		a, b := names[i], names[j]
		if queued[a] != queued[b] {
			return queued[a] > queued[b]
		}
		if len(byProduct[a]) != len(byProduct[b]) {
			return len(byProduct[a]) > len(byProduct[b])
		}
		return a < b
	})
	groups := make([]cqGroup, 0, len(names))
	for _, n := range names {
		groups = append(groups, cqGroup{name: n, rows: byProduct[n]})
	}
	return groups
}

// cqLastWrite is when a session last emitted anything, taken from its
// transcript's mtime.
//
// UpdatedAt is not a substitute and must not be used here: it only moves when
// hookcmd.apply saves, and PostToolUse does not save unless it is clearing a
// block — so a dispatcher that has been editing files for ten minutes has an
// UpdatedAt ten minutes old, and showing that under a "last output" label would
// answer a different question. The transcript file, which Claude Code appends
// to as the session emits, is the one honest signal. Stat it, never parse it:
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

// cqAge is floorAge with a seconds branch. The working view's whole point is
// sub-minute freshness, and floorAge collapses everything under a minute to
// "now" — which would render as "now ago" on every healthy row.
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

// cqCommits counts a dispatcher's commits. Zero is a real observation, so it is
// reported rather than omitted.
func cqCommits(n int) string {
	if n == 0 {
		return "no commits yet"
	}
	return cqPlural(n, "commit", "commits")
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

// cqWorkFlat is the working view's rows in the order they render, so a cursor
// over the grouped display has something one-dimensional to index.
func cqWorkFlat() []cqWorkRow {
	var out []cqWorkRow
	for _, g := range cqWorking {
		out = append(out, g.rows...)
	}
	return out
}
