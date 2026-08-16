package cockpit

// fleet.go is the v4 triage lens's data layer. The v3 command queue showed one
// ask at a time with everything else summarised beneath it; the fleet is one
// flat table of everything in flight — the dispatchers that want you ranked
// above the ones getting on with it — plus the detail panel for whichever row
// the cursor is on. fleet_view.go draws it; cq.go composes the sentences it
// carries.
//
// It derives from the snapshot collectFloor has already assembled (per-record
// forge, diff totals, per-file diffs, transcript tails) and from gh reads still
// warm in gh's memo cache, so the table costs no extra round-trips. That
// ordering is a requirement, not an optimisation: collectFleet must run AFTER
// collectFloor in loadSnapshot. It reads ctx and the snapshot only — never the
// package data vars, which applySnapshot has not published yet on the first
// load (see the comment on collectCtx.productFor).
//
// Like every collector it is best-effort and never fatal. A signal that cannot
// be read is left out rather than guessed at: a cell with no source behind it
// is empty, and a table that invents a column is worse than one that says less.

import (
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/transcript"
)

// ---- view-model ---------------------------------------------------------------

// fleetRow is one line of the table, and 1:1 with a dispatch record: a row
// always names a real dispatcher you can act on, so there is no synthetic "two
// dispatchers, one branch" row — a collision is carried as tone plus the
// sentence on the record it actually affects.
//
// Presentation is the view's job. This type holds the facts (product key, PR
// ref, tone name, timestamps); fleet_view.go composes them into cells, labels
// and colours.
type fleetRow struct {
	// id is the record's own key. The cursor, the skip order, the suppressed set
	// and undo are all keyed by it rather than by feature: the table is rebuilt
	// from the records on every poll and every fsnotify event, and that UI state
	// has to survive the rebuild.
	id string
	// kind is "queue" — it is waiting on you — "run", it is getting on with it,
	// or "past", its session is over. rank orders the table and picks the glyph;
	// see fleetRank. Past rows are not part of the in-flight table at all: they
	// are what `h` and the history filter show, and what resume acts on.
	kind string
	rank int
	// ask classifies what a queue row wants: permission | review | turn-done |
	// idle | needs. Empty on a running row, which is not asking for anything.
	ask string

	// product is the raw product key, the same one collectProducts groups by.
	product string
	feature string
	repo    string
	// ref is the PR id when there is one, else the branch, else "" — a dispatch
	// that failed before branching has neither and says so by omission.
	ref string

	// stage is the plan | act | observe | ship segment the chain lights, or ""
	// when there is nothing to infer it from. It is our inference from the last
	// tool used, not a state Claude Code reports — see cqPhase.
	stage string
	// pass counts the prompts submitted to this dispatcher (events.jsonl), 0
	// when the log has nothing attributed to it.
	pass int
	// signal is the SIGNAL cell: what a queue row wants, or where a running
	// row's PR stands. Empty when a running row has no PR — there is nothing
	// true to put there.
	signal string
	// tone is normal | red | amber and drives the detail panel's colour.
	tone string
	// why is the one sentence the detail panel leads with.
	why string
	// goal is what the dispatcher is working towards, and goalLabel names what
	// that text actually is — see cqGoal. Empty goal means nothing was recorded.
	goal      string
	goalLabel string

	// ctxTokens is what the last assistant turn had in its context window and
	// model is what ran it, both from the transcript; ctxKnown is false when the
	// transcript could not be read.
	ctxTokens int
	model     string
	ctxKnown  bool

	// mode is the permission mode this dispatcher was launched in, straight off
	// the record. Empty for a record written before the mode was a choice: those
	// ran in whatever the human's own Claude Code defaults to, and reporting
	// that as "auto" would be inventing the one fact nobody recorded.
	mode string

	acts []cqAct

	// moved is the freshest of the transcript's mtime and the record's
	// UpdatedAt: when this dispatcher was last seen doing anything. It is the
	// LAST column. See fleetMoved.
	moved time.Time
	// started is when the dispatch was created, and is the AGE column: how long
	// this dispatcher has been alive, which nothing it does resets.
	//
	// The two are different questions and a row can answer them very
	// differently — "4s / 3h" is a session still going after three hours, "3h /
	// 3h" is one that has said nothing since the moment it started. One column
	// could only ever have shown one of those, and the pair is the reading.
	started time.Time
	// waited is the record's UpdatedAt, kept as the queue rows' tie-break sort
	// key so the ask that has waited longest surfaces first.
	waited time.Time
}

// ---- collector ------------------------------------------------------------------

// collectFleet fills the triage table.
//
// The integration step must add these fields to snapshot (live.go), the
// matching package vars to data.go, the nil/"" guards to applySnapshot, and
// register collectFleet in loadSnapshot after collectFloor:
//
//	fleet        []fleetRow
//	cqLastOutput time.Time
func collectFleet(ctx *collectCtx, s *snapshot) {
	// Floor rows keyed by feature. collectFloor resolved the forge for every
	// record in this same load; re-deriving it here would mean a second
	// `git remote get-url` per dispatcher.
	floorBy := make(map[string]dispatch, len(s.dispatches))
	for _, d := range s.dispatches {
		floorBy[d.feature] = d
	}

	touched := cqTouchedPaths(ctx, s)
	passes := cqPassCounts()

	rows := []fleetRow{}
	var lastOut time.Time // freshest transcript write across everything running

	for _, rec := range ctx.records {
		switch floorState(rec) {
		// "review" belongs in the table's top half, not outside it: a finished
		// turn with a green unreviewed PR is the single most actionable thing the
		// cockpit can show. Omitting it made those dispatchers vanish from triage
		// entirely — neither queued nor running — with a merge sitting there
		// waiting.
		case "blocked", "needs", "review":
			rows = append(rows, fleetQueueRow(ctx, s, floorBy, touched, passes, rec))
		case "working":
			r, mt := fleetRunRow(ctx, s, floorBy, passes, rec)
			rows = append(rows, r)
			if mt.After(lastOut) {
				lastOut = mt
			}
		default:
			// Everything else is a session that is over: shipped ("live"), or
			// ended without shipping (floorState ""). Both used to be dropped
			// here, and a dropped record is a dispatcher the cockpit can never
			// show again — its transcript, branch and worktree all still exist,
			// but nothing on any screen could reach them. They are collected as
			// history rows, kept out of the in-flight table, and resumable.
			rows = append(rows, fleetPastRow(ctx, passes, rec))
		}
	}

	fleetSort(rows)
	s.fleet = rows
	// The instant, not its age: rendering it here would freeze it at the age it
	// had when this load ran. See cqLastOutput in data.go.
	s.cqLastOutput = lastOut
}

// ---- rank and order -------------------------------------------------------------

// fleetRank is the table's whole priority scheme, and the glyph legend in the
// help sheet is written against it:
//
//	0  it wants you and something is wrong  ●  failing checks, changes
//	                                           requested, or a file two
//	                                           dispatchers are both editing
//	1  it wants you                         ○
//	2  it is drifting                       ◆  green checks on a PR nobody
//	                                           has merged
//	3  it is running clean                  ·
//
// The design's other rank-2 trigger, thrash, is not implemented and must not
// be: it needs a check result sampled twice over time, and gh.Checks is a point
// sample. See cqShipDetail.
//
// Rank 4 is history. It is below every live row and never shares a table with
// one, so it needs no glyph or colour of its own: both fall through to the
// rank-3 defaults, and the SIGNAL cell says how the session ended.
func fleetRank(kind, tone string, stalled bool) int {
	if kind == "past" {
		return fleetPastRank
	}
	if kind == "queue" {
		if tone == "red" {
			return 0
		}
		return 1
	}
	if stalled {
		return 2
	}
	return 3
}

// fleetSort orders the table: rank first, then — within a rank — the tie-break
// that suits the rows in it. Queue rows keep the v3 queue's order (a permission
// prompt over a finished turn, then tone, then the longest wait), running rows
// lead with the one that has been quiet longest.
//
// Every comparison ends on the record id. Determinism is a correctness
// requirement here, not tidiness: the cursor is a position in this slice, and a
// pair of rows that swapped between two identical polls would move the
// selection under the reader's hands.
func fleetSort(rows []fleetRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.kind == "past" {
			// History reads the other way round from the live table: the thing
			// that just ended is the one you are most likely to want back, so the
			// newest sits at the top.
			if !a.moved.Equal(b.moved) {
				return a.moved.After(b.moved)
			}
			return a.id < b.id
		}
		if a.kind == "queue" {
			if ua, ub := cqUrgency(a), cqUrgency(b); ua != ub {
				return ua < ub
			}
			if ta, tb := cqToneRank(a.tone), cqToneRank(b.tone); ta != tb {
				return ta < tb
			}
			if !a.waited.Equal(b.waited) {
				return a.waited.Before(b.waited)
			}
			return a.id < b.id
		}
		if !a.moved.Equal(b.moved) {
			return a.moved.Before(b.moved)
		}
		return a.id < b.id
	})
}

// cqUrgency mirrors state.Status.Priority across the two asks the top of the
// table admits: a permission prompt (blocked) outranks a finished turn, because
// nothing moves at all until it is answered.
func cqUrgency(r fleetRow) int {
	if r.ask == "permission" {
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

// ---- one row --------------------------------------------------------------------

// fleetQueueRow builds a row for a dispatcher that is waiting on you.
func fleetQueueRow(ctx *collectCtx, s *snapshot, floorBy map[string]dispatch,
	touched map[string][]cqTouch, passes map[string]int, rec *state.Dispatch) fleetRow {

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
	ask := cqKind(rec, st)
	tone := cqToneOf(st, checks, review, clash)
	goal, goalLabel := cqGoal(rec)
	u, ctxKnown := transcript.LastUsage(rec.TranscriptPath)

	return fleetRow{
		id:        rec.ID,
		kind:      "queue",
		rank:      fleetRank("queue", tone, false),
		ask:       ask,
		product:   ctx.productFor(rec),
		feature:   rec.Feature,
		repo:      rec.RepoName,
		ref:       cqRef(forge, rec),
		stage:     cqPhase(s.tailLines[rec.Feature], rec),
		pass:      passes[rec.ID],
		signal:    cqWant(ask),
		tone:      tone,
		why:       cqWhy(s, rec, ask, tone, clash),
		goal:      goal,
		goalLabel: goalLabel,
		ctxTokens: u.Tokens,
		model:     cqShortModel(u.Model),
		ctxKnown:  ctxKnown,
		mode:      rec.Mode,
		acts:      cqActs(rec, ask),
		moved:     fleetMoved(rec),
		started:   rec.CreatedAt,
		waited:    rec.UpdatedAt,
	}
}

// fleetRunRow builds a row for a dispatcher that is getting on with it, and
// returns its transcript's last-write time so the caller can report the
// freshest output across the fleet.
//
// A running row is not asking for anything, so most of the queue row's
// composition has nothing to say here: there is no want, no tone beyond the one
// drift we can demonstrate, and no lead sentence unless the session actually
// said something. The design fills those gaps with prose ("Working through its
// loop", "Worth a look before it burns more context"); none of it has a source,
// so none of it is written.
func fleetRunRow(ctx *collectCtx, s *snapshot, floorBy map[string]dispatch,
	passes map[string]int, rec *state.Dispatch) (fleetRow, time.Time) {

	mt := cqLastWrite(rec.TranscriptPath)
	// The forge comes from the floor row rather than a second `git remote
	// get-url`: collectFloor resolved it for this record moments ago.
	forge := floorBy[rec.Feature].forge
	signal, stalled := cqShipDetail(forge, rec)
	// The record exists and no hook has fired for it: it was launched, and
	// nothing has been heard from the session since. cqShipDetail has nothing to
	// say about a dispatcher with no PR, so the cell would be blank — which on
	// the row the human is watching most closely reads as "no news", when the
	// news is that it is still starting. It says the same words the placeholder
	// said a moment ago, because it is the same wait continuing.
	if signal == "" && rec.Status == state.StatusLaunching {
		signal = startingSignal
	}

	tone, why := "normal", s.saidBy[rec.Feature]
	if stalled {
		// The one sentence a running row earns: a restatement of gh.Checks and
		// rec.PRState, not a reading of them.
		tone, why = "amber", "Its checks are green and the PR is not merged."
	}
	u, ctxKnown := transcript.LastUsage(rec.TranscriptPath)

	moved := rec.UpdatedAt
	if mt.After(moved) {
		moved = mt
	}
	return fleetRow{
		id:        rec.ID,
		kind:      "run",
		rank:      fleetRank("run", tone, stalled),
		product:   ctx.productFor(rec),
		feature:   rec.Feature,
		repo:      rec.RepoName,
		ref:       cqRef(forge, rec),
		stage:     cqPhase(s.tailLines[rec.Feature], rec),
		pass:      passes[rec.ID],
		signal:    signal,
		tone:      tone,
		why:       why,
		ctxTokens: u.Tokens,
		model:     cqShortModel(u.Model),
		ctxKnown:  ctxKnown,
		mode:      rec.Mode,
		acts:      cqActs(rec, "running"),
		moved:     moved,
		started:   rec.CreatedAt,
		waited:    rec.UpdatedAt,
	}, mt
}

// fleetPastRank is where history sits: below everything alive.
const fleetPastRank = 4

// fleetPastRow builds a row for a dispatcher whose session is over.
//
// It is deliberately the cheapest row of the three. A machine accumulates
// finished dispatchers forever while the live table stays small, so this pays
// for no gh request, no diff and no transcript read — one stat for the age, and
// facts already on the record. That is also why the STAGE cell is left empty:
// the phase is inferred from a transcript tail (cqPhase), and reading one per
// finished record on every five-second poll would cost the whole history.
func fleetPastRow(ctx *collectCtx, passes map[string]int, rec *state.Dispatch) fleetRow {
	goal, goalLabel := cqGoal(rec)
	return fleetRow{
		id:        rec.ID,
		kind:      "past",
		rank:      fleetRank("past", "normal", false),
		product:   ctx.productFor(rec),
		feature:   rec.Feature,
		repo:      rec.RepoName,
		ref:       cqRef(ctx.forge(rec.RepoPath), rec),
		pass:      passes[rec.ID],
		signal:    cqEnded(rec),
		tone:      "normal",
		why:       cqSentence(rec.StatusReason),
		goal:      goal,
		goalLabel: goalLabel,
		mode:      rec.Mode,
		acts:      cqActs(rec, "past"),
		moved:     fleetMoved(rec),
		started:   rec.CreatedAt,
		waited:    rec.UpdatedAt,
	}
}

// cqEnded is how a finished dispatcher finished, in the SIGNAL cell's width. It
// reports the furthest point its work actually reached — deployed over merged
// over marked — and "stopped" for a session that ended with none of them, which
// is the honest word for both a kill and a plain exit.
func cqEnded(rec *state.Dispatch) string {
	switch {
	case rec.DeployedAt != nil:
		return "deployed"
	case rec.PRState == "MERGED":
		return "merged"
	case rec.Status == state.StatusDone:
		return "marked shipped"
	}
	return "stopped"
}

// fleetMoved is when a dispatcher was last seen doing anything: the freshest of
// what it last wrote and when the hook last saved it.
//
// The LAST column asks one question of every row, so it must have one answer,
// and the max is right for all three kinds. For a blocked or turn-done
// dispatcher the transcript stops moving at the same instant UpdatedAt does, so
// they agree; for a working one the transcript is the only honest liveness
// signal (see cqLastWrite); and it degrades to UpdatedAt when the transcript
// cannot be read at all.
//
// What it is NOT is the dispatcher's age. That is CreatedAt, in its own column
// next to this one, because activity and lifetime are two facts and neither
// substitutes for the other — see fleetRow.started.
func fleetMoved(rec *state.Dispatch) time.Time {
	t := rec.UpdatedAt
	if mt := cqLastWrite(rec.TranscriptPath); mt.After(t) {
		return mt
	}
	return t
}

// fleetRepo strips a product prefix off a repo name — "cortiva-api" under
// product "cortiva" is "api" — because the PRODUCT column two cells to the left
// already said it. It is a display transform on a real name, never a rename:
// with no prefix to strip, or nothing left after stripping, the name stands.
func fleetRepo(repo, product string) string {
	p := strings.ToLower(product) + "-"
	if r := strings.ToLower(repo); strings.HasPrefix(r, p) && len(repo) > len(p) {
		return repo[len(p):]
	}
	return repo
}

// ---- the derived table ------------------------------------------------------------

// fleetHistory is the filter that swaps the live table for the finished one.
const fleetHistory = "history"

// fleetFilters is the cycle `f` walks. "all" is the resting state; the next
// three each narrow to a question the human might be asking, and the last
// leaves the fleet entirely for what it used to be — see fleetPast.
var fleetFilters = []string{"all", "wants you", "needs a look", "running", fleetHistory}

// fleetKeep reports whether a row survives a filter.
//
// "wants you" is every queue row rather than the design's `rank === 0 ||
// kind === 'queue'`: rank 0 is a queue row by construction, so the first half
// of that test never decided anything.
func fleetKeep(filter string, r fleetRow) bool {
	switch filter {
	case "wants you":
		return r.kind == "queue"
	case "needs a look":
		return r.rank <= 2
	case "running":
		return r.kind == "run"
	case fleetHistory:
		return r.kind == "past"
	}
	return r.kind != "past"
}

// fleetFilter is the active filter, defaulting to "all" so a zero model reads
// as "showing everything" rather than as "showing nothing".
func (m model) fleetFilter() string {
	if m.cqFilter == "" {
		return fleetFilters[0]
	}
	return m.cqFilter
}

// fleetAll is the in-flight table in display order: the ids the user has
// arranged first, then anything that has arrived since, in the collector's rank
// order. Rows acted on this session are suppressed until their record actually
// leaves the fleet — the kill or the merge takes a moment to land, and without
// this the row the user just cleared would reappear on the next 5s refresh.
//
// History is not in here, and must not be. Every "is anything in flight"
// question in the cockpit is this function's length — the dispatch form opens
// on an empty fleet, the headline counts it — and a machine with a month of
// finished dispatchers would answer all of them wrongly.
func (m model) fleetAll() []fleetRow {
	byID := make(map[string]fleetRow, len(fleet))
	for _, r := range fleet {
		if r.kind != "past" {
			byID[r.id] = r
		}
	}
	out := make([]fleetRow, 0, len(fleet))
	placed := make(map[string]bool, len(fleet))
	for _, id := range m.cqOrder {
		if r, ok := byID[id]; ok && !placed[id] && !m.cqSuppressed[id] {
			out = append(out, r)
			placed[id] = true
		}
	}
	for _, r := range fleet {
		if r.kind != "past" && !placed[r.id] && !m.cqSuppressed[r.id] {
			out = append(out, r)
		}
	}
	// A dispatch that has been asked for and has no record yet leads the table.
	// It is the newest thing on the screen and the one the human is looking for,
	// having just pressed the key that made it; and it has nothing to be ranked
	// by yet, so there is no order it could take its place in. See pending.go.
	if pend := m.pendingRows(); len(pend) > 0 {
		out = append(pend, out...)
	}
	return out
}

// fleetPast is the history table: every dispatcher whose session is over,
// newest first (fleetSort). The user's ordering does not apply — `s` rotates a
// queue, and there is no queue here — but the suppressed set still does, so a
// row cleared moments ago does not reappear under `h` while the record catches
// up.
func (m model) fleetPast() []fleetRow {
	out := make([]fleetRow, 0, len(fleet))
	for _, r := range fleet {
		if r.kind == "past" && !m.cqSuppressed[r.id] {
			out = append(out, r)
		}
	}
	return out
}

// fleetRows is what the table actually draws: fleetAll narrowed by `f`, or the
// history table when that is what `f` (or `h`) selected.
func (m model) fleetRows() []fleetRow {
	f := m.fleetFilter()
	if f == fleetHistory {
		return m.fleetPast()
	}
	all := m.fleetAll()
	if f == fleetFilters[0] {
		return all
	}
	out := make([]fleetRow, 0, len(all))
	for _, r := range all {
		if fleetKeep(f, r) {
			out = append(out, r)
		}
	}
	return out
}

// fleetSel is the row under the cursor, or false when the filter matches
// nothing.
func (m model) fleetSel() (fleetRow, bool) {
	rows := m.fleetRows()
	if len(rows) == 0 {
		return fleetRow{}, false
	}
	return rows[clampCursor(m.fleetCursor, len(rows))], true
}

// fleetCount tallies the three headline numbers over the rows on screen: what
// wants you, what is drifting, and what is simply running. They count the
// FILTERED set on purpose — the line sits above the table and describes it.
func fleetCount(rows []fleetRow) (wants, warn, clean int) {
	for _, r := range rows {
		switch {
		case r.kind == "queue":
			wants++
		case r.rank == 2:
			warn++
		default:
			clean++
		}
	}
	return wants, warn, clean
}

// fleetRunning is how many dispatchers are getting on with it, across the whole
// fleet rather than the filtered view — the unattended line is about the
// portfolio, not about what `f` happens to be showing.
func fleetRunning() int {
	n := 0
	for _, r := range fleet {
		if r.kind == "run" {
			n++
		}
	}
	return n
}
