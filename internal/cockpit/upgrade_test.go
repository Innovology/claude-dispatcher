package cockpit

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

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

// TestUpgradeKeyRunsRatherThanAsks: U starts the package manager behind the
// cockpit. The confirm bar it used to open was there because the next thing
// that happened was the terminal being taken away; nothing is taken away now,
// so a second agreement to what they just pressed a key for is not asked for.
func TestUpgradeKeyRunsRatherThanAsks(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height = 190, 44
	m.upgradeTo = "v2.2.0"
	m.install = brewInstall
	m = press(m, "4") // any lens where the dispatch prompt is not holding the keyboard

	m = press(m, "U")
	if m.confirm != nil {
		t.Errorf("U asked instead of running: %+v", m.confirm)
	}
	if m.upgrade == nil {
		t.Fatal("U did not start the upgrade")
	}
	if got := strings.Join(m.upgrade.cmd, " "); got != "brew upgrade --cask claude-dispatcher" {
		t.Errorf("the run does not carry the command: %q", got)
	}
	if m.upgrade.to != "v2.2.0" {
		t.Errorf("the run does not carry the target: %q", m.upgrade.to)
	}

	// The whole of it on screen is one bar: what is being installed, a meter
	// that is moving, and the manager's own words.
	bar := ansi.Strip(m.barsView())
	if !strings.Contains(bar, "upgrading") || !strings.Contains(bar, "v2.1.1 → v2.2.0") {
		t.Errorf("bar does not say what is happening: %q", bar)
	}
	if !strings.Contains(bar, "brew upgrade --cask claude-dispatcher") {
		t.Errorf("bar does not carry the manager's line: %q", bar)
	}
	if !strings.Contains(bar, "█") {
		t.Errorf("bar has no meter: %q", bar)
	}

	// A second press must not put a second package manager on the same install.
	before := m.upgrade
	m2 := press(m, "U")
	if m2.upgrade != before {
		t.Error("a second U started a second upgrade")
	}
	if !strings.Contains(m2.notice, "already upgrading") {
		t.Errorf("a second U said nothing: %q", m2.notice)
	}
}

// TestUpgradeBarRendersOverAnOverlay: the bar is the only sign the upgrade is
// happening, so it has to survive the human opening a form over it — a bar that
// vanished when the dispatch form opened would read as an upgrade that stopped.
func TestUpgradeBarRendersOverAnOverlay(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height = 190, 44
	m.upgradeTo, m.install = "v2.2.0", brewInstall
	m = press(press(m, "4"), "U")

	m = press(m, "+")
	if m.dispatchForm == nil {
		t.Fatal("expected the dispatch form to open")
	}
	if !strings.Contains(ansi.Strip(m.View()), "upgrading") {
		t.Error("the upgrade bar disappeared behind the dispatch form")
	}

	// And at every width: the caption is up to 90 columns of somebody else's
	// output, so a bar that did not clip would wrap and corrupt the alt screen.
	m.upgrade.say(strings.Repeat("Downloading a very long cask url ", 6))
	for _, w := range smokeWidths {
		m.width = w
		bar := ansi.Strip(m.upgradeBar())
		if strings.Contains(bar, "\n") {
			t.Errorf("@%d: the bar is more than one line: %q", w, bar)
		}
		if got := dispWidth(bar); got > w {
			t.Errorf("@%d: the bar is %d columns wide: %q", w, got, bar)
		}
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
	if m.upgrade != nil {
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
	if m.upgrade != nil || !strings.Contains(m.notice, "dev build") {
		t.Errorf("dev build: upgrade=%v notice=%q", m.upgrade, m.notice)
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
	if nm.upgrade != nil || !strings.Contains(nm.notice, "is the latest") {
		t.Errorf("checked-and-current: upgrade=%v notice=%q", nm.upgrade, nm.notice)
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

// TestForcedCheckFindsOneAndRuns: the human pressed U to upgrade, not to be
// told an upgrade exists. Finding one starts it.
func TestForcedCheckFindsOneAndRuns(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height, m.install = 190, 44, brewInstall

	next, _ := m.Update(upgradeMsg{latest: "v2.2.0", forced: true})
	nm := next.(model)
	if nm.upgradeTo != "v2.2.0" {
		t.Errorf("the found release was not recorded: %q", nm.upgradeTo)
	}
	if nm.upgrade == nil {
		t.Fatal("U found a release and then made the human press it again")
	}
	if nm.upgrade.to != "v2.2.0" {
		t.Errorf("the run does not name what it found: %q", nm.upgrade.to)
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
	if nm.upgradeTo != "v2.2.0" || nm.upgrade != nil || nm.notice != "" {
		t.Errorf("the poll interrupted: to=%q upgrade=%v notice=%q", nm.upgradeTo, nm.upgrade, nm.notice)
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
	if m.upgrade != nil {
		t.Error("U interrupted the human mid-prompt")
	}
}

// TestUpgradeRelaunches: a clean upgrade quits, but only so Run can exec the
// build that was just installed. A failed one stays put and says so — including
// what the package manager said, which is no longer on screen anywhere else.
func TestUpgradeRelaunches(t *testing.T) {
	m := newModel()
	m.width, m.height, m.install = 190, 44, brewInstall
	m.upgrade = &upgradeRun{cmd: brewInstall.Cmd, to: "v2.2.0"}

	next, cmd := m.Update(upgradeRanMsg{})
	nm := next.(model)
	if !nm.relaunch {
		t.Error("a clean upgrade must ask Run to relaunch")
	}
	if cmd == nil {
		t.Error("a clean upgrade must quit so the terminal is handed back first")
	}

	next, _ = m.Update(upgradeRanMsg{err: errUpgrade, detail: "Error: No such keg: /opt/homebrew/Cellar/x"})
	nm = next.(model)
	if nm.relaunch {
		t.Error("a failed upgrade must not relaunch into the old build")
	}
	if nm.upgrade != nil {
		t.Error("a failed upgrade left its bar on screen")
	}
	if !strings.Contains(nm.notice, "upgrade failed") ||
		!strings.Contains(nm.notice, "brew upgrade --cask claude-dispatcher") {
		t.Errorf("failure notice does not say what failed: %q", nm.notice)
	}
	if !strings.Contains(nm.notice, "No such keg") {
		t.Errorf("failure notice dropped the manager's own reason: %q", nm.notice)
	}

	// A tick arriving after the failure retires with it rather than re-arming a
	// bar for a run that is over.
	if _, cmd := nm.Update(upgradeTickMsg{}); cmd != nil {
		t.Error("the tick outlived the run it was pacing")
	}
}

// TestUpgradeWaitsForWhatWouldBeLost: the exec takes the screen and everything
// typed into it. A build that lands while the human is part-way through a
// dispatch prompt waits for them to finish rather than taking it away.
func TestUpgradeWaitsForWhatWouldBeLost(t *testing.T) {
	stampVersion(t, "2.1.1")
	m := newModel()
	m.width, m.height = 190, 44
	m.upgradeTo, m.install = "v2.2.0", brewInstall
	m = press(press(m, "4"), "U")
	m = press(m, "+")
	if m.dispatchForm == nil {
		t.Fatal("expected the dispatch form to open")
	}

	next, cmd := m.Update(upgradeRanMsg{})
	nm := next.(model)
	if nm.relaunch {
		t.Fatal("the exec took the form the human was typing into")
	}
	if cmd == nil {
		t.Fatal("the held relaunch has nothing left to release it")
	}
	if bar := ansi.Strip(nm.barsView()); !strings.Contains(bar, "upgraded") ||
		!strings.Contains(bar, "as soon as you are done") {
		t.Errorf("the bar does not say what it is waiting for: %q", bar)
	}
	// A tick while it is still open changes nothing but the frame.
	held, _ := nm.Update(upgradeTickMsg{})
	if held.(model).relaunch {
		t.Fatal("a tick execed over the open form")
	}

	// The form closes: the very next tick execs.
	nm = press(nm, "esc")
	if nm.dispatchForm != nil {
		t.Fatal("esc did not close the form")
	}
	done, cmd := nm.Update(upgradeTickMsg{})
	if !done.(model).relaunch || cmd == nil {
		t.Errorf("the held relaunch never landed: relaunch=%v cmd=%v", done.(model).relaunch, cmd != nil)
	}
}

// TestInputPendingCoversEveryTypedField: the list of things an exec must not
// take away is hand-written, so it is checked against the states that actually
// hold text. Reading overlays are deliberately absent — one left open at lunch
// must not stop the upgrade from ever landing.
func TestInputPendingCoversEveryTypedField(t *testing.T) {
	base := func() model {
		m := newModel()
		m.width, m.height = 190, 44
		return press(m, "4") // off the triage lens, where the form is always up
	}
	holds := map[string]func(model) model{
		"the dispatch form":   func(m model) model { return press(m, "+") },
		"the palette":         func(m model) model { return press(m, ":") },
		"settings":            func(m model) model { return press(m, ",") },
		"a park reason":       func(m model) model { m.parkOpen = true; return m },
		"a product name":      func(m model) model { m.clNaming = true; return m },
		"a linear token":      func(m model) model { m.clKeying = true; return m },
		"a pending confirm":   func(m model) model { m.confirm = &confirmState{kind: "kill"}; return m },
		"a typed triage form": func(m model) model { m.cqDispatch, m.dxTitle = true, "half a name"; return m },
	}
	for name, open := range holds {
		if m := open(base()); !m.inputPending() {
			t.Errorf("%s: an exec would have taken it away", name)
		}
	}

	frees := map[string]func(model) model{
		"help":                 func(m model) model { return press(m, "?") },
		"an untouched dx form": func(m model) model { m.cqDispatch = true; return m },
	}
	for name, open := range frees {
		if m := open(base()); m.inputPending() {
			t.Errorf("%s: holds the upgrade back with nothing to lose", name)
		}
	}
}

// TestUpgradeWriterKeepsTheLastLine: the caption is whatever the package
// manager last said. Downloads carriage-return their percentage over one line
// and never terminate it, so \r has to break a line as \n does — otherwise the
// bar shows nothing at all for the length of a download and then dumps it.
func TestUpgradeWriterKeepsTheLastLine(t *testing.T) {
	r := &upgradeRun{}
	w := &upgradeWriter{run: r}

	_, _ = w.Write([]byte("==> Downloading https://example/x.zip\n"))
	if got := r.status(); got != "==> Downloading https://example/x.zip" {
		t.Errorf("newline-terminated line: %q", got)
	}
	_, _ = w.Write([]byte("####  20.1%\r####  61.4%\r"))
	if got := r.status(); got != "####  61.4%" {
		t.Errorf("carriage-returned progress: %q", got)
	}
	// A blank line must not wipe a caption that said something.
	_, _ = w.Write([]byte("\n\n"))
	if got := r.status(); got != "####  61.4%" {
		t.Errorf("a blank line blanked the caption: %q", got)
	}
	// Colour and control characters never reach the renderer: dispWidth cannot
	// measure them and the rest of the footer would inherit them.
	_, _ = w.Write([]byte("\x1b[32m==> Purging files\x1b[0m\n"))
	if got := r.status(); got != "==> Purging files" {
		t.Errorf("escape sequences reached the bar: %q", got)
	}
	if strings.ContainsRune(r.status(), 0x1b) {
		t.Error("raw escape survived")
	}

	// The error is quoted from the line that named one, not from whatever the
	// manager happened to say last on its way out.
	_, _ = w.Write([]byte("Error: cask 'claude-dispatcher' is not installed\n"))
	_, _ = w.Write([]byte("==> Cleaning up\n"))
	if got := r.errLine(); !strings.Contains(got, "not installed") {
		t.Errorf("errLine lost the reason: %q", got)
	}
	if got := r.status(); got != "==> Cleaning up" {
		t.Errorf("the caption stopped following the output: %q", got)
	}
}

// TestUpgradeMeterClaimsMotionAndNothingElse: the package manager does not tell
// us how far through it is, so the meter must not read as a proportion — it is
// a fixed run that slides and wraps, and it is the same width whatever the
// frame.
func TestUpgradeMeterClaimsMotionAndNothingElse(t *testing.T) {
	const w = 24
	seen := map[string]bool{}
	for f := 0; f < 200; f++ {
		got := upgradeMeter(f, w)
		if n := len([]rune(got)); n != w {
			t.Fatalf("frame %d: width %d, want %d", f, n, w)
		}
		if lit := strings.Count(got, "█"); lit != upgradeMeterRun {
			t.Fatalf("frame %d: %d lit cells, want %d — a meter that fills is a claim about progress",
				f, lit, upgradeMeterRun)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Error("the meter never moves")
	}
	if upgradeMeter(0, 0) != "" {
		t.Error("a zero-width meter must render nothing rather than panic")
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

// TestUpgradeRunCmdAgainstARealProcess drives the whole run path — spawn,
// output pumps, exit status, the reason carried out — against a real child
// rather than a hand-built upgradeRun.
//
// The first case is the one that matters most: a command that reads from stdin.
// The upgrade has no terminal on it, so a package manager that stops to ask
// something would hang behind a bar that spins forever. Stdin is /dev/null, so
// the read returns immediately and the run either finishes or fails — never
// waits.
func TestUpgradeRunCmdAgainstARealProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX shell to stand in for a package manager")
	}
	run := func(t *testing.T, script string) (upgradeRanMsg, *upgradeRun) {
		t.Helper()
		r := &upgradeRun{started: time.Now()}
		in := version.Install{Method: version.MethodBrewCask, Cmd: []string{"sh", "-c", script}}
		done := make(chan upgradeRanMsg, 1)
		go func() { done <- upgradeRunCmd(in, r)().(upgradeRanMsg) }()
		select {
		case msg := <-done:
			return msg, r
		case <-time.After(10 * time.Second):
			t.Fatal("the run never came back — something is waiting for an answer nobody can give")
			return upgradeRanMsg{}, nil
		}
	}

	t.Run("a command that asks gets EOF, not a wait", func(t *testing.T) {
		msg, r := run(t, `printf 'Proceed? [Y/n] '; read answer; echo "answered ${answer:-nothing}"`)
		if msg.err != nil {
			t.Fatalf("the shell could not run: %v", msg.err)
		}
		if got := r.status(); !strings.Contains(got, "answered nothing") {
			t.Errorf("the prompt was not left unanswered: %q", got)
		}
	})

	t.Run("output from both streams reaches the caption", func(t *testing.T) {
		_, r := run(t, `echo "==> Downloading"; printf '### 40%%\r### 90%%\r' ; echo "==> Installed" >&2`)
		if got := r.status(); got != "==> Installed" {
			t.Errorf("last line = %q", got)
		}
	})

	t.Run("a failure carries the manager's own reason", func(t *testing.T) {
		msg, _ := run(t, `echo "Error: cask is not installed" >&2; echo "==> Cleaning up"; exit 1`)
		if msg.err == nil {
			t.Fatal("a non-zero exit came back clean")
		}
		if !strings.Contains(msg.detail, "not installed") {
			t.Errorf("detail = %q, want the error line", msg.detail)
		}
		if notice := upgradeFailed(version.Install{Cmd: []string{"brew", "upgrade"}}, msg.err, msg.detail); !strings.Contains(notice, "not installed") {
			t.Errorf("the notice does not carry it: %q", notice)
		}
	})
}
