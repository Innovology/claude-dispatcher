// Package cockpit is the v2 dispatch cockpit: a faithful terminal port of the
// "Factory Cockpit v2" design — eight keyboard-switched lenses over the
// portfolio of dispatchers, products, backlog, usage, decisions and velocity.
//
// It is a stateless viewer. This first cut is seeded with the design's own
// representative data (see seed.go and the seed_*.go files); real backend
// wiring is layered on top of the same view model later.
//
// The design is a monospace grid, so every `ch` width in the mock maps 1:1
// onto a terminal column. palette.go and layout.go are the shared vocabulary
// every lens renders with — colours, glyphs and fixed-width cell composition.
package cockpit

import "github.com/charmbracelet/lipgloss"

// The palette, verbatim from the design's C table. Referenced by hex through
// fg/bg so a lens can use any colour the mock uses without a named constant.
const (
	cWhite  = "#ffffff"
	cFg     = "#e3e8ee"
	cMid    = "#a1acb8"
	cDim    = "#7f8b97"
	cFaint  = "#737f8b"
	cRule   = "#1f2630"
	cSel    = "#171d25"
	cRed    = "#dd5f5a"
	cAmber  = "#d9a24e"
	cBlue   = "#6a9de2"
	cGreen  = "#4cbb8d"
	cViolet = "#9c8ee2"

	cTransparent = "" // no colour / default terminal background
)

// Colours the design spells as literals rather than through its C table. They
// are still colours the cockpit renders, so they live here and nowhere else.
const (
	// cSurface is the design's panel background. The cockpit draws on the
	// terminal's own background, so this is only needed where a glyph sits ON a
	// light fill and takes the surface colour as its foreground — the caret.
	cSurface = "#0f1319"

	// cChainOff is an unreached step of the plan → act → observe → ship chain,
	// and the arrows between every step. The step in progress is cWhite.
	cChainOff = "#38424f"

	// Muted fills back text rather than carry it, so they sit far darker than
	// the C-table hue they echo: product-board lane headers, the queue's ready
	// left edge and the non-leading velocity bar.
	cFillGreen  = "#2f6b56"
	cFillViolet = "#544a7e"
	cFillBlue   = "#33527e"
	cFillGrey   = "#48525f"

	// cBoards is azure boards in sourceMeta. Its two siblings there need no
	// constant of their own: linear is cViolet and github is cMid.
	cBoards = "#a878cf"
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
