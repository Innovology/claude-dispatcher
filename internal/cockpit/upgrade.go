package cockpit

// upgrade.go keeps the bottom-right corner honest about which build is running
// and whether it is behind the published one.

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/version"
)

// upgradeMsg carries the newest published release tag to the UI goroutine.
// forced marks the answer a human asked for by pressing U, which is the only
// one that gets to speak: the poll's check is ambient and says nothing unless
// it has an upgrade to offer.
type upgradeMsg struct {
	latest string
	forced bool
}

// upgradeCheckCmd looks up the newest release off the UI goroutine. The lookup
// is cache-gated inside the version package, so firing it on every poll costs a
// file read rather than a network call. A dev build skips it entirely: there is
// no released version an unstamped build can be behind.
func upgradeCheckCmd() tea.Cmd {
	if !version.IsRelease() {
		return nil
	}
	return func() tea.Msg { return upgradeMsg{latest: version.Latest()} }
}

// upgradeRecheckCmd is the same lookup with the cache stepped over. It is what
// U runs when nothing newer is known: the cached answer is good for hours, and
// "you are on the latest" is the one thing we must not say on its word alone.
func upgradeRecheckCmd() tea.Cmd {
	return func() tea.Msg { return upgradeMsg{latest: version.Recheck(), forced: true} }
}

// upgradeRanMsg reports how the package manager got on, once the terminal
// comes back.
type upgradeRanMsg struct{ err error }

// versionForms is the bottom-right version, widest rendering first. The footer
// takes the first one that fits, so a cramped terminal keeps the version number
// and drops the upgrade offer rather than losing both.
//
// The offer names the key, not the command: the key is the shorter string and
// the one that does the work. An install we cannot upgrade in place still gets
// its clause — the version gap is worth knowing even when we have nothing to
// press — but it says why instead ("nix-managed · upgrade it where it is
// declared").
func (m model) versionForms() []string {
	if m.upgradeTo == "" {
		return []string{fg(cFaint, version.Display())}
	}
	behind := fg(cAmber, version.Display()+" → "+version.Label(m.upgradeTo))
	tail := m.install.Note
	if m.install.CanUpgrade() {
		tail = "U upgrades"
	}
	return []string{
		behind + fg(cDim, " · "+tail),
		behind,
	}
}

// startUpgrade is the U key: it asks before running anything. The confirm bar
// spells out the exact command, because this is the one act in the cockpit
// that reaches outside the state dir and changes the machine.
func (m model) startUpgrade() (model, tea.Cmd) {
	if !version.IsRelease() {
		m.notice = "dev build — nothing to upgrade to"
		return m, nil
	}
	if m.upgradeTo == "" {
		// Knowing of nothing newer is not the same as there being nothing newer.
		// The ambient answer is up to checkTTL old, and a human presses U because
		// they think a release is out — so go and look rather than reciting a
		// file. The answer comes back as a forced upgradeMsg, which either opens
		// the confirm below or says we are current, having actually checked.
		if m.upgradeChecking {
			return m, nil
		}
		m.upgradeChecking = true
		m.notice = "checking for a newer build…"
		return m, upgradeRecheckCmd()
	}
	if !m.install.CanUpgrade() {
		m.notice = m.install.Note
		return m, nil
	}
	m.confirm = &confirmState{
		kind:  "upgrade",
		label: m.install.Hint() + "  →  " + version.Label(m.upgradeTo),
	}
	return m, nil
}

// upgradeRunCmd hands the terminal to the package manager, the same way `enter`
// hands it to tmux. Its output is the human's — a cask download has its own
// progress to show, and hiding it behind a spinner would mean inventing a
// summary of a process we do not control.
func upgradeRunCmd(in version.Install) tea.Cmd {
	if !in.CanUpgrade() {
		return nil
	}
	c := exec.Command(in.Cmd[0], in.Cmd[1:]...)
	c.Env = in.Env()
	return tea.ExecProcess(c, func(err error) tea.Msg { return upgradeRanMsg{err: err} })
}

// upgradeFailed is the notice for a package manager that exited non-zero. Its
// own output is already on screen above the cockpit's redraw, so this says
// which command failed rather than repeating a reason it did not give us.
func upgradeFailed(in version.Install, err error) string {
	return "upgrade failed (" + strings.Join(in.Cmd, " ") + "): " + err.Error()
}
