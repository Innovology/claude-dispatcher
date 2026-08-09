package cockpit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"claude-dispatcher/internal/version"
)

// stampVersion runs the model as a released build for the duration of a test.
func stampVersion(t *testing.T, v string) {
	t.Helper()
	orig := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = orig })
}

// footerOf renders the cockpit and returns its last line — the footer —
// stripped of colour.
func footerOf(m model) string {
	lines := strings.Split(m.View(), "\n")
	return ansi.Strip(lines[len(lines)-1])
}

// TestFooterCarriesTheVersion checks the version sits in the bottom-right
// corner of every lens, at every width that has room for it — and that where
// there is no room (a narrow terminal whose keybinding help already fills the
// row) it drops out cleanly rather than clipping the help.
func TestFooterCarriesTheVersion(t *testing.T) {
	stampVersion(t, "2.1.1")
	for _, w := range smokeWidths {
		for i := 1; i <= 8; i++ {
			m := newModel()
			m.width, m.height = w, 44
			m = press(m, itoa(i))
			footer := footerOf(m)
			shown := strings.Contains(footer, "v2.1.1")
			fits := dispWidth(m.footerHelp())+2+len("v2.1.1") <= w-2*pad

			switch {
			case fits && !shown:
				t.Errorf("lens %d @%d: room for the version but the footer has none: %q", i, w, footer)
			case !fits && shown:
				t.Errorf("lens %d @%d: version squeezed into a footer with no room: %q", i, w, footer)
			}
			if shown {
				if trailing := len(footer) - len(strings.TrimRight(footer, " ")); trailing > pad {
					t.Errorf("lens %d @%d: version is not at the right edge (%d trailing spaces): %q",
						i, w, trailing, footer)
				}
			}
		}
	}
}

// TestFooterNagsWhenTheBuildIsBehind checks the upgrade command appears beside
// the version once a newer release is known.
func TestFooterNagsWhenTheBuildIsBehind(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height = 190, 44
	m.upgradeTo = "v2.2.0"

	footer := footerOf(m)
	if !strings.Contains(footer, "v2.1.1 → v2.2.0") {
		t.Errorf("footer does not show the upgrade: %q", footer)
	}
	if !strings.Contains(footer, version.UpgradeHint()) {
		t.Errorf("footer does not show %q: %q", version.UpgradeHint(), footer)
	}
}

// TestFooterShedsTheUpgradeCommandBeforeTheVersion: on a terminal too narrow
// for both, the version number survives and the command is what goes.
func TestFooterShedsTheUpgradeCommandBeforeTheVersion(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.height = 44
	m.upgradeTo = "v2.2.0"

	// Wide enough for the help plus "v2.1.1 → v2.2.0", not for the command.
	m.width = dispWidth(m.footerHelp()) + len("v2.1.1 → v2.2.0") + 2*pad + 4
	footer := footerOf(m)
	if !strings.Contains(footer, "v2.1.1 → v2.2.0") {
		t.Errorf("narrow footer lost the version: %q", footer)
	}
	if strings.Contains(footer, version.UpgradeHint()) {
		t.Errorf("narrow footer kept the upgrade command it had no room for: %q", footer)
	}
}

// TestNoticeOutranksTheVersion: a notice says what just happened, the version
// is ambient — when only one fits, the notice keeps the corner.
func TestNoticeOutranksTheVersion(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.height = 44
	m.notice = "killed webhook retries"
	// Room for the help and the notice, but not for the version too.
	m.width = dispWidth(m.footerHelp()) + dispWidth(m.notice) + 2*pad + 4

	footer := footerOf(m)
	if !strings.Contains(footer, m.notice) {
		t.Errorf("notice squeezed out by the version: %q", footer)
	}
	if strings.Contains(footer, "v2.1.1") {
		t.Errorf("version kept a corner it had no room for: %q", footer)
	}

	// Given room for both, they share the corner.
	m.width = 190
	footer = footerOf(m)
	if !strings.Contains(footer, m.notice) || !strings.Contains(footer, "v2.1.1") {
		t.Errorf("wide footer should carry both notice and version: %q", footer)
	}
}

// TestDevBuildIsNeverNagged: an unstamped build shows what it is and never
// checks for, or claims to be behind, a release.
func TestDevBuildIsNeverNagged(t *testing.T) {
	stampVersion(t, "dev")
	if upgradeCheckCmd() != nil {
		t.Error("a dev build should not check for upgrades")
	}
	m := newModel()
	m.width, m.height = 190, 44
	if footer := footerOf(m); !strings.Contains(footer, "dev") {
		t.Errorf("dev build footer does not say so: %q", footer)
	}
}

// TestUpgradeMsgOnlyAcceptsANewerRelease guards the nag itself: only a release
// genuinely ahead of this build may set it.
func TestUpgradeMsgOnlyAcceptsANewerRelease(t *testing.T) {
	stampVersion(t, "2.1.1")
	cases := map[string]string{
		"v2.2.0":  "v2.2.0", // ahead — nag
		"v2.1.1":  "",       // same
		"v2.0.0":  "",       // behind
		"":        "",       // unknown (offline, or the check failed)
		"nightly": "",       // unparseable
	}
	for latest, want := range cases {
		m := newModel()
		m.width, m.height = 190, 44
		next, _ := m.Update(upgradeMsg{latest: latest})
		if got := next.(model).upgradeTo; got != want {
			t.Errorf("upgradeMsg{%q}: upgradeTo = %q, want %q", latest, got, want)
		}
	}
}
