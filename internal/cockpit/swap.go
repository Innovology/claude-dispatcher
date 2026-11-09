package cockpit

// swap.go is `a` on the triage table: move a dispatcher onto another Claude
// subscription and resume its conversation there (dispatch.SwapAccount) —
// what to do when the one it runs on has run out.
//
// It is a picker rather than a cycle, because the choice is made on the
// strength of a number: every account is listed with what it has left, the
// one it is on marked and not offered, the same figures the dispatch forms
// show (accounts.go), probed when the picker opens.
//
// The act runs in the background and the table stays. Resume hands the
// terminal over because ⏎ is a request to look at the session; a swap is a
// request to get it going again, and the row saying "resuming its session"
// then working is the answer to that.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// swapTarget is the dispatcher the picker is about, captured when it opens
// for the reason parkTarget is: the table rebuilds underneath it.
type swapTarget struct {
	id, feature string
	// current is the account it runs on, as the picker names it.
	current string
}

// swapOpenFor opens the picker on r, or says why it cannot.
func (m model) swapOpenFor(r fleetRow) (model, tea.Cmd) {
	names := m.accountNames()
	if len(names) < 2 {
		m.notice = "there is only one account — claude-dispatcher account add <name> adds another"
		return m, nil
	}
	rec := recordByID(r.id)
	if rec == nil {
		m.notice = "no dispatch record to move"
		return m, nil
	}
	cur := rec.Account
	if cur == "" {
		cur = account.Default
	}
	m.swapOpen = true
	m.swapAt = &swapTarget{id: r.id, feature: r.feature, current: cur}
	m.swapCursor = 0
	for i, n := range names {
		if n != cur {
			m.swapCursor = i
			break
		}
	}
	return m, accountsCmd(m.cfg)
}

// updateSwap drives the picker while it owns the keyboard.
func (m model) updateSwap(k string) (model, tea.Cmd) {
	names := m.accountNames()
	t := m.swapAt
	switch k {
	case "esc", "q":
		m.swapOpen, m.swapAt = false, nil
		return m, nil
	case "j", "down":
		m.swapCursor = clampCursor(m.swapCursor+1, len(names))
	case "k", "up":
		m.swapCursor = clampCursor(m.swapCursor-1, len(names))
	case "enter":
		if t == nil || len(names) == 0 {
			m.swapOpen, m.swapAt = false, nil
			return m, nil
		}
		to := names[clampCursor(m.swapCursor, len(names))]
		if to == t.current {
			m.notice = "\"" + t.feature + "\" is already on " + to + " — pick another"
			return m, nil
		}
		m.swapOpen, m.swapAt = false, nil
		m.notice = "moving \"" + t.feature + "\" to " + to + "…"
		return m, swapCmd(m.cfg, t.id, to)
	}
	return m, nil
}

// viewSwap renders the picker: whose session, then every account with what
// it has left, the current one marked.
func (m model) viewSwap(w, h int) string {
	t := m.swapAt
	if t == nil {
		return ""
	}
	cw := w - 2*pad
	if cw < 10 {
		cw = w
	}
	var out []string
	out = append(out, fg(cDim, "switch the account this dispatcher runs on"))
	out = append(out, line(t.feature, cw, cWhite, ""))
	out = append(out, fg(cFaint, "its conversation moves across and resumes there · the session restarts"))
	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	names := m.accountNames()
	sel := clampCursor(m.swapCursor, len(names))
	now := time.Now()
	for i, n := range names {
		bg, marker, nameColor := cTransparent, " ", cFg
		if i == sel {
			bg, marker, nameColor = cSel, "▸", cWhite
		}
		detail, tone := m.acctDetail(n, now), cDim
		if strings.HasPrefix(detail, "! ") {
			tone = cAmber
		}
		if n == t.current {
			detail, tone, nameColor = "current · "+detail, cFaint, cFaint
		}
		out = append(out, row(cw, bg,
			c(marker, 2, cMid),
			c(n, 12, nameColor),
			c(m.acctPct(n, now), 5, cMid),
			flexc(detail, tone),
		))
	}
	out = append(out, "")
	out = append(out, fg(cFaint, "j/k choose · enter moves it · esc cancels"))
	return clampLines(gutter(strings.Join(out, "\n"), pad), h)
}

// swapCmd moves the dispatcher and resumes it, by id.
//
// The record is read from disk, not from the snapshot: whether the turn is
// over decides whether ending its claude loses anything, and a snapshot can be
// a load behind a reply that set it working again.
func swapCmd(cfg *config.Config, id, to string) tea.Cmd {
	return func() tea.Msg {
		var rec *state.Dispatch
		for _, d := range state.LoadAll() {
			if d.ID == id {
				rec = d
			}
		}
		if rec == nil {
			return actionMsg{notice: "no dispatch record to move"}
		}
		acct, ok := account.Find(cfg, to)
		if !ok {
			return actionMsg{notice: "no account \"" + to + "\" any more"}
		}
		mode, _, err := dispatchpkg.SwapAccount(rec, acct, "")
		if err != nil {
			return actionMsg{notice: "switch failed: " + firstLine(err.Error())}
		}
		if mode == dispatchpkg.ResumeRelaunched {
			return actionMsg{notice: "started \"" + rec.Feature + "\" again on " + to}
		}
		return actionMsg{notice: "\"" + rec.Feature + "\" resumed on " + to + " · same conversation"}
	}
}
