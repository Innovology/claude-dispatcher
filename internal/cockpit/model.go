package cockpit

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/version"
)

// lensOrder is the 1..6 lens bar, in order. Digit keys select by index.
//
// "product" is deliberately absent: it is still a legal m.lens value, but in v4
// the single-product view is a panel inside the products lens, opened with
// enter and closed with esc, so it has no digit of its own.
var lensOrder = []string{"floor", "products", "backlog", "usage", "decisions", "velocity"}

// shipFxState animates a feature merging and sliding off the floor list.
type shipFxState struct {
	feature, repo string
	frame         int
}

// confirmState is a pending y/n confirmation. kind drives doConfirm; the JS
// stored a closure, but a value-receiver model cannot, so we switch on kind.
type confirmState struct {
	label         string
	kind          string // "kill" | "ship" | "upgrade"
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

	// upgradeTo is the newest published release tag when this build is behind
	// it, "" when up to date, unknown, or running unstamped.
	upgradeTo string

	shipCursor int
	resumeOpen bool
	resumeText string

	backlogCursor int
	picked        map[string]bool
	srcFilter     string

	shipFx     *shipFxState
	justLanded string

	pane string

	// ---- products lens: the assignment editor --------------------------------
	// clMap is the working copy of repo→product; "" means unassigned. It is
	// seeded from the config and written back on every change, so the file
	// stays the source of truth.
	clOpen    bool
	clPane    string // "repos" | "products"
	clRepo    int
	clProd    int
	clMarked  map[string]bool
	clNaming  bool
	clNewName string
	clMap     map[string]string

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

	// install is how this build got onto the machine, and so what would upgrade
	// it — see version.Detect.
	install version.Install

	// away says the human is inside a dispatcher session this cockpit handed
	// them to and has not come back from yet. It is only ever set when the
	// handover exits on the way OUT rather than on the way back — see attach —
	// and it is what earns the return trip a full recheck rather than a redraw.
	away bool

	// relaunch says the cockpit is quitting only to come back as the build it
	// just installed. Run reads it after the program returns — the terminal has
	// to be handed back before we exec, or the new process inherits an alt
	// screen and raw mode it did not set up. See relaunch().
	relaunch bool

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

	cqDispatch bool // the dispatch form is up over a fleet that is not empty

	// The table's cursor. fleetSelID is the id of the row it is on: the fleet is
	// rebuilt on every poll, so an index alone would move the selection under
	// the reader's hands whenever a rank changed (see fleetSync).
	cqFilter    string // "" (all) | "wants you" | "needs a look" | "running"
	fleetCursor int
	fleetSelID  string

	cqUndo *cqUndoEntry // the last cleared row, restorable with `u`

	// ---- triage lens: the structured dispatch form -------------------------
	// The floor's freeform draft became four fields: where it lands, what it
	// does, when it is done, and whether it needs you between steps. See
	// dispatchx.go — cqDispatch is what says the form is open.
	dxField  dxFieldID // which line owns the keyboard
	dxFilter string    // WHERE: repo filter (runes from the key message)
	dxRepo   int       // cursor into dxRows(); clamped on read, reset by filtering
	dxWhat   string    // WHAT: the work, and the feature name
	dxGoal   string    // DONE WHEN: completion condition, optional
	dxAuto   bool      // AUTO: unattended (default true)
}

func newModel() model {
	return model{
		lens:         "floor",
		pane:         "list",
		rightTab:     "overview",
		srcFilter:    "all",
		picked:       map[string]bool{},
		clPane:       "repos",
		clMarked:     map[string]bool{},
		clMap:        map[string]string{},
		cqSuppressed: map[string]bool{},
		// How this build was installed cannot change while it runs, so it is
		// read once here rather than from the footer, which redraws on every
		// frame. Holding it on the model is also what lets a test drive the
		// brew/nix/unknown cases without being installed that way.
		install: version.Detect(),
		// The default dispatch is unattended. Without this the form opens on
		// "asks after every step", which is not the product's default posture.
		dxAuto: true,
	}
}

// ---- responsive fit tiers ---------------------------------------------------

type fitTier struct {
	showDetail, showSummary bool
	dec, vel                int
	cols                    string
}

var (
	fitWide     = fitTier{showDetail: true, showSummary: true, dec: 3, vel: 7, cols: "≥170 cols"}
	fitStandard = fitTier{showDetail: true, showSummary: true, dec: 2, vel: 5, cols: "110–170 cols"}
	fitNarrow   = fitTier{showDetail: false, showSummary: false, dec: 1, vel: 4, cols: "<110 cols"}
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
	// The upgrade check is about the binary, not the portfolio, so it runs even
	// on demo data — a config-less cockpit is still a real build.
	if m.cfg == nil {
		return upgradeCheckCmd() // no config — run on demo seed data
	}
	return tea.Batch(loadSnapshotCmd(m.cfg), trackRefreshCmd(m.cfg), waitState(m.stateCh), refreshTick(), upgradeCheckCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case snapshotMsg:
		applySnapshot(snapshot(msg))
		m.loading = false
		// The fleet is rebuilt from the fresh records; fold the user's ordering
		// and cleared set back onto it before anything renders, then re-key the
		// cursor onto the row it was on — a rank that changed in this refresh
		// would otherwise move the selection under the reader's hands.
		return m.cqReconcile().fleetSync(), nil

	case stateChangedMsg:
		return m, tea.Batch(loadSnapshotCmd(m.cfg), waitState(m.stateCh))

	case refreshTickMsg:
		// Re-checked on the poll so a cockpit left open for days still notices
		// a release; the version package's own cache decides when that costs a
		// network call.
		return m, tea.Batch(trackRefreshCmd(m.cfg), refreshTick(), upgradeCheckCmd())

	case upgradeMsg:
		if version.IsOutdated(msg.latest) {
			m.upgradeTo = msg.latest
		}
		return m, nil

	case upgradeRanMsg:
		if msg.err != nil {
			m.notice = upgradeFailed(m.install, msg.err)
			return m, nil
		}
		// The binary on disk is now the new one, but this process is still the
		// old one — a running program cannot become a different program. Quit
		// and let Run exec what was just installed, which keeps the human in the
		// same terminal rather than making them start the cockpit again.
		m.relaunch = true
		return m, tea.Quit

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
			// The handover never happened, so nobody went anywhere and there is
			// no return trip to wait for.
			m.notice = "attach failed: " + msg.err.Error()
			m.away = false
		}
		if m.cfg == nil {
			return m, nil
		}
		// A handover that exits on the way out has not returned anyone yet:
		// rechecking here would read the world at the instant the human left and
		// then have nothing to say when they actually come back, which is the
		// staleness this is meant to end. Wait for focus. Everywhere else this
		// message IS the return, so recheck now.
		if m.away {
			return m, loadSnapshotCmd(m.cfg)
		}
		return m, recheckCmd(m.cfg)

	case tea.FocusMsg:
		// Only a jump-in earns a recheck; focus on its own must not. A full
		// forge re-read every time the human alt-tabs back to the terminal is
		// how the gh quota gets burned (see internal/gh/cache.go), and a cockpit
		// that never left has nothing to catch up on.
		if !m.away {
			return m, nil
		}
		m.away = false
		if m.cfg == nil {
			return m, nil
		}
		return m, recheckCmd(m.cfg)

	case cqFlashMsg:
		// A stale timer must not clear a newer item — same guard as undoSeq.
		if m.cqFlash == "" || msg.seq != m.cqFlashSeq {
			return m, nil
		}
		// cqFlashDone re-syncs the cursor itself: the row it just cleared has
		// gone, so the selection has to move rather than follow the departed id.
		mm, cmd := m.cqFlashDone()
		return mm, cmd

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
	case "upgrade":
		// No undo is offered: the package manager owns what happens next, and
		// an offer to undo something we cannot undo would be a lie.
		return m, upgradeRunCmd(m.install)
	}
	return m, nil
}

func (m model) offerUndo(label string) (model, tea.Cmd) {
	m.undoSeq++
	m.undo = label
	seq := m.undoSeq
	return m, tea.Tick(undoLinger, func(time.Time) tea.Msg { return undoClearMsg{seq: seq} })
}
