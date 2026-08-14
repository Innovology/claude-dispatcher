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

// The palette, verbatim from the design's C table. Referenced by hex through
// fg/bg so a lens can use any colour the mock uses without a named constant.
const (
	cWhite = "#f8fafc"
	cFg    = "#e2e8f0"
	cMid   = "#cbd5e1"
	cDim   = "#a3b1c2"
	cFaint = "#7d8da3"

	// A rule and a selected row share one hex in this revision — the design's
	// C.rule and C.sel are both #18222f. They stay two constants because they
	// are two roles (a border versus a highlight fill) and earlier revisions
	// spelled them apart; collapsing them would lose that.
	cRule = "#18222f"
	cSel  = "#18222f"

	cRed   = "#fb7185"
	cAmber = "#fbbf24"
	// cBlue is the design's C.blue, which this revision swings to cyan: the
	// review state, the review slice of the usage split and the live agent rule.
	cBlue   = "#22d3ee"
	cGreen  = "#34d399"
	cViolet = "#a78bfa"

	cTransparent = "" // no colour / default terminal background
)

// Colours the design spells as literals rather than through its C table. They
// are still colours the cockpit renders, so they live here and nowhere else.
const (
	// cSurface is the design's panel background. The cockpit draws on the
	// terminal's own background, so this is only needed where a glyph sits ON a
	// light fill and takes the surface colour as its foreground — the caret.
	cSurface = "#060b14"

	// cChainArrow is the arrow between two steps of the plan → act → observe →
	// ship chain. It reads a shade above the steps it separates: an unreached
	// step is cFaint and the step in progress is cWhite, so the arrows must not
	// compete with either. (Until this revision one constant served both the
	// arrows and the unreached step; the design now separates them.)
	cChainArrow = "#5b6b80"

	// Fills back text rather than carry it: product-board lane headers, the
	// queue's ready left edge and the non-leading velocity bar. Green and blue
	// now take the full C-table hue — this revision drops the muted variants —
	// but they stay named apart from cGreen/cBlue because they are a different
	// role and the design has separated them before.
	cFillGreen  = "#34d399"
	cFillViolet = "#4c3f7a"
	cFillBlue   = "#22d3ee"
	cFillGrey   = "#5b6b80"

	// cBoards is azure boards in sourceMeta. Its two siblings there need no
	// constant of their own: linear is cViolet and github is cMid.
	cBoards = "#c084fc"
)

// styleCache memoises foreground/background styles so we build each colour's
// lipgloss.Style once rather than per cell per frame.
var (
	fgCache = map[string]lipgloss.Style{}
	bgCache = map[string]lipgloss.Style{}
)

// fg colours s with the given hex. An empty hex leaves the string untouched.
func fg(hex, s string) string {
	if hex == "" {
		return s
	}
	st, ok := fgCache[hex]
	if !ok {
		st = lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
		fgCache[hex] = st
	}
	return st.Render(s)
}

// paint colours s foreground hex over background bgHex in one pass, so a
// selected row keeps its highlight underneath per-cell foreground colours.
func paint(hex, bgHex, s string) string {
	if hex == "" && bgHex == "" {
		return s
	}
	key := hex + "|" + bgHex
	st, ok := bgCache[key]
	if !ok {
		st = lipgloss.NewStyle()
		if hex != "" {
			st = st.Foreground(lipgloss.Color(hex))
		}
		if bgHex != "" {
			st = st.Background(lipgloss.Color(bgHex))
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
