package cockpit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/appearance"
)

// countOSReads swaps the OS reader for one that answers `a` and counts how
// often it was asked.
func countOSReads(t *testing.T, a appearance.Appearance, err error) *int {
	t.Helper()
	calls := 0
	prev := queryAppearance
	queryAppearance = func() (appearance.Appearance, error) {
		calls++
		return a, err
	}
	t.Cleanup(func() { queryAppearance = prev })
	return &calls
}

func writeMode(t *testing.T, word string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mode")
	if err := os.WriteFile(path, []byte(word), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A named file answers, and then the OS is not asked at all. That is the point
// of naming one: on the reporting desktop the portal says "no preference"
// while the switch is plainly light, and the file its tmux config already
// reads says so for free — a read rather than a subprocess.
func TestANamedAppearanceFileIsAskedInsteadOfTheOS(t *testing.T) {
	calls := countOSReads(t, appearance.Dark, nil)

	got, err := queryAppearanceFrom(writeMode(t, "light"))
	if err != nil || got != appearance.Light {
		t.Errorf("queryAppearanceFrom = %v, %v — want light", got, err)
	}
	if *calls != 0 {
		t.Errorf("the OS was asked %d times while a file was answering", *calls)
	}
}

// A file that cannot answer is not an answer: the OS is asked exactly as if
// none had been named, so a typo'd path degrades to the old behaviour instead
// of pinning a theme.
func TestAnUnusableAppearanceFileFallsThroughToTheOS(t *testing.T) {
	for _, path := range []string{
		filepath.Join(t.TempDir(), "not-there"),
		writeMode(t, "solarized"),
		"",
	} {
		calls := countOSReads(t, appearance.Dark, nil)
		got, err := queryAppearanceFrom(path)
		if err != nil || got != appearance.Dark {
			t.Errorf("%q: queryAppearanceFrom = %v, %v — want the OS's dark", path, got, err)
		}
		if *calls != 1 {
			t.Errorf("%q: the OS was asked %d times, want once", path, *calls)
		}
	}
}

// "Nothing to ask" stops the poll; "asked, no preference" used to leave it
// spinning at two seconds forever. Measured on a NixOS/niri desktop: a busctl
// process every two seconds for an answer that never changes. It still asks,
// because a preference can be set later — it just stops spinning.
func TestAPollThatKeepsGettingNoAnswerSlowsDown(t *testing.T) {
	m := model{themeMode: themeSystem, themePolling: true}
	if got := m.themePollNext(); got != themePollEvery {
		t.Fatalf("a fresh poll waits %v, want %v", got, themePollEvery)
	}

	for i := 0; i < themeQuietAfter; i++ {
		m, _ = m.onThemeOS(themeOSMsg{appearance: appearance.Unknown})
	}
	if got := m.themePollNext(); got != themePollQuiet {
		t.Errorf("after %d answers of no preference the poll waits %v, want %v",
			themeQuietAfter, got, themePollQuiet)
	}

	// A desktop that starts answering is the switch working again.
	m, _ = m.onThemeOS(themeOSMsg{appearance: appearance.Light})
	if got := m.themePollNext(); got != themePollEvery {
		t.Errorf("after a real answer the poll waits %v, want %v back", got, themePollEvery)
	}
	if m.themeOS != appearance.Light {
		t.Errorf("the real answer was not taken: themeOS = %v", m.themeOS)
	}
}

// An error is as much "no answer" as Unknown is, and must not be mistaken for
// one either way: a portal that is timing out slows the poll, and a backend
// that says there is nothing to ask still stops it outright.
func TestPollErrorsCountAsNoAnswerAndUnsupportedStillStops(t *testing.T) {
	m := model{themeMode: themeSystem, themePolling: true, themeOS: appearance.Dark}
	for i := 0; i < themeQuietAfter; i++ {
		m, _ = m.onThemeOS(themeOSMsg{err: errors.New("portal timed out")})
	}
	if got := m.themePollNext(); got != themePollQuiet {
		t.Errorf("repeated errors leave the poll at %v, want %v", got, themePollQuiet)
	}
	if m.themeOS != appearance.Dark {
		t.Errorf("an error changed the theme to %v", m.themeOS)
	}

	m2, cmd := m.onThemeOS(themeOSMsg{err: appearance.ErrUnsupported})
	if !m2.themeNoOS || m2.themePolling || cmd != nil {
		t.Errorf("nothing to ask should stop the poll: noOS=%v polling=%v cmd=%v",
			m2.themeNoOS, m2.themePolling, cmd != nil)
	}
}
