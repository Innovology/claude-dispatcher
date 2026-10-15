package cockpit

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
)

// launchCall is the last dispatch a form handed off — every argument of it,
// so a test reads the one it is about rather than restating the seam — and how
// many it handed off in all.
type launchCall struct {
	repo, feature, prompt string
	mode                  dispatchpkg.Mode
	model                 dispatchpkg.Model
	root                  dispatchpkg.Root
	prefix                *string
	fanOut                bool
	account               string
	n                     int
}

// captureLaunches replaces the seam both forms hand off through (dxLaunch for
// the triage form, launchDispatch for the `+` overlay) and records what each
// dispatch asked for. The command it returns is a real one that does nothing:
// "did it dispatch" is read off the returned command too, and a nil would make
// every rejected submit look like every accepted one.
func captureLaunches(t *testing.T) *launchCall {
	t.Helper()
	got := &launchCall{}
	prev, dxPrev := launchDispatch, dxLaunch
	stub := func(_ *config.Config, repo, feature, prompt string, mode dispatchpkg.Mode, mdl dispatchpkg.Model,
		root dispatchpkg.Root, prefix *string, fanOut bool, acct string) tea.Cmd {
		*got = launchCall{repo: repo, feature: feature, prompt: prompt, mode: mode, model: mdl, root: root,
			prefix: prefix, fanOut: fanOut, account: acct, n: got.n + 1}
		return func() tea.Msg { return nil }
	}
	launchDispatch, dxLaunch = stub, stub
	t.Cleanup(func() { launchDispatch, dxLaunch = prev, dxPrev })
	return got
}
