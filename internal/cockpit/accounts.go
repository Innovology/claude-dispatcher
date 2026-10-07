package cockpit

// accounts.go is ACCOUNT on both dispatch forms: which Claude subscription a
// dispatch runs under, and how much of each one's 5-hour and weekly limits is
// left (internal/account).
//
// The figures are read when a form opens, not on the poll. They are the
// answer to a question asked at the moment of dispatching — "which of these
// has room?" — and the poll would be spending a subprocess per account every
// few seconds to keep a figure fresh on a screen that is mostly not showing
// it. The list of names is the config's and is there at once; who each one is
// logged in as and what it has left arrive a tenth of a second later, and say
// "…" until then rather than a figure nobody read.
//
// A percentage is shown only where a status line reported one, with its age
// beside it. An account no session has drawn a status line for says so: the
// usage lens learns a cap from transcripts, but that is a model of a limit and
// this is the place the human picks a subscription on the strength of a
// number, so it carries only the number Claude Code gave.

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// acctInfo is one account as the forms show it.
type acctInfo struct {
	acct      account.Account
	auth      account.Auth
	limits    account.Limits
	hasLimits bool
	problems  []string
}

// accountsMsg carries the probe back.
type accountsMsg struct{ infos []acctInfo }

// probeAccounts is the seam the probe runs through, so a test can hand the
// forms accounts without a claude to ask.
var probeAccounts = func(cfg *config.Config) []acctInfo {
	all := account.List(cfg)
	out := make([]acctInfo, len(all))
	var wg sync.WaitGroup
	for i, a := range all {
		wg.Add(1)
		go func(i int, a account.Account) {
			defer wg.Done()
			info := acctInfo{acct: a, auth: account.Probe(a)}
			info.limits, info.hasLimits = account.ReadLimits(state.Dir(), a.ConfigDir())
			info.problems = account.Problems(a, info.auth)
			out[i] = info
		}(i, a)
	}
	wg.Wait()
	return out
}

// accountsCmd asks every account who it is and what it has left.
func accountsCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg { return accountsMsg{infos: probeAccounts(cfg)} }
}

// accountNames is every account a form can offer, default first — read from
// the config, so the switch works before the probe has answered.
func (m model) accountNames() []string {
	all := account.List(m.cfg)
	out := make([]string, len(all))
	for i, a := range all {
		out[i] = a.Name
	}
	return out
}

// accountInfo is what the probe found for name, if it has answered.
func (m model) accountInfo(name string) (acctInfo, bool) {
	for _, in := range m.accounts {
		if in.acct.Name == name {
			return in, true
		}
	}
	return acctInfo{}, false
}

// acctPct is name's least percentage left as a short tag — "77%" — or "" with
// no reading. The least of the two windows, because that is the one that will
// stop a session first.
func (m model) acctPct(name string, now time.Time) string {
	in, ok := m.accountInfo(name)
	if !ok || !in.hasLimits {
		return ""
	}
	if least := in.limits.Least(now); least >= 0 {
		return itoa(least) + "%"
	}
	return ""
}

// acctDetail is everything known about name on one line: who it is, what it
// has left and how old that is, or the first thing stopping it from running a
// dispatch.
func (m model) acctDetail(name string, now time.Time) string {
	in, ok := m.accountInfo(name)
	if !ok {
		return "…"
	}
	if len(in.problems) > 0 {
		return "! " + in.problems[0]
	}
	who := ""
	switch {
	case !in.auth.Known:
		who = ""
	case !in.auth.LoggedIn:
		who = "not logged in"
	default:
		who = in.auth.Email
		if in.auth.Plan != "" {
			who += " · " + in.auth.Plan
		}
	}
	left := "no usage reading yet — a session has to draw its status line first"
	if in.hasLimits {
		left = in.limits.Brief(now) + " · " + usgAgo(in.limits.At) + " ago"
	}
	if who == "" {
		return left
	}
	return who + " · " + left
}

// acctNormalize is name if it is still an account, else the default — a
// form whose account was removed from the config while it was open
// dispatches on the human's own login rather than on nothing.
func (m model) acctNormalize(name string) string {
	for _, n := range m.accountNames() {
		if n == name {
			return n
		}
	}
	return account.Default
}
