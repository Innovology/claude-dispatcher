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
	count         int      // marked count, for "kill N marked"
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

	lens          string
	cursor        int
	productCursor int
	workingOpen   bool

	paletteOpen   bool
	paletteText   string
	paletteCursor int

	replyText    string
	replyFocused bool

	notice  string
	groupBy string

	shipCursor int
	resumeOpen bool
	resumeText string

	modelsBy map[string]string // feature → model override
	modesBy  map[string]string // feature → permission-mode override

	backlogCursor int
	picked        map[string]bool
	srcFilter     string

	shipFx     *shipFxState
	shipped    map[string]bool
	justLanded string

	narrowPane   string
	pane         string
	stackCursor  int
	ticketCursor int

	decRepo      int
	decCursor    int
	pluginCursor int

	rightTab     string
	reviewCursor int
	reviewOpen   bool

	filter     string
	filterOpen bool
	marked     map[string]bool
	helpOpen   bool
	diffOpen   bool

	follow   bool
	tailN    int
	confirm  *confirmState
	undo     string
	undoSeq  int
	newCount int
}

func newModel() model {
	return model{
		lens:       "floor",
		groupBy:    "product",
		pane:       "list",
		narrowPane: "list",
		rightTab:   "review",
		srcFilter:  "all",
		tailN:      3,
		newCount:   2,
		modelsBy:   map[string]string{},
		modesBy:    map[string]string{},
		picked:     map[string]bool{},
		shipped:    map[string]bool{},
		marked:     map[string]bool{},
	}
}

// ---- responsive fit tiers ---------------------------------------------------

type fitTier struct {
	listPct                                              int
	showDetail, showStrip, showAgents, showSummary, tail bool
	stats, dec, vel, velTilePct, lanePct                 int
	cols                                                 string
}

var (
	fitWide     = fitTier{listPct: 44, showDetail: true, showStrip: true, showAgents: true, showSummary: true, tail: true, stats: 6, dec: 3, vel: 7, velTilePct: 25, lanePct: 25, cols: "≥170 cols"}
	fitStandard = fitTier{listPct: 40, showDetail: true, showStrip: true, showAgents: false, showSummary: true, tail: false, stats: 3, dec: 2, vel: 5, velTilePct: 50, lanePct: 50, cols: "110–170 cols"}
	fitNarrow   = fitTier{listPct: 100, showDetail: false, showStrip: false, showAgents: false, showSummary: false, tail: false, stats: 3, dec: 1, vel: 4, velTilePct: 50, lanePct: 100, cols: "<110 cols"}
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

func (m model) groupByMode() string {
	if m.groupBy != "" {
		return m.groupBy
	}
	return "product"
}

func (m model) modelOf(x dispatch) string {
	if v := m.modelsBy[x.feature]; v != "" {
		return v
	}
	if len(x.agents) > 0 && x.agents[0].model != "" {
		return x.agents[0].model
	}
	return "sonnet"
}

func (m model) modeOf(x dispatch) string {
	if v := m.modesBy[x.feature]; v != "" {
		return v
	}
	if x.mode != "" {
		return x.mode
	}
	return "edits"
}

// ---- animation messages -----------------------------------------------------

type (
	followTickMsg struct{}
	shipTickMsg   struct{}
	landClearMsg  struct{}
	undoClearMsg  struct{ seq int }
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
		return m, nil

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

	case followTickMsg:
		if !m.follow {
			return m, nil
		}
		m.tailN++
		return m, followTick()

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
		m.marked = map[string]bool{}
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
