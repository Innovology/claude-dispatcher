package cockpit

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
)

// withAccounts gives m a second account and the probe's answer for both: the
// default has a reading, "work" has none yet.
func withAccounts(m model) model {
	m.cfg.Accounts = map[string]string{"work": "/tmp/claude-work"}
	now := time.Now()
	in := accountsMsg{infos: []acctInfo{
		{
			acct: account.Account{Name: account.Default},
			auth: account.Auth{Known: true, LoggedIn: true, Email: "me@example.com", Plan: "max"},
			limits: account.Limits{
				FiveHour: &account.Window{UsedPct: 23, ResetsAt: now.Add(2 * time.Hour).Unix()},
				SevenDay: &account.Window{UsedPct: 41, ResetsAt: now.Add(72 * time.Hour).Unix()},
				At:       now.Add(-3 * time.Minute),
			},
			hasLimits: true,
		},
		{
			acct: account.Account{Name: "work", Dir: "/tmp/claude-work"},
			auth: account.Auth{Known: true, LoggedIn: true, Email: "me@work.example", Plan: "pro"},
		},
	}}
	next, _ := m.Update(in)
	return next.(model)
}

// The figure the choice is made on sits on the line it is made: the least of
// the two windows beside each account's name, and the selected one in full.
func TestDXAccountShowsWhatEachHasLeft(t *testing.T) {
	m := withAccounts(dxFormModel(t))
	if got := m.dxAccountWords(); len(got) != 2 || got[0] != "default 59%" || got[1] != "work" {
		t.Fatalf("ACCOUNT words = %q, want [default 59%% work]", got)
	}
	hint := m.dxAccountHint()
	for _, want := range []string{"me@example.com", "max", "5h 77%", "wk 59% left", "3m ago"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q does not carry %q", hint, want)
		}
	}
	// An account no status line has reported for says so rather than show a
	// number nobody read.
	m.dxAccount = "work"
	if hint := m.dxAccountHint(); !strings.Contains(hint, "no usage reading yet") {
		t.Errorf("work's hint = %q", hint)
	}
	if out := m.View(); !strings.Contains(out, "ACCOUNT") || !strings.Contains(out, "default 59%") {
		t.Error("the ACCOUNT row is not on the form")
	}
}

func TestDXAccountReachesTheLaunch(t *testing.T) {
	var got string
	prev := dxLaunch
	dxLaunch = func(_ *config.Config, _, _, _ string, _ dispatchpkg.Mode, _ dispatchpkg.Model, _ dispatchpkg.Root, _ bool, acct string) tea.Cmd {
		got = acct
		return nil
	}
	t.Cleanup(func() { dxLaunch = prev })

	m := withAccounts(dxFormModel(t))
	m.dxTitle, m.dxWhat = "payment retries", "retry declined cards"
	m.dxField = dxAccountF
	m = press(m, "space")
	if m.dxAccount != "work" {
		t.Fatalf("space on ACCOUNT → %q, want work", m.dxAccount)
	}
	if !strings.Contains(m.dxSummary(), "on work") {
		t.Errorf("summary %q does not say which account", m.dxSummary())
	}
	m, _ = m.dxSubmit()
	if got != "work" {
		t.Errorf("the launch got account %q, want work", got)
	}
	if !strings.Contains(m.notice, "on work") {
		t.Errorf("notice = %q", m.notice)
	}

	// Untouched, it is the human's own login.
	m = withAccounts(dxFormModel(t))
	m.dxTitle, m.dxWhat = "payment retries", "retry declined cards"
	if _, _ = m.dxSubmit(); got != account.Default {
		t.Errorf("an untouched ACCOUNT launched on %q, want default", got)
	}
}

// An account removed from the config while the form was open dispatches on
// the default rather than on a name that no longer resolves.
func TestDXAccountFallsBackWhenRemoved(t *testing.T) {
	m := withAccounts(dxFormModel(t))
	m.dxAccount = "work"
	delete(m.cfg.Accounts, "work")
	if got := m.acctNormalize(m.dxAccount); got != account.Default {
		t.Errorf("normalised to %q", got)
	}
}

func TestDispatchFormAccountStep(t *testing.T) {
	var got string
	prev := launchDispatch
	launchDispatch = func(_ *config.Config, _, _, _ string, _ dispatchpkg.Mode, _ dispatchpkg.Model, _ dispatchpkg.Root, _ bool, acct string) tea.Cmd {
		got = acct
		return nil
	}
	t.Cleanup(func() { launchDispatch = prev })

	m := newModel()
	m.width, m.height = 130, 30
	m.cfg = &config.Config{Roots: []string{seedRepoRoot(t, "api")}}
	m.lens = "products"
	m = press(m, "+")
	m = withAccounts(m)
	m = press(m, "enter")
	m = typeStr(m, "thing")
	for range 3 { // → root → mode → model
		m = press(m, "enter")
	}
	m = press(m, "enter") // → account
	if m.dispatchForm.step != dispatchAccount {
		t.Fatalf("step = %d, want account", m.dispatchForm.step)
	}
	out := m.View()
	for _, want := range []string{"default", "59%", "me@example.com", "work", "me@work.example"} {
		if !strings.Contains(out, want) {
			t.Errorf("the account step does not show %q", want)
		}
	}
	m = press(m, "down")
	m = press(m, "enter") // → fan out
	m = press(m, "enter") // → prompt
	m = typeStr(m, "do it")
	m = press(m, "enter")
	if got != "work" {
		t.Errorf("the launch got account %q, want work", got)
	}
	if !strings.Contains(m.notice, "on work") {
		t.Errorf("notice = %q", m.notice)
	}
}

// A launch naming an account the config does not have fails as a launch,
// with the reason, rather than running on some other login.
func TestLaunchRefusesAnUnknownAccount(t *testing.T) {
	msg := launchCmd(&config.Config{}, "repo", "feature", "prompt", dispatchpkg.ModeAuto,
		dispatchpkg.DefaultModel, dispatchpkg.DefaultRoot, false, "nope")()
	lm, ok := msg.(launchedMsg)
	if !ok || !lm.failed || !strings.Contains(lm.reason, "no account named nope") {
		t.Fatalf("msg = %#v", msg)
	}
}
