package cockpit

// pending.go covers the window between asking for a dispatch and the cockpit
// having anything to show for it.
//
// Dispatching is not instant, and none of it happens on the UI goroutine.
// launchCmd rescans the repo roots; dispatch.Launch then fetches the default
// branch as the remote sees it, materialises a worktree, inherits the repo's
// trust decision and starts a tmux session. Only after all of that does it
// write the record — and the record is what every list in this cockpit is built
// from. Until it lands there is literally nothing to draw, so the dispatch the
// human just asked for was invisible for as long as the fetch and the worktree
// took, and then appeared as though it had always been there.
//
// On a cockpit with nothing else in flight it was worse than invisible. viewCQ
// falls back to the dispatch form whenever the table is empty, so submitting
// put an empty form back on the screen: the same view the human had just filled
// in, wiped, with a notice above it. The one reading available is "that did not
// work", and the honest answer — it is starting — was on screen nowhere.
//
// A pending dispatch is the cockpit's own note of what it just asked for. It
// draws in the table like anything else, says "starting session" where a real
// row says what it wants, and offers no acts, because there is no session to
// attach to and no record to kill yet. It lives exactly as long as it is the
// only evidence there is — see prunePending.

import (
	"strings"
	"time"

	dispatchpkg "claude-dispatcher/internal/dispatch"
)

// startingSignal is what a dispatcher says while it is starting and has not
// reported in.
//
// It covers both halves of that wait — before the record exists (a pending row)
// and after it exists but before any hook has fired for it (a record still in
// state.StatusLaunching) — because to the human they are one wait with one
// answer: it is starting, and nothing has come back from it yet. The halves
// differ only in which of our own artefacts exists, which is not a distinction
// worth two words in a table cell.
const startingSignal = "starting session"

// failedSignal is what a dispatch that never started says in the cell the human
// reads for what a row wants. What this one wants is to be read: the reason
// sits beside it, in the detail panel's lead.
const failedSignal = "did not start"

// pendingDispatch is a dispatch this cockpit has asked for and has not yet seen
// a record for. Everything in it is known at the moment of asking; nothing here
// is read back from disk, which is the point — it exists precisely because
// there is nothing on disk to read.
type pendingDispatch struct {
	feature string
	repo    string
	product string
	branch  string
	prompt  string
	// since is when it was asked for, and is what the AGE column counts from.
	since time.Time
	// settled says the launch has finished and reported success, so a record for
	// it now exists. It does not mean the table has caught up — see prunePending.
	settled bool
	// settledAt is when it reported that, which is the instant the record was on
	// disk. A snapshot only speaks for this dispatch if it read the records
	// after it — see prunePending.
	settledAt time.Time
	// failed says the launch came back with an error, and reason is what it
	// said. The note then stops being a placeholder and becomes the only
	// account of the dispatch there is: it stays on the table, saying why,
	// until the human takes it off (see prunePending).
	failed bool
	reason string
}

// pendingID is the row id a pending dispatch carries. It is namespaced so it
// can never collide with a record id: the cursor, the skip order and the
// suppressed set are all keyed by id, and a placeholder must not inherit the
// UI state of a real dispatcher (or leave any behind when it goes).
func pendingID(feature string) string { return pendingPrefix + feature }

const pendingPrefix = "pending:"

// isPendingID reports whether a row id belongs to a note rather than a record.
// The acts a note offers cannot go through the record-keyed commands — there is
// no record to key by — so cqRun tells them apart by this.
func isPendingID(id string) bool { return strings.HasPrefix(id, pendingPrefix) }

// pendingFor builds the note from what the launch was given. The branch is
// composed the way dispatch.Launch composes it rather than copied from the
// form, so every caller gets the branch that will actually appear, not the
// branch one particular screen previewed.
func (m model) pendingFor(repo, feature, prompt string) pendingDispatch {
	product := ""
	if m.cfg != nil {
		product = m.cfg.ProductFor(repo)
	}
	if product == "" {
		// The same word collectCtx.productFor files an unmapped repo under, so
		// the placeholder and the row that replaces it sit in one column.
		product = clUnassigned
	}
	return pendingDispatch{
		feature: feature,
		repo:    repo,
		product: product,
		branch:  "feature/" + dispatchpkg.Slugify(feature),
		prompt:  prompt,
		since:   time.Now(),
	}
}

// markPending records an ask. A second ask under a name already pending
// replaces the first rather than stacking a second row: the feature name is the
// key throughout this product — dispatch.Launch refuses a name already live —
// so two rows would be two claims about one thing.
func (m model) markPending(p pendingDispatch) model {
	out := make([]pendingDispatch, 0, len(m.pending)+1)
	for _, q := range m.pending {
		if q.feature != p.feature {
			out = append(out, q)
		}
	}
	m.pending = append(out, p)
	return m
}

// failPending turns an ask into the report of a launch that did not happen.
//
// The row used to be deleted here, and deleting it is the whole of the bug
// this file is named after. A launch can fail before dispatch.Launch writes
// anything at all — a feature already live, a worktree its last session left on
// another branch, a supervisor that would not take the command — so there is no
// record to fall back to, nothing in history, and no worktree: the row was the
// only thing on screen that knew this dispatch had ever been asked for. Taking
// it away left a footer notice against a table that looked exactly as it had
// before, which reads as "nothing happened", and on an otherwise empty fleet
// put the blank dispatch form back up, which reads as "that did not work"
// without ever saying what did not work.
//
// So the note stays and says so. reason is the launch's own words.
func (m model) failPending(feature, reason string) model {
	for i := range m.pending {
		if m.pending[i].feature == feature {
			m.pending[i].failed = true
			m.pending[i].reason = reason
			m.pending[i].settled = false
		}
	}
	return m
}

// dropPending forgets an ask outright. It is what dismissing a failed row does:
// the human has read why it did not start, and the note has nothing left to
// say. Nothing else removes a failed note — see prunePending.
func (m model) dropPending(feature string) model {
	out := m.pending[:0:0]
	for _, p := range m.pending {
		if p.feature != feature {
			out = append(out, p)
		}
	}
	m.pending = out
	return m
}

// settlePending marks an ask as launched. The row stays: the record exists now,
// but the table is still the one built before it did, and dropping the
// placeholder here would blank the row for however long the next snapshot takes
// — which is the disappearance this whole file exists to prevent.
func (m model) settlePending(feature string) model {
	for i := range m.pending {
		if m.pending[i].feature == feature {
			m.pending[i].settled = true
			m.pending[i].settledAt = time.Now()
		}
	}
	return m
}

// prunePending retires the notes a fresh snapshot has made unnecessary. It runs
// on every published snapshot, before the cursor is re-keyed. recordsAt is when
// that snapshot read the dispatch records.
//
// Two things retire a note, and they are different questions:
//
//   - the table now carries a row for that feature — the record landed and is
//     saying more than we could. The cursor moves across with it, because the
//     human's selection was on this dispatcher, not on this row object;
//   - the launch reported success, and a snapshot has since read the records.
//     Whatever the table now shows for this feature (including nothing, for a
//     session that ended immediately) is the record's own answer, and ours must
//     stop competing with it.
//
// "Since" is the load-bearing word, and it means since the record was written
// — not since the snapshot landed. A load takes seconds; one that read the directory
// before this dispatch existed can return long after it and know nothing about
// it, and retiring the note on that would take the row away and leave nothing
// in its place. That is the disappearance this file exists to prevent, in the
// one gap the first version left open: the note went, the record's row was not
// in the stale fleet that arrived, and on an otherwise empty table the lens fell
// back to the dispatch form — the screen the human had just submitted, blank.
//
// A note whose launch has not reported yet survives every snapshot: nothing has
// happened to make it untrue.
func (m model) prunePending(recordsAt time.Time) model {
	if len(m.pending) == 0 {
		return m
	}
	rowFor := make(map[string]string, len(fleet))
	for _, r := range fleet {
		rowFor[r.feature] = r.id
	}
	out := m.pending[:0:0]
	for _, p := range m.pending {
		id, onTable := rowFor[p.feature]
		switch {
		case p.failed && !onTable:
			// A failed note answers to nothing: the thing it reports is that
			// there is nothing for a snapshot to find, so no snapshot can
			// retire it. Only the human takes it off (x, cqRun).
			//
			// A launch that got far enough to write a record is the exception,
			// and it falls through to the case below: the record says the same
			// thing with more behind it, and two rows for one dispatch would be
			// this cockpit contradicting itself.
			out = append(out, p)
		case onTable:
			if m.fleetSelID == pendingID(p.feature) {
				m.fleetSelID = id
			}
		case p.settled && recordsAt.After(p.settledAt):
			// The records were read after the launch wrote one. Nothing to hand
			// the cursor to — this feature is genuinely not on the table.
		default:
			out = append(out, p)
		}
	}
	m.pending = out
	return m
}

// pendingRows is the placeholder rows the table should draw right now.
//
// A note whose feature is already on the table is skipped rather than drawn,
// so the two can never both be up: prunePending is what retires it, and this is
// what makes an unpruned moment harmless.
func (m model) pendingRows() []fleetRow {
	if len(m.pending) == 0 {
		return nil
	}
	onTable := make(map[string]bool, len(fleet))
	for _, r := range fleet {
		onTable[r.feature] = true
	}
	out := make([]fleetRow, 0, len(m.pending))
	for _, p := range m.pending {
		if onTable[p.feature] {
			continue
		}
		out = append(out, pendingRow(p))
	}
	return out
}

// pendingRow draws the note as a table row.
//
// It is a "run" row: it is not asking for anything, so it must not sit in the
// half of the table that means someone is waiting on you, and it must not be
// counted among the rows that do. Every cell is either known from the ask or
// left empty — there is no stage to infer, no turn to count, no context to
// read, and no PR — and the empty ones render as the same dashes and blanks a
// real row shows when its source has nothing to say.
func pendingRow(p pendingDispatch) fleetRow {
	goal, goalLabel := "", "goal"
	if s := cqFirstSentence(p.prompt); s != "" {
		// The same quote, under the same label, that cqGoal will show the moment
		// the record takes over — so the panel does not appear to change its mind
		// about what this dispatcher was asked to do.
		goal, goalLabel = s, "prompt"
	}
	signal, tone, why := startingSignal, "normal",
		"Starting: its worktree and session are being made. Nothing has reported back yet."
	rank := fleetRank("run", "normal")
	var acts []cqAct
	if p.failed {
		// The reason is the launch's own words, in the cell the human is already
		// reading for what a dispatcher wants — because what this one wants is
		// to be told what went wrong.
		//
		// Rank 0 for the red glyph; it leads the table either way, because every
		// note does (fleetAll). It stays a "run" row all the same: the queue's
		// own keys act on records — park writes a reason to one, skip rotates
		// the ask queue — and there is no record here for either to reach.
		//
		// x is the only act, and all it does is take the row off. Which is
		// precisely what used to happen by itself, with nothing said.
		signal, tone, why, rank = failedSignal, "red", p.reason, 0
		acts = []cqAct{{k: "x", d: "dismiss",
			ok: "dismissed \"" + p.feature + "\"", keep: true}}
	}
	return fleetRow{
		id:        pendingID(p.feature),
		kind:      "run",
		rank:      rank,
		product:   p.product,
		feature:   p.feature,
		repo:      p.repo,
		ref:       p.branch,
		signal:    signal,
		tone:      tone,
		why:       why,
		acts:      acts,
		goal:      goal,
		goalLabel: goalLabel,
		// A starting note offers no acts at all: attach would have no session to
		// hand over and kill no record to mark, and an offered key that cannot
		// act is the defect this lens keeps finding in its own design. A failed
		// one offers the single act that can be honoured with no record behind
		// it — see above.
		// Both ages count from the same instant, which is the truth about a
		// dispatcher that has existed for as long as it has been silent.
		moved:   p.since,
		started: p.since,
		waited:  p.since,
	}
}
