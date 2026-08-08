package cockpit

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
)

// lensOrder is the 1..8 lens bar, in order. Digit keys select by index.
var lensOrder = []string{"floor", "products", "product", "queue", "backlog", "usage", "decisions", "velocity"}

// shipFxState animates a feature merging and sliding off the floor list.
type shipFxState struct {
	feature, repo string
	frame         int
}

// confirmState is a pending y/n confirmation. kind drives doConfirm; the JS
// stored a closure, but a value-receiver model cannot, so we switch on kind.
type confirmState struct {
	label         string
	kind          string // "kill" | "ship"
	feature, repo string
	features      []string // kill targets (marked set, or the one selected)
}

// model is the whole cockpit. Every lens reads and writes these fields; each
// lens owns the subset named after it (product* for the product lens, etc.).
type model struct {
	width, height int

	// live-data plumbing (nil cfg → stay on demo seed data)
	cfg          *config.Config
	stateCh      chan struct{}
	loading      bool
	settings     *settingsState
	dispatchForm *dispatchForm

	// key is the untouched message for the key being handled. Routing is by
	// name (handleKey takes a string), but text input must come from the
	// message itself — see typedText in keys.go.
	key tea.KeyMsg

	lens          string
	productCursor int

	paletteOpen   bool
	paletteText   string
	paletteCursor int

	notice string

	shipCursor int
	resumeOpen bool
	resumeText string

	backlogCursor int
	picked        map[string]bool
	srcFilter     string

	shipFx     *shipFxState
	justLanded string

	pane string

	decRepo      int
	decCursor    int
	pluginCursor int

	rightTab     string
	reviewCursor int
	reviewOpen   bool

	helpOpen bool

	confirm *confirmState
	undo    string
	undoSeq int

	// ---- triage lens: the command queue -------------------------------------
	// The queue itself is derived from the records on every read (cq.go), so the
	// model holds only what the user did to it: the order they left it in, what
	// they have already acted on, and what they are typing.
	cqOrder      []string        // item ids, front first; `s` rotates, new asks land at the back
	cqSuppressed map[string]bool // acted on, hidden until the record leaves the queue for real
	cqCleared    int             // "N things handled" this session

	cqFlash     string // an act's confirmation, held on screen for cqFlashLinger
	cqFlashKeep bool   // the flashing act did not clear the item (attach)
	cqFlashID   string // the item it was fired on — flashes clear by id, never by position
	cqFlashSeq  int    // generation guard: only the newest flash's tick may fire

	cqWork     bool   // the working view (`w`)
	cqDispatch bool   // draft mode: the prompt is up over a queue that is not empty
	cqDraft    string // the draft text (runes from the key message, never the key name)

	cqUndo *cqUndoEntry // the last cleared row, restorable with `u`
}

func newModel() model {
	return model{
		lens:         "floor",
		pane:         "list",
		rightTab:     "review",
		srcFilter:    "all",
		picked:       map[string]bool{},
		cqSuppressed: map[string]bool{},
	}
}

// ---- responsive fit tiers ---------------------------------------------------

type fitTier struct {
	showDetail, showSummary bool
	dec, vel, velTilePct    int
	lanePct                 int
	cols                    string
}

var (
	fitWide     = fitTier{showDetail: true, showSummary: true, dec: 3, vel: 7, velTilePct: 25, lanePct: 25, cols: "≥170 cols"}
	fitStandard = fitTier{showDetail: true, showSummary: true, dec: 2, vel: 5, velTilePct: 50, lanePct: 50, cols: "110–170 cols"}
	fitNarrow   = fitTier{showDetail: false, showSummary: false, dec: 1, vel: 4, velTilePct: 50, lanePct: 100, cols: "<110 cols"}
)

// fit picks the tier for the current terminal width.
func (m model) fit() fitTier {
	switch {
	case m.width >= 170:
		return fitWide
	case m.width >= 110:
		return fitStandard
	default:
		return fitNarrow
	}
}

// ---- animation messages -----------------------------------------------------

type (
	shipTickMsg  struct{}
	landClearMsg struct{}
	undoClearMsg struct{ seq int }
	// cqFlashMsg ends an act's on-screen confirmation. seq identifies the flash
	// it was scheduled for, so a superseded tick cannot clear a newer one.
	cqFlashMsg struct{ seq int }
)

func (m model) Init() tea.Cmd {
	if m.cfg == nil {
		return nil // no config — run on demo seed data
	}
	return tea.Batch(loadSnapshotCmd(m.cfg), trackRefreshCmd(m.cfg), waitState(m.stateCh), refreshTick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case snapshotMsg:
		applySnapshot(snapshot(msg))
		m.loading = false
		// The queue is rebuilt from the fresh records; fold the user's ordering
		// and cleared set back onto it before anything renders.
		return m.cqReconcile(), nil

	case stateChangedMsg:
		return m, tea.Batch(loadSnapshotCmd(m.cfg), waitState(m.stateCh))

	case refreshTickMsg:
		return m, tea.Batch(trackRefreshCmd(m.cfg), refreshTick())

	case trackedMsg:
		return m, loadSnapshotCmd(m.cfg)

	case actionMsg:
		m.notice = msg.notice
		if m.cfg != nil {
			return m, loadSnapshotCmd(m.cfg)
		}
		return m, nil

	case attachReturnedMsg:
		m.notice = ""
		if msg.err != nil {
			m.notice = "attach failed: " + msg.err.Error()
		}
		if m.cfg != nil {
			return m, loadSnapshotCmd(m.cfg)
		}
		return m, nil

	case cqFlashMsg:
		// A stale timer must not clear a newer item — same guard as undoSeq.
		if m.cqFlash == "" || msg.seq != m.cqFlashSeq {
			return m, nil
		}
		return m.cqFlashDone()

	case shipTickMsg:
		return m.advanceShip()

	case landClearMsg:
		m.justLanded = ""
		return m, nil

	case undoClearMsg:
		if msg.seq == m.undoSeq {
			m.undo = ""
		}
		return m, nil

	case tea.KeyMsg:
		// Ctrl-L: force a full clear + repaint, the universal "redraw" key —
		// handy after a tmux attach or any terminal noise garbles the screen.
		if msg.String() == "ctrl+l" {
			return m, tea.ClearScreen
		}
		m.key = msg
		return m.handleKey(msg.String())
	}
	return m, nil
}

// ---- confirm / undo / ship --------------------------------------------------

func (m model) doConfirm() (model, tea.Cmd) {
	c := m.confirm
	m.confirm = nil
	if c == nil {
		return m, nil
	}
	switch c.kind {
	case "kill":
		feats := c.features
		if len(feats) == 0 {
			feats = []string{c.feature}
		}
		mm, undo := m.offerUndo(c.label)
		return mm, tea.Batch(killCmd(feats), undo)
	case "ship":
		x := dispatch{feature: c.feature, repo: c.repo}
		mm, tick := m.startShip(x)
		mm2, undo := mm.offerUndo("ship " + c.feature)
		return mm2, tea.Batch(tick, shipCmd(c.feature), undo)
	}
	return m, nil
}

func (m model) offerUndo(label string) (model, tea.Cmd) {
	m.undoSeq++
	m.undo = label
	seq := m.undoSeq
	return m, tea.Tick(undoLinger, func(time.Time) tea.Msg { return undoClearMsg{seq: seq} })
}
