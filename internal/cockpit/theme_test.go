package cockpit

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"claude-dispatcher/internal/appearance"
	"claude-dispatcher/internal/config"
)

// useTheme activates t for one test and puts the design back afterwards: the
// active theme is package state every other test renders through.
func useTheme(t *testing.T, th *theme) {
	t.Helper()
	setTheme(th)
	t.Cleanup(func() { setTheme(darkTheme) })
}

var hexRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// TestEveryThemeDefinesEveryRole: a role a theme leaves out renders as the
// role's NAME passed to lipgloss, which is no colour at all.
func TestEveryThemeDefinesEveryRole(t *testing.T) {
	for name, th := range themes {
		if th.name != name {
			t.Errorf("theme registered as %q calls itself %q", name, th.name)
		}
		for _, role := range themeRoles {
			if hex := th.colors[role]; !hexRe.MatchString(hex) {
				t.Errorf("theme %s: role %q = %q, want #rrggbb", name, role, hex)
			}
		}
		if len(th.colors) != len(themeRoles) {
			t.Errorf("theme %s defines %d colours for %d roles", name, len(th.colors), len(themeRoles))
		}
	}
}

func luminance(hex string) float64 {
	ch := func(s string) float64 {
		v, _ := strconv.ParseUint(s, 16, 8)
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(hex[1:3]) + 0.7152*ch(hex[3:5]) + 0.0722*ch(hex[5:7])
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestThemesAreLegibleOnTheirGround holds every theme's words to WCAG AA on the
// ground it is drawn for (the surface role) — the failure that asked for a
// light theme was the design's greys on a white terminal — and to a readable
// floor on a selected row, which keeps each cell's own colour over the fill.
func TestThemesAreLegibleOnTheirGround(t *testing.T) {
	words := []string{cWhite, cFg, cMid, cDim, cFaint, cRed, cAmber, cBlue, cGreen, cViolet, cSearchHit, cBoards}
	for name, th := range themes {
		ground, sel := th.colors[cSurface], th.colors[cSel]
		for _, role := range words {
			if r := contrast(th.colors[role], ground); r < 4.5 {
				t.Errorf("theme %s: %s on its ground is %.2f:1, want ≥ 4.5", name, role, r)
			}
			if r := contrast(th.colors[role], sel); r < 3.5 {
				t.Errorf("theme %s: %s on a selected row is %.2f:1, want ≥ 3.5", name, role, r)
			}
		}
		// The emphasis ramp keeps its order: louder roles stand further from
		// the ground, whichever direction that is.
		ramp := []string{cWhite, cFg, cMid, cDim, cFaint}
		for i := 1; i < len(ramp); i++ {
			if contrast(th.colors[ramp[i-1]], ground) <= contrast(th.colors[ramp[i]], ground) {
				t.Errorf("theme %s: %s is not louder than %s", name, ramp[i-1], ramp[i])
			}
		}
	}
}

// TestThemeRedrawsThroughRoles: switching the table changes what an already
// cached role renders as — the caches hold hex, so setTheme must empty them.
// It reads the cached styles rather than rendered text, because under go test
// lipgloss detects no colour support and renders no escape codes to compare.
func TestThemeRedrawsThroughRoles(t *testing.T) {
	colours := func() (string, string, string) {
		fg(cFaint, "x")
		paint(cWhite, cSel, "y")
		st := bgCache[cWhite+"|"+cSel]
		return fmt.Sprint(fgCache[cFaint].GetForeground()), fmt.Sprint(st.GetForeground()), fmt.Sprint(st.GetBackground())
	}
	for _, th := range []*theme{darkTheme, lightTheme, darkTheme} {
		useTheme(t, th)
		faint, text, sel := colours()
		if faint != th.colors[cFaint] || text != th.colors[cWhite] || sel != th.colors[cSel] {
			t.Errorf("theme %s drew faint %s, bright %s on %s — a stale style cache", th.name, faint, text, sel)
		}
	}
}

func TestThemeModeOf(t *testing.T) {
	for _, tc := range []struct {
		in   string
		mode string
		ok   bool
	}{
		{"", themeSystem, true},
		{"system", themeSystem, true},
		{" Light ", "light", true},
		{"dark", "dark", true},
		{"solarized-ish", themeSystem, false},
	} {
		mode, ok := themeModeOf(tc.in)
		if mode != tc.mode || ok != tc.ok {
			t.Errorf("themeModeOf(%q) = %q,%v want %q,%v", tc.in, mode, ok, tc.mode, tc.ok)
		}
	}
}

// fakeCSI stands in for Bubble Tea's unexported unknown-CSI message, which is
// a named []byte.
type fakeCSI []byte

func TestTermThemeReport(t *testing.T) {
	if a, ok := termThemeReport(fakeCSI("\x1b[?997;1n")); !ok || a != appearance.Dark {
		t.Errorf("997;1 = %v,%v want dark", a, ok)
	}
	if a, ok := termThemeReport(fakeCSI("\x1b[?997;2n")); !ok || a != appearance.Light {
		t.Errorf("997;2 = %v,%v want light", a, ok)
	}
	for _, msg := range []any{fakeCSI("\x1b[?997;3n"), fakeCSI("\x1b[5~"), "\x1b[?997;1n", nil, 42} {
		if _, ok := termThemeReport(msg); ok {
			t.Errorf("%#v read as a theme report", msg)
		}
	}
}

// TestSystemFollowsTheNewestChange: the OS poll applies only when the OS has
// moved, so a terminal that reported otherwise is not overruled every tick.
func TestSystemFollowsTheNewestChange(t *testing.T) {
	t.Cleanup(func() { setTheme(darkTheme) })
	m := newModel()
	m.themeOS, m.themeSystem = appearance.Dark, appearance.Dark
	m.applyTheme()

	m, cmd := m.onThemeOS(themeOSMsg{appearance: appearance.Light})
	if activeTheme != lightTheme || cmd == nil || !m.themePolling {
		t.Fatalf("OS switched to light: theme %s, re-armed %v", activeTheme.name, cmd != nil)
	}

	// The terminal says dark (a pinned terminal theme, say).
	mm, _ := m.Update(fakeCSI("\x1b[?997;1n"))
	m = mm.(model)
	if activeTheme != darkTheme {
		t.Fatalf("terminal report dark: theme %s", activeTheme.name)
	}
	// The OS has not moved; its unchanged answer must not flip it back.
	m, _ = m.onThemeOS(themeOSMsg{appearance: appearance.Light})
	if activeTheme != darkTheme {
		t.Error("an unchanged OS answer overruled the terminal's newer report")
	}

	// Nothing to ask: the poll stops for good.
	m, cmd = m.onThemeOS(themeOSMsg{err: appearance.ErrUnsupported})
	if cmd != nil || m.themePolling || !m.themeNoOS {
		t.Error("an unsupported OS kept polling")
	}
}

// TestStaticThemeIgnoresTheSwitch: light means light whatever the switch says.
func TestStaticThemeIgnoresTheSwitch(t *testing.T) {
	t.Cleanup(func() { setTheme(darkTheme) })
	m := newModel()
	m.themeMode = "light"
	m.applyTheme()
	mm, _ := m.Update(fakeCSI("\x1b[?997;1n"))
	m = mm.(model)
	if activeTheme != lightTheme {
		t.Errorf("static light followed a dark report: %s", activeTheme.name)
	}
	if _, cmd := m.onThemeOS(themeOSMsg{appearance: appearance.Dark}); cmd != nil {
		t.Error("static mode re-armed the OS poll")
	}
}

// TestSettingsCyclesTheTheme: enter on the theme field steps system → light →
// dark → system, redraws at once and writes config.toml each time.
func TestSettingsCyclesTheTheme(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { setTheme(darkTheme) })

	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{Roots: []string{"~/repos"}}
	m.lens = "products"
	m = press(m, ",")
	for m.settings.fields[m.settings.cursor].key != "theme" {
		m = press(m, "j")
	}
	if !strings.Contains(m.View(), "system · dark now") {
		t.Error("the theme field does not say what system resolves to")
	}

	for _, want := range []string{"light", "dark", "system"} {
		m = press(m, "enter")
		if m.themeMode != want {
			t.Fatalf("enter cycled to %q, want %q", m.themeMode, want)
		}
		if got, err := config.Load(); err != nil || got.Theme != want {
			t.Fatalf("saved theme = %q (%v), want %q", got.Theme, err, want)
		}
		if want == "light" && activeTheme != lightTheme {
			t.Error("choosing light did not redraw in the light theme")
		}
	}
	if m.settings.editing {
		t.Error("a choice field opened the text editor")
	}
}
