package cockpit

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"claude-dispatcher/internal/version"
)

// errUpgrade stands in for whatever the package manager exited with.
var errUpgrade = errors.New("exit status 1")

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
		for i := 1; i <= 6; i++ {
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

// brewInstall and nixManaged are the two shapes the footer has to tell apart:
// one we can upgrade in place, one we must not touch.
var (
	brewInstall = version.Install{
		Method: version.MethodBrewCask,
		Cmd:    []string{"brew", "upgrade", "--cask", "claude-dispatcher"},
	}
	nixManaged = version.Install{
		Method: version.MethodNixManaged,
		Note:   "nix-managed · upgrade it where it is declared",
	}
)

// TestFooterNagsWhenTheBuildIsBehind checks the offer appears beside the
// version once a newer release is known — the key when we can act on it, and
// the reason when we cannot.
func TestFooterNagsWhenTheBuildIsBehind(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height = 190, 44
	m.upgradeTo = "v2.2.0"
	m.install = brewInstall

	footer := footerOf(m)
	if !strings.Contains(footer, "v2.1.1 → v2.2.0") {
		t.Errorf("footer does not show the upgrade: %q", footer)
	}
	// The key, not the command: it is shorter and it is what does the work.
	if !strings.Contains(footer, "U upgrades") {
		t.Errorf("footer does not offer the key: %q", footer)
	}

	// An install we will not upgrade in place says why, and never offers a key
	// that would do nothing.
	m.install = nixManaged
	footer = footerOf(m)
	if !strings.Contains(footer, "v2.1.1 → v2.2.0") {
		t.Errorf("nix footer lost the version gap: %q", footer)
	}
	if !strings.Contains(footer, "nix-managed") {
		t.Errorf("nix footer does not explain itself: %q", footer)
	}
	if strings.Contains(footer, "U upgrades") {
		t.Errorf("nix footer offered a key it cannot honour: %q", footer)
	}
}

// TestUpgradeKeyAsksFirst: U opens the confirm bar spelling out the exact
// command, because it is the one act in the cockpit that changes the machine
// rather than the state dir.
func TestUpgradeKeyAsksFirst(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height = 190, 44
	m.upgradeTo = "v2.2.0"
	m.install = brewInstall
	m = press(m, "4") // any lens where the dispatch prompt is not holding the keyboard

	m = press(m, "U")
	if m.confirm == nil || m.confirm.kind != "upgrade" {
		t.Fatalf("U did not ask: %+v", m.confirm)
	}
	if !strings.Contains(m.confirm.label, "brew upgrade --cask claude-dispatcher") {
		t.Errorf("confirm does not name the command: %q", m.confirm.label)
	}
	if !strings.Contains(m.confirm.label, "v2.2.0") {
		t.Errorf("confirm does not name the target: %q", m.confirm.label)
	}
	if bar := ansi.Strip(m.barsView()); !strings.Contains(bar, "brew upgrade") {
		t.Errorf("confirm bar does not show the command: %q", bar)
	}

	// n cancels and nothing is run.
	m = press(m, "n")
	if m.confirm != nil || m.relaunch {
		t.Error("cancelling must leave nothing pending")
	}
}

// TestUpgradeKeyRefusesWhatItCannotDo covers the two ways U is a no-op, each of
// which has to say something rather than nothing.
func TestUpgradeKeyRefusesWhatItCannotDo(t *testing.T) {
	// Nix-managed: a real upgrade exists, but not one we may run.
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height = 190, 44
	m.upgradeTo, m.install = "v2.2.0", nixManaged
	m = press(m, "4")
	m = press(m, "U")
	if m.confirm != nil {
		t.Error("a nix-managed install must not be offered an imperative upgrade")
	}
	if !strings.Contains(m.notice, "nix-managed") {
		t.Errorf("notice does not explain the refusal: %q", m.notice)
	}

	// A dev build has no release to be behind.
	stampVersion(t, "dev")
	m = newModel()
	m.width, m.height, m.install = 190, 44, brewInstall
	m.upgradeTo = "v2.2.0"
	m = press(press(m, "4"), "U")
	if m.confirm != nil || !strings.Contains(m.notice, "dev build") {
		t.Errorf("dev build: confirm=%v notice=%q", m.confirm, m.notice)
	}
}

// TestUpgradeKeyLooksBeforeClaimingToBeCurrent: knowing of nothing newer is not
// the same as there being nothing newer — the ambient answer is up to six hours
// old, and after an in-place upgrade it is older than the build reading it. U
// goes and asks, and only then says we are current.
func TestUpgradeKeyLooksBeforeClaimingToBeCurrent(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height, m.install = 190, 44, brewInstall
	m = press(m, "4")

	m, cmd := m.startUpgrade()
	if cmd == nil {
		t.Fatal("U answered from the cache instead of checking")
	}
	if !m.upgradeChecking || !strings.Contains(m.notice, "checking") {
		t.Errorf("U does not say it is looking: checking=%v notice=%q", m.upgradeChecking, m.notice)
	}
	// A second press must not put a second call on the wire.
	if _, again := m.startUpgrade(); again != nil {
		t.Error("a second U fired a second check while the first was still out")
	}

	// The check comes back current: now the claim is one we have made.
	next, _ := m.Update(upgradeMsg{latest: "v2.1.1", forced: true})
	nm := next.(model)
	if nm.confirm != nil || !strings.Contains(nm.notice, "is the latest") {
		t.Errorf("checked-and-current: confirm=%v notice=%q", nm.confirm, nm.notice)
	}
	if nm.upgradeChecking {
		t.Error("the check finished but the model still thinks one is in flight")
	}

	// It comes back unreachable: say so rather than leaving the press unanswered.
	next, _ = m.Update(upgradeMsg{forced: true})
	if nm = next.(model); !strings.Contains(nm.notice, "could not reach") {
		t.Errorf("a check that could not run said %q", nm.notice)
	}
}

// TestForcedCheckFindsOneAndAsks: the human pressed U to upgrade, not to be
// told an upgrade exists. Finding one goes straight to the confirm bar — which
// still asks, so nothing runs unattended.
func TestForcedCheckFindsOneAndAsks(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height, m.install = 190, 44, brewInstall

	next, _ := m.Update(upgradeMsg{latest: "v2.2.0", forced: true})
	nm := next.(model)
	if nm.upgradeTo != "v2.2.0" {
		t.Errorf("the found release was not recorded: %q", nm.upgradeTo)
	}
	if nm.confirm == nil || nm.confirm.kind != "upgrade" {
		t.Fatalf("U found a release and then made the human press it again: %+v", nm.confirm)
	}
	if !strings.Contains(nm.confirm.label, "v2.2.0") {
		t.Errorf("confirm does not name what it found: %q", nm.confirm.label)
	}
}

// TestPollCheckStaysAmbient: the once-a-minute check has no human waiting on
// it. It may raise the offer and it may retire it, but it never speaks.
func TestPollCheckStaysAmbient(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height, m.install = 190, 44, brewInstall

	next, _ := m.Update(upgradeMsg{latest: "v2.2.0"})
	nm := next.(model)
	if nm.upgradeTo != "v2.2.0" || nm.confirm != nil || nm.notice != "" {
		t.Errorf("the poll interrupted: to=%q confirm=%v notice=%q", nm.upgradeTo, nm.confirm, nm.notice)
	}

	// A check that could not run keeps what is already known: an offer must not
	// blink out because the network did.
	next, _ = nm.Update(upgradeMsg{})
	if got := next.(model).upgradeTo; got != "v2.2.0" {
		t.Errorf("a failed check dropped the known upgrade: %q", got)
	}

	// A check that ran and found us current retires it.
	next, _ = nm.Update(upgradeMsg{latest: "v2.1.1"})
	if got := next.(model).upgradeTo; got != "" {
		t.Errorf("the nag outlived the release that raised it: %q", got)
	}
}

// TestUpgradeKeyIsTextAtThePrompt: while the dispatch prompt has the keyboard —
// which it does whenever nothing is in flight — every letter is what the human
// is typing, U included. The same rule already governs `?`, `u` and `q` there,
// and a key that quietly upgraded the machine mid-sentence would be the worst
// of the set to make an exception for.
func TestUpgradeKeyIsTextAtThePrompt(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height = 190, 44
	m.upgradeTo, m.install = "v2.2.0", brewInstall
	if !m.cqPromptOn() {
		t.Fatal("expected the empty fleet to leave the prompt holding the keyboard")
	}

	m = press(m, "U")
	if m.confirm != nil {
		t.Error("U interrupted the human mid-prompt")
	}
}

// TestUpgradeRelaunches: a clean upgrade quits, but only so Run can exec the
// build that was just installed. A failed one stays put and says so.
func TestUpgradeRelaunches(t *testing.T) {
	m := newModel()
	m.width, m.height, m.install = 190, 44, brewInstall

	next, cmd := m.Update(upgradeRanMsg{})
	nm := next.(model)
	if !nm.relaunch {
		t.Error("a clean upgrade must ask Run to relaunch")
	}
	if cmd == nil {
		t.Error("a clean upgrade must quit so the terminal is handed back first")
	}

	next, _ = m.Update(upgradeRanMsg{err: errUpgrade})
	nm = next.(model)
	if nm.relaunch {
		t.Error("a failed upgrade must not relaunch into the old build")
	}
	if !strings.Contains(nm.notice, "upgrade failed") ||
		!strings.Contains(nm.notice, "brew upgrade --cask claude-dispatcher") {
		t.Errorf("failure notice does not say what failed: %q", nm.notice)
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
