package cockpit

// blsearch.go is `/` on the BACKLOG: one query over every open ticket, its
// hits the only rows left on the table until esc puts the rest back.
//
// A ticket is found the way textsearch.go finds anything — words apart must
// all match, smart-case — and each word can match it three ways:
//
//   - by its number. "#123", "123" and "0123" all name GitHub issue 123,
//     "eng-124" names Linear's ENG-124 and "124" names it too, and "ab#42" or
//     "42" names Azure's AB#42. A ticket named by its number is above every
//     other hit. A number the ticket's begins with names it as well, a step
//     below, because a query is typed a digit at a time: "#12" finds 12 first
//     and then 120…129, rather than nothing until the last digit lands. A word
//     starting with "#" is a number and nothing else — the reader typed the
//     sign that means "issue number", so "#2" must not also find every body
//     that mentions a 2.
//   - in its id and title, fuzzily, the way fzf matches a line: "chkout" finds
//     "Checkout times out", and "shop-api" finds every issue of that repo.
//   - in its body and labels, fuzzily inside one word, which is what keeps a
//     long issue body from matching nearly any query (see textsearch.go).
//
// The table is filtered rather than lit in place: the point of searching a
// backlog of hundreds is the dozen it leaves. A hit found in its body says
// why on the line under it, the words that matched lit, since its title alone
// cannot show it. The hits stay grouped by product, best first in each group
// until `o` picks another order.

import (
	"reflect"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// blIndex is the backlog cut up for searching: record i is tickets[i], its
// head "id title", its prose blProse.
type blIndex struct {
	textIndex
	tickets []ticket

	src uintptr // the backlog it was cut from, by identity
	n   int
}

// blProse is what a ticket is searched in besides its id and title, in the
// order the detail pane draws it.
func blProse(t ticket) [2]string { return [2]string{t.body, strings.Join(t.tags, ", ")} }

// blProseBody and blProseTags are blProse's fields, for the snippet line.
const (
	blProseBody = iota
	blProseTags
)

// buildBlIndex cuts up the tickets.
func buildBlIndex(ts []ticket) *blIndex {
	ix := &blIndex{textIndex: newTextIndex(), tickets: ts, src: reflect.ValueOf(ts).Pointer(), n: len(ts)}
	for _, t := range ts {
		p := blProse(t)
		ix.add(t.id+" "+t.title, blIDKeys(t.id), p[:]...)
	}
	ix.seal()
	return ix
}

// backlogIndex is the index of the backlog on screen, cut the first time a
// query runs over it and kept until a load brings a different one.
var backlogIndex *blIndex

func currentBlIndex() *blIndex {
	if ix := backlogIndex; ix != nil && ix.src == reflect.ValueOf(backlogTickets).Pointer() && ix.n == len(backlogTickets) {
		return ix
	}
	backlogIndex = buildBlIndex(backlogTickets)
	return backlogIndex
}

// blIDKeys are the ids a query can name a ticket by: its id whole, lowercased
// ("shop-api#12", "eng-124", "ab#42"), and the number it ends in with its
// padding dropped ("12", "124", "42"). Every source's id ends in its number,
// and that number is what a person reading a ticket out loud says.
func blIDKeys(id string) []string {
	low := strings.ToLower(id)
	i := len(low)
	for i > 0 && low[i-1] >= '0' && low[i-1] <= '9' {
		i--
	}
	if i == len(low) {
		return []string{low}
	}
	return []string{low, blTrimNum(low[i:])}
}

// blIDKey is a query word as an id: lowercased, a leading "#" taken off, a
// bare number's padding dropped. "" for a word with no digit in it, which
// cannot name a ticket.
func blIDKey(word string) string {
	w := strings.TrimPrefix(strings.ToLower(word), "#")
	if !strings.ContainsAny(w, "0123456789") {
		return ""
	}
	if blDigits(w) {
		return blTrimNum(w)
	}
	return w
}

// blIDScore names a ticket exactly by an id, or, for a bare number, by one its
// number begins with — a step below an exact match and above any fuzzy one.
func blIDScore(q, rec string) int {
	switch {
	case q == rec:
		return textIDBonus
	case blDigits(q) && blDigits(rec) && strings.HasPrefix(rec, q):
		return textIDBonus / 2
	}
	return 0
}

// blIDOnly is a word that is a number and nothing else: "#" and digits.
func blIDOnly(word string) bool {
	return strings.HasPrefix(word, "#") && blDigits(word[1:])
}

func blDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// blTrimNum drops the zeros a number is padded with, keeping a lone 0.
func blTrimNum(s string) string {
	if s = strings.TrimLeft(s, "0"); s == "" {
		return "0"
	}
	return s
}

var blIDs = textIDs{key: blIDKey, score: blIDScore, only: blIDOnly}

// blFound is what a query found: each hit by the position of its ticket in
// the backlog, and what each of the query's words matched.
type blFound struct {
	ix    *blIndex
	terms []textTerm
	hits  map[int]textHit // ticket index → its hit
	words int
}

// search runs query over the index. nil when the query has no words or there
// is no backlog to search.
func (ix *blIndex) search(query string) *blFound {
	terms, hits, ok := ix.textIndex.search(query, blIDs)
	if !ok {
		return nil
	}
	f := &blFound{ix: ix, terms: terms, hits: map[int]textHit{}, words: len(terms)}
	for _, h := range hits {
		f.hits[h.rec] = h
	}
	return f
}

// blLit is what a search lit on one ticket, as rune offsets into each part of
// it. The zero value lights nothing.
type blLit struct {
	id, title map[int]bool
	prose     [2]map[int]bool // by blProse's order
}

// lit is what the query's words matched on the ticket at index i.
func (f *blFound) lit(i int) blLit {
	var out blLit
	if f == nil {
		return out
	}
	light := func(set *map[int]bool, at int) {
		if *set == nil {
			*set = map[int]bool{}
		}
		(*set)[at] = true
	}
	idLen := len([]rune(f.ix.tickets[i].id))
	for _, t := range f.terms {
		if t.byID[i] {
			for p := 0; p < idLen; p++ {
				light(&out.id, p)
			}
		}
		if k := t.heads[i]; k > 0 {
			for _, p := range t.headMt[k-1].Pos {
				switch {
				case p < idLen:
					light(&out.id, p)
				case p > idLen:
					light(&out.title, p-idLen-1)
				}
			}
		}
	}
	copy(out.prose[:], litProse(f.terms, i, len(out.prose)))
	return out
}

// quotes is whether the hit at ticket index i is drawn with a quote under it:
// whether some word of the query was best matched in its body or labels, so
// its id and title cannot show why it is here. False with no search up.
func (f *blFound) quotes(i int) bool {
	if f == nil {
		return false
	}
	h, hit := f.hits[i]
	return hit && h.snip.ok && h.inHead < f.words
}

// snippet is the line under a hit that was found in its body or labels rather
// than its title: the stretch of prose its best word is in, lit. ok is false
// for a hit whose every word matched in its id or title, which needs no
// quote.
func (f *blFound) snippet(i int) (string, map[int]bool, bool) {
	if !f.quotes(i) {
		return "", nil, false
	}
	h := f.hits[i]
	// One line, so a body's line breaks become spaces — rune for rune, which
	// keeps every offset the search lit where it was.
	text := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, blProse(f.ix.tickets[i])[h.snip.field])
	s, lit := textSnippet(text, h.snip.at, f.lit(i).prose[h.snip.field])
	if h.snip.field == blProseTags {
		s = "labels: " + s
		shifted := map[int]bool{}
		for p := range lit {
			shifted[p+len([]rune("labels: "))] = true
		}
		lit = shifted
	}
	return s, lit, true
}

// ---- state ----------------------------------------------------------------

// blSearching is whether a query is up with anything in it to match.
func (m model) blSearching() bool { return strings.TrimSpace(m.blQuery) != "" }

// blSearchShown is whether the query line is drawn: from the `/` that opens
// it, before anything is typed, until esc takes it away.
func (m model) blSearchShown() bool { return m.blTyping || m.blSearching() }

// blSearchRun matches the query as it now reads, cursor on the best hit.
func (m model) blSearchRun() model {
	m.blFound = nil
	if m.blSearching() {
		m.blFound = currentBlIndex().search(m.blQuery)
	}
	return m.blTo(0)
}

// blSearchClear puts the whole backlog back, the cursor still on the ticket it
// was on: you searched to get there.
func (m model) blSearchClear() model {
	m.blQuery, m.blTyping, m.blFound = "", false, nil
	return m.blSync()
}

// blSearchRefresh runs the query again over what a load just brought in, and
// keeps the cursor on its ticket either way.
func (m model) blSearchRefresh() model {
	if m.blSearching() {
		m.blFound = currentBlIndex().search(m.blQuery)
	}
	return m.blSync()
}

// updateBlSearch is the backlog's keys while its query line has the keyboard.
// Every letter is the query's — q, the digits and the backlog's own keys
// included (handleKey routes here ahead of the global keys) — and only the
// arrows move. enter gives the letters back with the hits kept.
func (m model) updateBlSearch(k string) (model, tea.Cmd) {
	switch k {
	case "esc":
		return m.blSearchClear(), nil
	case "enter":
		if !m.blSearching() {
			return m.blSearchClear(), nil
		}
		m.blTyping = false
		return m, nil
	case "up":
		return m.blTo(m.backlogCursor - 1), nil
	case "down":
		return m.blTo(m.backlogCursor + 1), nil
	case "backspace":
		if r := []rune(m.blQuery); len(r) > 0 {
			m.blQuery = string(r[:len(r)-1])
		}
		return m.blSearchRun(), nil
	case "ctrl+u":
		m.blQuery = ""
		return m.blSearchRun(), nil
	case "ctrl+w":
		q := strings.TrimRight(m.blQuery, " ")
		m.blQuery = q[:strings.LastIndex(q, " ")+1]
		return m.blSearchRun(), nil
	}
	// Text comes from the key message, never the key's name: a paste arrives
	// as one multi-rune message.
	if s, ok := typedTextFor(m.key, k); ok {
		m.blQuery += s
		return m.blSearchRun(), nil
	}
	return m, nil
}
