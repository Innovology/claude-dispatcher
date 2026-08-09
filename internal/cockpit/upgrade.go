package cockpit

// upgrade.go keeps the bottom-right corner honest about which build is running
// and whether it is behind the published one.

import (
	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/version"
)

// upgradeMsg carries the newest published release tag to the UI goroutine.
type upgradeMsg struct{ latest string }

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

// versionForms is the bottom-right version, widest rendering first. The footer
// takes the first one that fits, so a cramped terminal keeps the version number
// and drops the upgrade command rather than losing both.
func (m model) versionForms() []string {
	if m.upgradeTo == "" {
		return []string{fg(cFaint, version.Display())}
	}
	behind := fg(cAmber, version.Display()+" → "+version.Label(m.upgradeTo))
	return []string{
		behind + fg(cDim, " · "+version.UpgradeHint()),
		behind,
	}
}
