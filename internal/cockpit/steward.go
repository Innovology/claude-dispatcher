package cockpit

// steward.go is the cockpit's side of the product stewards (internal/steward).
//
// A steward is a conversation per product, and the human picks which one they
// are talking to: t on the triage table opens the steward picker, which lists
// every product's stewards — the live one, the one a reboot interrupted, the
// ones put down — under a "new steward" line of its own. enter jumps into a
// live steward, reopens an older one where it left off, or starts a new one;
// x ends the live or interrupted one, which stays reopenable. Nothing starts or
// restarts a steward on its own: an interrupted steward is reopened, or
// replaced with a fresh one, when the human says which (ADR 0029).
//
// What a steward does still shows where the human already looks — its notes
// and answers on their rows — and the headline says how many are live, and
// how many were interrupted and are waiting to be reopened.

import (
	"errors"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/steward"
	"claude-dispatcher/internal/supervisor"
)

// stewardRow is one steward as the picker and the headline read it.
type stewardRow struct {
	id, product string
	state       steward.State
	opened      time.Time
}

// stewardsNow is every steward as the last load found it. A load reads the
// records and asks the supervisor once per product with a current steward.
var stewardsNow []stewardRow

// collectStewards reads every steward and what it is now.
func collectStewards() []stewardRow {
	var out []stewardRow
	for _, s := range stewardAll() {
		out = append(out, stewardRow{id: s.ID, product: steward.Key(s.Product), state: steward.StateOf(s), opened: s.OpenedAt})
	}
	return out
}

// The steward package, as seams so a test can drive the picker without a
// supervisor.
var (
	stewardAll     = steward.All
	stewardStart   = steward.Start
	stewardReopen  = steward.Reopen
	stewardEnd     = steward.End
	stewardRulings = steward.ActiveRulings
)

// stewardClause is the headline's word on the stewards: nothing while there are
// none, how many are live, and how many were interrupted — those are waiting on
// the human to reopen them or start fresh, so they are said apart.
func stewardClause() string {
	live, interrupted := 0, 0
	for _, s := range stewardsNow {
		switch s.state {
		case steward.Live:
			live++
		case steward.Interrupted:
			interrupted++
		}
	}
	var parts []string
	switch live {
	case 0:
	case 1:
		parts = append(parts, "steward live")
	default:
		parts = append(parts, countOf(live, "steward", "stewards")+" live")
	}
	if interrupted > 0 {
		parts = append(parts, countOf(interrupted, "steward", "stewards")+" interrupted")
	}
	return strings.Join(parts, " · ")
}

// stewardItem is one line of the picker: a steward, or the "new steward" line
// that closes each product's group (st nil).
type stewardItem struct {
	product string
	st      *stewardRow
}

// stewardShown is how many of a product's stewards the picker lists: the
// current one and the most recent before it. Older ones are still on record
// and reopenable by id (claude-dispatcher steward open <id>).
const stewardShown = 5

// stewardItems is the picker's lines: each product with a steward or a repo,
// its stewards newest first, then its "new steward" line.
func stewardItems() []stewardItem {
	names := slices.Clone(productOrder)
	for _, s := range stewardsNow {
		if !slices.Contains(names, s.product) {
			names = append(names, s.product)
		}
	}
	if len(names) == 0 {
		names = []string{steward.Unassigned}
	}
	var out []stewardItem
	for _, p := range names {
		shown := 0
		for i := range stewardsNow {
			if stewardsNow[i].product == p && shown < stewardShown {
				out = append(out, stewardItem{product: p, st: &stewardsNow[i]})
				shown++
			}
		}
		out = append(out, stewardItem{product: p})
	}
	return out
}

// stewardPickOpenFor opens the picker with the cursor on product's group: its
// current steward if it has one, else its "new steward" line.
func (m model) stewardPickOpenFor(product string) (model, tea.Cmd) {
	m.stewardPickOpen = true
	m.stewardPickCursor = 0
	items := stewardItems()
	m.stewardPickRulings = map[string][]steward.Ruling{}
	for _, it := range items {
		if _, read := m.stewardPickRulings[it.product]; !read {
			m.stewardPickRulings[it.product] = stewardRulings(it.product, time.Now())
		}
	}
	for i, it := range items {
		if it.product != product {
			continue
		}
		m.stewardPickCursor = i
		if it.st == nil || it.st.state != steward.Ended {
			break
		}
	}
	return m, nil
}

// stewardPickProduct is the product the picker opens on from triage: the
// selected row's, else the first one.
func (m model) stewardPickProduct() string {
	if r, ok := m.fleetSel(); ok && r.product != "" {
		return r.product
	}
	if len(productOrder) > 0 {
		return productOrder[0]
	}
	return steward.Unassigned
}

// updateStewardPick drives the picker while it owns the keyboard.
func (m model) updateStewardPick(k string) (model, tea.Cmd) {
	items := stewardItems()
	switch k {
	case "esc", "q", "t":
		m.stewardPickOpen = false
		return m, nil
	case "j", "down":
		m.stewardPickCursor = clampCursor(m.stewardPickCursor+1, len(items))
	case "k", "up":
		m.stewardPickCursor = clampCursor(m.stewardPickCursor-1, len(items))
	case "enter":
		if len(items) == 0 {
			return m, nil
		}
		it := items[clampCursor(m.stewardPickCursor, len(items))]
		m.stewardPickOpen = false
		switch {
		case it.st == nil:
			if live := liveStewardOf(it.product); live != nil {
				// A new conversation would end the one that is live, and
				// whatever the human is in the middle of there with it: that
				// is x, said on purpose, never a side effect of enter.
				m.stewardPickOpen = true
				m.notice = it.product + " has a live steward · x ends it (it stays reopenable), then start a new one"
				return m, nil
			}
			m.notice = "starting " + it.product + "'s steward…"
			return m, stewardStartCmd(it.product)
		case it.st.state == steward.Live:
			return m.attachSession(supervisor.Session{Name: steward.SessionName(it.product)}, "")
		default:
			m.notice = "reopening " + it.product + "'s steward…"
			return m, stewardReopenCmd(it.st.id)
		}
	case "x":
		if len(items) == 0 {
			return m, nil
		}
		it := items[clampCursor(m.stewardPickCursor, len(items))]
		if it.st == nil || it.st.state == steward.Ended {
			m.notice = "nothing to end here · x ends a live or interrupted steward"
			return m, nil
		}
		m.stewardPickOpen = false
		return m, stewardEndCmd(it.st.id)
	}
	return m, nil
}

// liveStewardOf is product's live steward as the last load saw it, or nil.
func liveStewardOf(product string) *stewardRow {
	for i := range stewardsNow {
		if stewardsNow[i].product == product && stewardsNow[i].state == steward.Live {
			return &stewardsNow[i]
		}
	}
	return nil
}

// stewardOpenedMsg is a start or reopen coming back. A steward that was
// already live is not an error here: the human asked to see it.
type stewardOpenedMsg struct {
	product string
	err     error
}

func stewardStartCmd(product string) tea.Cmd {
	return func() tea.Msg {
		_, err := stewardStart(product)
		return stewardOpenedMsg{product: product, err: err}
	}
}

func stewardReopenCmd(id string) tea.Cmd {
	return func() tea.Msg {
		s, err := stewardReopen(id)
		product := ""
		if s != nil {
			product = steward.Key(s.Product)
		}
		return stewardOpenedMsg{product: product, err: err}
	}
}

func stewardEndCmd(id string) tea.Cmd {
	return func() tea.Msg {
		s, err := stewardEnd(id)
		if err != nil {
			return actionMsg{notice: "steward: " + err.Error()}
		}
		return actionMsg{notice: steward.Key(s.Product) + "'s steward ended · t reopens it"}
	}
}

// onStewardOpened attaches to the steward, or says why it could not start.
func (m model) onStewardOpened(msg stewardOpenedMsg) (model, tea.Cmd) {
	// Untrusted still attaches: the trust dialog is on the screen it opens.
	if msg.err != nil && !errors.Is(msg.err, steward.ErrRunning) && !errors.Is(msg.err, steward.ErrUntrusted) {
		m.notice = "steward: " + msg.err.Error()
		return m.requestLoad(loadPlain)
	}
	if msg.product == "" {
		return m.requestLoad(loadPlain)
	}
	// No socket: a steward belongs to a product rather than to one repository,
	// so it lives on the default server rather than any repo's own (ADR 0014).
	return m.attachSession(supervisor.Session{Name: steward.SessionName(msg.product)}, "")
}

// viewStewardPick draws the picker.
func (m model) viewStewardPick(w, h int) string {
	cw := w - 2*pad
	if cw < 10 {
		cw = w
	}
	var out []string
	out = append(out, fg(cDim, "stewards · one conversation per product, kept so you can pick one back up"))
	out = append(out, fg(cRule, strings.Repeat("─", cw)))
	items := stewardItems()
	sel := clampCursor(m.stewardPickCursor, len(items))
	last := ""
	for i, it := range items {
		if it.product != last {
			if last != "" {
				out = append(out, "")
			}
			out = append(out, fg(cMid, it.product))
			if rs := m.stewardPickRulings[it.product]; len(rs) > 0 {
				out = append(out, fg(cDim, "  "+rulingsLine(rs)))
			}
			last = it.product
		}
		bg, marker, tone := cTransparent, " ", cFg
		if i == sel {
			bg, marker, tone = cSel, "▸", cWhite
		}
		if it.st == nil {
			out = append(out, row(cw, bg, c(marker, 2, cMid), c("+", 2, cDim), flexc("new steward", tone)))
			continue
		}
		glyph, word, wordTone := "·", "ended", cFaint
		switch it.st.state {
		case steward.Live:
			glyph, word, wordTone = "●", "live", cGreen
		case steward.Interrupted:
			glyph, word, wordTone = "◌", "interrupted · enter reopens", cAmber
		}
		out = append(out, row(cw, bg,
			c(marker, 2, cMid),
			c(glyph, 2, wordTone),
			c(it.st.id, 14, tone),
			c("opened "+floorAge(it.st.opened)+" ago", 18, cDim),
			flexc(word, wordTone),
		))
	}
	out = append(out, "")
	out = append(out, fg(cFaint, "j/k choose · enter jumps in, reopens or starts · x ends one · esc closes"))
	return clampLines(gutter(strings.Join(out, "\n"), pad), h)
}

// rulingsLine is a product's standing rulings in one line: how many, and the
// hand one grants with when it ends, since that is the one with consequences.
func rulingsLine(rs []steward.Ruling) string {
	line := countOf(len(rs), "ruling", "rulings") + " standing"
	for _, r := range rs {
		if slices.Contains(r.Allow, steward.AllowMerge) {
			where := ""
			if r.Repo != "" {
				where = " in " + r.Repo
			}
			return line + " · may merge" + where + " until " + r.Until.Local().Format("Mon 15:04")
		}
	}
	return line
}
