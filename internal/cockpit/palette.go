// Package cockpit is the dispatch cockpit: a terminal port of the "Factory
// Cockpit v4" design — six keyboard-switched lenses over the portfolio of
// dispatchers, products, backlog, usage, decisions and velocity. The single
// product view is a panel inside the products lens rather than a lens of its
// own, which is why the digits stop at six.
//
// It is a stateless viewer over the real backends. The design's own portfolio
// is a mock and defines shape only: every data var starts empty (data.go) and
// is filled solely by the collectors, so a signal with no source behind it
// renders as an honest empty state rather than as the design's figures.
//
// The design is a monospace grid, so every `ch` width in the mock maps 1:1
// onto a terminal column. palette.go and layout.go are the shared vocabulary
// every lens renders with — colours, glyphs and fixed-width cell composition.
package cockpit

import "github.com/charmbracelet/lipgloss"

// The palette: the design's C table, as roles. Each constant names what a
// colour is FOR, and the active theme (theme.go) says what hex that is — the
// design's own values are the dark theme. Lenses pass these through fg/paint
// and never see a hex, which is what lets one table redraw the whole cockpit
// for a light terminal.
const (
	cWhite = "bright"
	cFg    = "text"
	cMid   = "mid"
	cDim   = "dim"
	cFaint = "faint"

	// A rule and a selected row share one hex in the design — its C.rule and
	// C.sel are both #18222f. They stay two roles because they are two jobs (a
	// border versus a highlight fill), earlier revisions spelled them apart,
	// and the light theme needs them apart again.
	cRule = "rule"
	cSel  = "selection"

	cRed   = "red"
	cAmber = "amber"
	// cBlue is the design's C.blue, which this revision swings to cyan: the
	// review state, the review slice of the usage split and the live agent rule.
	cBlue   = "blue"
	cGreen  = "green"
	cViolet = "violet"

	// cSearchHit lights the characters a `/` search matched. It is amber's
	// neighbour rather than amber itself: amber already means "this wants you"
	// on every table in the cockpit, and a search hit is a fact about the query,
	// not about the work. Nothing draws it yet — the search lands with other
	// work — but it is a role now so every theme already answers for it.
	cSearchHit = "search-hit"

	cTransparent = "" // no colour / default terminal background
)

// Colours the design spells as literals rather than through its C table. They
// are still colours the cockpit renders, so they are roles like the rest.
const (
	// cSurface is the design's panel background. The cockpit draws on the
	// terminal's own background, so this is only needed where a glyph sits ON a
	// text-coloured fill and takes the surface colour as its foreground — the
	// caret.
	cSurface = "surface"

	// cChainArrow is the arrow between two steps of the plan → act → observe →
	// ship chain. It reads a shade quieter than the steps it separates: an
	// unreached step is cFaint and the step in progress is cWhite, so the
	// arrows must not compete with either. (Until this revision one constant
	// served both the arrows and the unreached step; the design now separates
	// them.)
	cChainArrow = "chain-arrow"

	// Fills back text rather than carry it: product-board lane headers, the
	// queue's ready left edge and the non-leading velocity bar. Green and blue
	// take the full C-table hue in the dark design — this revision drops the
	// muted variants — but they stay named apart from cGreen/cBlue because they
	// are a different role and the design has separated them before.
	cFillGreen  = "fill-green"
	cFillViolet = "fill-violet"
	cFillBlue   = "fill-blue"
	cFillGrey   = "fill-grey"

	// cBoards is azure boards in sourceMeta. Its two siblings there need no
	// constant of their own: linear is cViolet and github is cMid.
	cBoards = "boards"
)

// styleCache memoises foreground/background styles so we build each colour's
// lipgloss.Style once rather than per cell per frame. Keyed by role, so
// setTheme empties them.
var (
	fgCache = map[string]lipgloss.Style{}
	bgCache = map[string]lipgloss.Style{}
)

// fg colours s with the given role. An empty role leaves the string untouched.
func fg(role, s string) string {
	if role == "" {
		return s
	}
	st, ok := fgCache[role]
	if !ok {
		st = lipgloss.NewStyle().Foreground(lipgloss.Color(hexOf(role)))
		fgCache[role] = st
	}
	return st.Render(s)
}

// paint colours s foreground role over background bgRole in one pass, so a
// selected row keeps its highlight underneath per-cell foreground colours.
func paint(role, bgRole, s string) string {
	if role == "" && bgRole == "" {
		return s
	}
	key := role + "|" + bgRole
	st, ok := bgCache[key]
	if !ok {
		st = lipgloss.NewStyle()
		if role != "" {
			st = st.Foreground(lipgloss.Color(hexOf(role)))
		}
		if bgRole != "" {
			st = st.Background(lipgloss.Color(hexOf(bgRole)))
		}
		bgCache[key] = st
	}
	return st.Render(s)
}

// stateMeta mirrors STATE_META: the glyph, label and colour for each state a
// dispatcher can be in on the floor.
type stateMeta struct {
	glyph string
	label string
	color string
}

var stateMetaBy = map[string]stateMeta{
	"blocked": {"■", "blocked", cRed},
	"claimed": {"◈", "claims done", cViolet},
	"needs":   {"◆", "needs you", cAmber},
	"review":  {"◇", "in review", cBlue},
	"working": {"●", "working", cGreen},
	"live":    {"✓", "live", cMid},
	"closed":  {"✓", "closed", cMid},
}

// bandColor mirrors BAND: DORA performance tiers.
var bandColor = map[string]string{
	"elite":  cGreen,
	"high":   cMid,
	"medium": cAmber,
	"low":    cRed,
}

// priColor mirrors PRI_COLOR for backlog ticket priority.
var priColor = map[string]string{
	"urgent": cRed,
	"high":   cAmber,
	"med":    cMid,
	"low":    cDim,
}
