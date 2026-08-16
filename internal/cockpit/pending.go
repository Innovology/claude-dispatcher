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
}

// pendingID is the row id a pending dispatch carries. It is namespaced so it
// can never collide with a record id: the cursor, the skip order and the
// suppressed set are all keyed by id, and a placeholder must not inherit the
// UI state of a real dispatcher (or leave any behind when it goes).
func pendingID(feature string) string { return "pending:" + feature }

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

// dropPending forgets an ask outright. This is the failure path: a launch that
// reported an error produced no record and never will, so the row has to go
// with the notice that says why.
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
		}
	}
	return m
}

// prunePending retires the notes a fresh snapshot has made unnecessary. It runs
// on every snapshot, before the cursor is re-keyed.
//
// Two things retire a note, and they are different questions:
//
//   - the table now carries a row for that feature — the record landed and is
//     saying more than we could. The cursor moves across with it, because the
//     human's selection was on this dispatcher, not on this row object;
//   - the launch reported success and a snapshot has since been built. The
//     records were re-read in that build, so whatever the table now shows for
//     this feature (including nothing, for a session that ended immediately) is
//     the record's own answer, and ours must stop competing with it.
//
// A note whose launch has not reported yet survives every snapshot: nothing has
// happened to make it untrue.
func (m model) prunePending() model {
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
		if !onTable && !p.settled {
			out = append(out, p)
			continue
		}
		if onTable && m.fleetSelID == pendingID(p.feature) {
			m.fleetSelID = id
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
	return fleetRow{
		id:        pendingID(p.feature),
		kind:      "run",
		rank:      fleetRank("run", "normal", false),
		product:   p.product,
		feature:   p.feature,
		repo:      p.repo,
		ref:       p.branch,
		signal:    startingSignal,
		tone:      "normal",
		why:       "Starting: its worktree and session are being made. Nothing has reported back yet.",
		goal:      goal,
		goalLabel: goalLabel,
		// No acts. Attach would have no session to hand over, and kill would have
		// no record to mark — an offered key that cannot act is the defect this
		// lens keeps finding in its own design.
		moved:  p.since,
		waited: p.since,
	}
}
