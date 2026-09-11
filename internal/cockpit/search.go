package cockpit

// search.go is `/` on the products lens: a live fzf search over whatever list
// is in front of you, matched characters lit where they sit.
//
// It searches the list that has the keyboard, not a fixed one — the portfolio
// table, the assignment editor's repos, or the checkouts inside an unfolded
// row. That is the same rule the keymap uses for scope, and for the same
// reason: there is only ever one list you are looking at, and asking which one
// you meant would be asking about something already on screen.
//
// The matching is fzf's own (internal/fuzzy), so a query ranks the way it does
// in the terminal, and the positions it returns are what gets highlighted. The
// list is NOT filtered down to the hits: the table is a portfolio and the point
// of a row is often its neighbours — how many repos a product has, what else is
// stale beside it. Jumping between matches keeps that context; filtering throws
// it away and makes the count at the top of the screen a different number from
// the one you were reading a second ago.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/fuzzy"
	"claude-dispatcher/internal/keymap"
)

// searchWhere is which list a query applies to, derived from what has the
// keyboard rather than chosen.
type searchWhere int

const (
	searchProducts searchWhere = iota // the portfolio table
	searchRepos                       // the assignment editor's repo list
	searchFold                        // the checkouts inside an unfolded row
)

func (m model) searchWhere() searchWhere {
	switch {
	case m.clOpen && m.clFoldRow != "":
		return searchFold
	case m.clOpen:
		return searchRepos
	}
	return searchProducts
}

// searchCandidates is the list the query runs over, in the order it is drawn,
// so a hit's index is the row to move the cursor to.
func (m model) searchCandidates() []string {
	switch m.searchWhere() {
	case searchFold:
		row, ok := clFoldTarget(m.clRepos(), m.clFoldRow)
		if !ok {
			return nil
		}
		out := make([]string, 0, len(row.worktrees))
		for _, w := range row.worktrees {
			// Both halves are searchable, because both are how a checkout is
			// referred to: the directory it sits in and the branch it holds.
			out = append(out, baseName(w.path)+" "+w.branch)
		}
		return out
	case searchRepos:
		rows := m.clRepos()
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.name)
		}
		return out
	default:
		out := make([]string, 0, len(products))
		for _, p := range products {
			out = append(out, p.name)
		}
		return out
	}
}

// searchHits are the current query's matches, best first.
func (m model) searchHits() []fuzzy.Match {
	if m.searchText == "" {
		return nil
	}
	return fuzzy.Rank(m.searchText, m.searchCandidates())
}

// searchPosFor is the highlight for one row of the list being searched, or nil
// when that row is not a hit. Keyed by the row's index in the drawn order.
func (m model) searchPosFor(where searchWhere, index int) map[int]bool {
	if m.searchText == "" || m.searchWhere() != where {
		return nil
	}
	for _, h := range m.searchHits() {
		if h.Index == index {
			return fuzzy.PosSet(h.Pos)
		}
	}
	return nil
}

// clFoldHighlights is the fold's per-checkout highlight, indexed as its
// worktrees are.
//
// A checkout is searched as "<folder> <branch>" because both are how one is
// referred to, but only the folder is drawn as text the highlight can sit in —
// so a position past the folder is a match in the branch, and is dropped here
// rather than painted at an offset that means nothing.
func (m model) clFoldHighlights(r clRepoRow) []map[int]bool {
	if m.searchText == "" || m.searchWhere() != searchFold {
		return nil
	}
	out := make([]map[int]bool, len(r.worktrees))
	for _, h := range m.searchHits() {
		if h.Index >= len(r.worktrees) {
			continue
		}
		n := len([]rune(baseName(r.worktrees[h.Index].path)))
		set := map[int]bool{}
		for _, p := range h.Pos {
			if p < n {
				set[p] = true
			}
		}
		if len(set) > 0 {
			out[h.Index] = set
		}
	}
	return out
}

// searchJump moves the cursor of whichever list is being searched onto the
// nth hit, wrapping. delta is +1 for the next match and -1 for the previous.
func (m model) searchJump(delta int) model {
	hits := m.searchHits()
	if len(hits) == 0 {
		m.notice = "no match for " + m.searchText
		return m
	}
	m.searchAt = ((m.searchAt+delta)%len(hits) + len(hits)) % len(hits)
	target := hits[m.searchAt].Index
	switch m.searchWhere() {
	case searchFold:
		m.clFoldIdx = target
	case searchRepos:
		m.clRepo = target
	default:
		m.productCursor = target
	}
	m.notice = "match " + itoa(m.searchAt+1) + " of " + itoa(len(hits))
	return m
}

// searchStart opens the query line. The cursor is not moved yet: a search that
// jumped on its first keystroke would walk the table away from where the human
// was reading while they were still typing what they were looking for.
func (m model) searchStart() model {
	m.searchOpen, m.searchText, m.searchAt = true, "", -1
	return m
}

// searchClear puts the screen back: no query, no highlights, cursor where the
// search left it. Where it left it is deliberate — you searched to get there.
func (m model) searchClear() model {
	m.searchOpen, m.searchText, m.searchAt = false, "", -1
	return m
}

// updateSearch handles the keys while a query is being typed or is up. handled
// is false for keys the lens beneath should still get.
//
// Typing takes the keyboard the way the naming prompt does, and for the same
// reason: every letter is text, so nothing below may claim one. Once the query
// is committed with enter the letters go back to the lens, and only the search
// scope's own jump keys stay taken.
func (m model) updateSearch(k string) (model, tea.Cmd, bool) {
	if m.searchOpen {
		switch k {
		case "esc":
			return m.searchClear(), nil, true
		case "enter":
			// Commit: the query stays live and lit, the keyboard goes back.
			m.searchOpen = false
			if len(m.searchHits()) == 0 && m.searchText != "" {
				m.notice = "no match for " + m.searchText
				return m, nil, true
			}
			return m.searchJump(1), nil, true
		case "backspace":
			r := []rune(m.searchText)
			if len(r) > 0 {
				m.searchText = string(r[:len(r)-1])
			}
			m.searchAt = -1
			return m, nil, true
		}
		// Text comes from the key message, never the key's name — a paste
		// arrives as one multi-rune message.
		if s, ok := typedTextFor(m.key, k); ok {
			m.searchText += s
			m.searchAt = -1
			return m, nil, true
		}
		return m, nil, true
	}

	if m.searchText == "" {
		return m, nil, false
	}
	switch m.keys.Resolve(keymap.Search, k) {
	case "n":
		return m.searchJump(1), nil, true
	case "ctrl+n":
		return m.searchJump(-1), nil, true
	case "esc":
		return m.searchClear(), nil, true
	}
	return m, nil, false
}

// searchLine is the query as it is drawn, with a cursor while it is being
// typed and the hit count once it is not.
func (m model) searchLine() string {
	if m.searchText == "" && !m.searchOpen {
		return ""
	}
	if m.searchOpen {
		return "/" + m.searchText + "▏"
	}
	hits := m.searchHits()
	if len(hits) == 0 {
		return "/" + m.searchText + " · no match"
	}
	at := m.searchAt + 1
	if at < 1 {
		at = 1
	}
	// Kept short on purpose: this sits at the right-hand end of a header the
	// pane truncates, and a hint that gets cut in half ("… ctrl+n ba") is worse
	// than one that names the two keys and trusts the arrow between them.
	return "/" + m.searchText + " · " + itoa(at) + "/" + itoa(len(hits)) +
		" · " + m.keys.KeyFor("search.prev") + "◂▸" + m.keys.KeyFor("search.next")
}

// baseName is the last path segment, without dragging path/filepath into the
// hot path of a redraw.
func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
