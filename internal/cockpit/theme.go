package cockpit

// theme.go is what the palette's names mean on a given terminal. The design is
// a dark one, and the cockpit paints only foregrounds onto the terminal's own
// background — so on a light terminal the design's pale greys were pale grey
// on white, and a selected row was a slab of near-black. Every colour a lens
// asks for is therefore a ROLE (cFaint, cSel, cGreen …), and a theme is the
// table that turns roles into hex. fg and paint resolve through the active
// one at render time, so no lens knows which theme it is drawing in.
//
// Which theme is active is the theme MODE (config `theme`): "system" follows
// the light/dark switch, live while the cockpit is open, and a theme's name
// holds that theme. See the system-following half below.
//
// A theme is only a table, so a third-party one (catppuccin, say) is a table
// here and nothing else: `system` picks the registered theme for the side the
// switch is on, and a static mode names any theme by name.

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"claude-dispatcher/internal/appearance"
)

// theme is a role → hex table and the appearance it is drawn for.
type theme struct {
	name string
	// appearance is the terminal background the table is legible on — which
	// side of the system switch it answers for.
	appearance appearance.Appearance
	colors     map[string]string
}

// themeRoles is every role a theme must define. A theme missing one would
// render that role with no colour at all, so a test holds every registered
// theme to the whole list.
var themeRoles = []string{
	cWhite, cFg, cMid, cDim, cFaint,
	cRule, cSel,
	cRed, cAmber, cBlue, cGreen, cViolet,
	cSurface, cChainArrow,
	cFillGreen, cFillViolet, cFillBlue, cFillGrey,
	cBoards,
	bootLeader, cRamp1, cRamp2, cRamp3, cRamp4, cRamp5,
}

// darkTheme is the design, verbatim — the C table and the literals the design
// spells beside it. Why each value is what it is lives on its role in
// palette.go; this is only the answer.
var darkTheme = &theme{
	name:       "dark",
	appearance: appearance.Dark,
	colors: map[string]string{
		cWhite: "#f8fafc",
		cFg:    "#e2e8f0",
		cMid:   "#cbd5e1",
		cDim:   "#a3b1c2",
		cFaint: "#7d8da3",

		cRule: "#18222f",
		cSel:  "#18222f",

		cRed:    "#fb7185",
		cAmber:  "#fbbf24",
		cBlue:   "#22d3ee",
		cGreen:  "#34d399",
		cViolet: "#a78bfa",

		cSurface:    "#060b14",
		cChainArrow: "#5b6b80",

		cFillGreen:  "#34d399",
		cFillViolet: "#4c3f7a",
		cFillBlue:   "#22d3ee",
		cFillGrey:   "#5b6b80",

		cBoards: "#c084fc",

		bootLeader: "#243244",
		cRamp1:     "#a78bfa",
		cRamp2:     "#8ba3fb",
		cRamp3:     "#6ebcf5",
		cRamp4:     "#4ac8f2",
		cRamp5:     "#22d3ee",
	},
}

// lightTheme is the design turned over rather than inverted. The text ramp
// keeps its order of emphasis — cWhite is still the loudest word on the
// screen, it is just the darkest now — and every hue takes the shade of itself
// that reads on white (the 700s where the dark design uses a 300 or 400), so
// amber still means "this wants you" rather than becoming a yellow nobody can
// read. Roles that sit BEHIND text flip the other way: a rule and a selected
// row are a light grey, and the caret's glyph is the ground's white.
var lightTheme = &theme{
	name:       "light",
	appearance: appearance.Light,
	colors: map[string]string{
		cWhite: "#0f172a",
		cFg:    "#1e293b",
		cMid:   "#334155",
		cDim:   "#475569",
		cFaint: "#5b6b80",

		// Apart here where the dark design shares one hex: on white, a border
		// as heavy as a selection fill reads as a second selection.
		cRule: "#cbd5e1",
		cSel:  "#dbe3ed",

		cRed:    "#be123c",
		cAmber:  "#b45309",
		cBlue:   "#0e7490",
		cGreen:  "#047857",
		cViolet: "#6d28d9",

		// Still amber's neighbour: the olive side of it, since a paler amber
		// is exactly what stops reading on white.
		cSurface:    "#ffffff",
		cChainArrow: "#94a3b8",

		cFillGreen:  "#10b981",
		cFillViolet: "#c4b5fd",
		cFillBlue:   "#06b6d4",
		cFillGrey:   "#94a3b8",

		cBoards: "#9333ea",

		bootLeader: "#b4c0cf",
		cRamp1:     "#6d28d9",
		cRamp2:     "#553bc7",
		cRamp3:     "#3e4eb5",
		cRamp4:     "#2661a2",
		cRamp5:     "#0e7490",
	},
}

// themes is every theme a mode can name.
var themes = map[string]*theme{
	darkTheme.name:  darkTheme,
	lightTheme.name: lightTheme,
}

// themeForAppearance is the theme `system` uses for each side of the switch.
// Unknown draws the design as it was drawn, which is the dark one.
func themeForAppearance(a appearance.Appearance) *theme {
	if a == appearance.Light {
		return lightTheme
	}
	return darkTheme
}

// activeTheme is the table fg and paint resolve through. It is package state
// because they are free functions called from every lens, and it is only ever
// touched on Bubble Tea's event loop, which is also where View runs.
var activeTheme = darkTheme

// setTheme makes t the table every colour resolves through. The style caches
// are keyed by role, so they are emptied: a cached style holds the old hex.
func setTheme(t *theme) {
	if t == nil || t == activeTheme {
		return
	}
	activeTheme = t
	clear(fgCache)
	clear(bgCache)
}

// hexOf resolves a role through the active theme. Anything that is not a role
// is passed through, so a literal hex still renders as itself.
func hexOf(role string) string {
	if hex, ok := activeTheme.colors[role]; ok {
		return hex
	}
	return role
}

// ---- modes ------------------------------------------------------------------

// themeSystem is the mode that follows the light/dark switch.
const themeSystem = "system"

// themeModes is what the settings editor cycles through: system first, then
// the built-in pair, then any other registered theme by name.
func themeModes() []string {
	out := []string{themeSystem, lightTheme.name, darkTheme.name}
	var rest []string
	for name := range themes {
		if !slices.Contains(out, name) {
			rest = append(rest, name)
		}
	}
	slices.Sort(rest)
	return append(out, rest...)
}

// themeModeOf reads config's `theme`. Empty is system — the setting predates
// nobody, so a config without the line follows the switch — and a name that is
// not a theme is system too, reported rather than dropped.
func themeModeOf(v string) (mode string, ok bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch {
	case v == "" || v == themeSystem:
		return themeSystem, true
	case themes[v] != nil:
		return v, true
	}
	return themeSystem, false
}

// currentTheme is the theme the model's mode resolves to right now.
func (m model) currentTheme() *theme {
	if t := themes[m.themeMode]; t != nil {
		return t
	}
	return themeForAppearance(m.themeSystem)
}

// applyTheme makes the model's theme the active one.
func (m model) applyTheme() { setTheme(m.currentTheme()) }

// ---- following the switch -----------------------------------------------------
//
// Two things report the switch and neither is enough alone. The operating
// system knows the setting, but the cockpit can only ask it (see
// internal/appearance) — so it is polled, every themePollEvery. The terminal
// knows what it is actually painting, and a terminal that follows the system
// says so the moment it repaints: mode 2031 asks it to send `CSI ? 997 ; 1 n`
// (dark) or `; 2 n` (light) on every change, and `CSI ? 996 n` asks for the
// current one. tmux 3.6+ relays both, and ghostty, kitty, contour and foot
// send them. Where the terminal speaks, the switch is instant; where it does
// not, the poll catches it a couple of seconds later.
//
// When the two disagree — a terminal pinned to a dark theme on a light desktop
// — the newest CHANGE wins: the OS answer only counts when it differs from the
// OS's previous one, so a terminal that has said "dark" is not overruled every
// two seconds by a setting that has not moved.

// themePollEvery is how often the OS is asked. A portal read is a couple of
// milliseconds; this is the worst case for noticing a switch on a terminal
// that does not report one itself.
const themePollEvery = 2 * time.Second

// themeQueryTimeout bounds one OS read. A portal that has hung must not hold
// a goroutine, or the first frame, for longer than a beat.
const themeQueryTimeout = time.Second

const (
	// termThemeOn turns on the terminal's change reports and asks for the
	// current answer in one write.
	termThemeOn = "\x1b[?2031h\x1b[?996n"
	// termThemeOff is written on the way out: a report arriving at the shell
	// after we have gone is noise typed at a prompt.
	termThemeOff = "\x1b[?2031l"
)

// themeOSMsg is one OS read.
type themeOSMsg struct {
	appearance appearance.Appearance
	err        error
}

// queryAppearance reads the OS switch with the timeout applied.
func queryAppearance() (appearance.Appearance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), themeQueryTimeout)
	defer cancel()
	return appearance.Query(ctx)
}

// themePollCmd reads the OS switch after one poll interval.
func themePollCmd() tea.Cmd {
	return tea.Tick(themePollEvery, func(time.Time) tea.Msg {
		a, err := queryAppearance()
		return themeOSMsg{appearance: a, err: err}
	})
}

// termThemeCmd writes termThemeOn. It goes straight to stdout because Bubble
// Tea v1 has no command for a raw sequence — and it cannot tear a frame,
// because the renderer writes through the same *os.File, whose write lock
// holds for a whole Write however many syscalls it takes.
func termThemeCmd() tea.Cmd {
	return func() tea.Msg {
		_, _ = io.WriteString(os.Stdout, termThemeOn)
		return nil
	}
}

// stdoutIsTerminal says whether there is a terminal to talk to at all. Written
// to a pipe, a mode sequence is garbage in somebody's log.
func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// initTheme reads the configured mode and, for system, answers the switch
// before the first frame — the boot screen is drawn in whatever this decides.
// It reserves the poll for Init to issue, the way Run reserves the first
// load: Init cannot record that it started one.
func (m model) initTheme(configured string) model {
	mode, ok := themeModeOf(configured)
	m.themeMode = mode
	if !ok {
		note := "theme: no theme called " + strings.TrimSpace(configured) + " — following the system"
		if m.notice != "" {
			note = m.notice + " · " + note
		}
		m.notice = note
	}
	m.themeTerm = stdoutIsTerminal()
	if mode == themeSystem {
		a, err := queryAppearance()
		m.themeNoOS = errors.Is(err, appearance.ErrUnsupported)
		switch {
		case a != appearance.Unknown:
			m.themeOS, m.themeSystem = a, a
		case m.themeTerm && !lipgloss.HasDarkBackground():
			// Nothing to ask on this machine (a remote shell, a desktop with no
			// preference set): the terminal's own background is the next best
			// answer, and it is read once, here, before Bubble Tea owns the tty.
			m.themeSystem = appearance.Light
		}
		m.themePolling = !m.themeNoOS
	}
	m.applyTheme()
	return m
}

// themeFollowCmds are what system mode runs: the terminal's reports and the
// OS poll. Init issues the poll initTheme reserved; everywhere else a poll is
// started only when none is running, so switching modes back and forth in
// settings cannot stack loops.
func (m model) themeFollowCmds() tea.Cmd {
	if m.themeMode != themeSystem {
		return nil
	}
	var cmds []tea.Cmd
	if m.themeTerm {
		cmds = append(cmds, termThemeCmd())
	}
	if m.themePolling {
		cmds = append(cmds, themePollCmd())
	}
	return tea.Batch(cmds...)
}

// themeFollowTerm re-sends the terminal half only, for a return from a jump-in:
// the OS poll never stopped.
func (m model) themeFollowTerm() tea.Cmd {
	if m.themeMode != themeSystem || !m.themeTerm {
		return nil
	}
	return termThemeCmd()
}

// followSystem is entering system mode after startup (from settings).
func (m model) followSystem() (model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.themeTerm {
		cmds = append(cmds, termThemeCmd())
	}
	if !m.themePolling && !m.themeNoOS {
		m.themePolling = true
		cmds = append(cmds, themePollCmd())
	}
	return m, tea.Batch(cmds...)
}

// onThemeOS handles one OS read: a change is applied, and the poll re-arms for
// as long as the mode is system and the machine has something to ask.
func (m model) onThemeOS(msg themeOSMsg) (model, tea.Cmd) {
	m.themePolling = false
	if errors.Is(msg.err, appearance.ErrUnsupported) {
		m.themeNoOS = true
		return m, nil
	}
	if msg.err == nil && msg.appearance != appearance.Unknown && msg.appearance != m.themeOS {
		m.themeOS, m.themeSystem = msg.appearance, msg.appearance
		m.applyTheme()
	}
	if m.themeMode != themeSystem {
		return m, nil
	}
	m.themePolling = true
	return m, themePollCmd()
}

// termThemeReport recognises the terminal's `CSI ? 997 ; 1|2 n`. Bubble Tea v1
// does not know the sequence and hands it to Update as its unexported unknown-
// CSI message — a byte slice — so it is matched by shape and then exactly by
// content, which nothing else Bubble Tea delivers can be.
func termThemeReport(msg tea.Msg) (appearance.Appearance, bool) {
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
		return appearance.Unknown, false
	}
	switch string(v.Bytes()) {
	case "\x1b[?997;1n":
		return appearance.Dark, true
	case "\x1b[?997;2n":
		return appearance.Light, true
	}
	return appearance.Unknown, false
}
