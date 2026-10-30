package cockpit

// decsearch.go is `/` on the DECISIONS lens: one query over every decision in
// every repo, its hits listed best first where the repo's own list was.
//
// The query is read as textsearch.go reads every one: words apart must all
// match, smart-case, each as an id, in the head or inside one word of the
// prose. For a decision the id is its number — "4", "0004", "#4" and "adr-4"
// all name ADR 0004, "d18" names D18 — the head is "id title", and the prose
// is its context, decision and consequences.
//
// The list is filtered, where the products lens's search lights its rows in
// place. A portfolio row's neighbours are the point of it; a decision found by
// what it says cannot show why on a row that only carries its title, so here
// each hit says so on the line under it — the words its prose matched, lit —
// and forty hits out of two hundred are the answer rather than two hundred rows
// to page through. esc then leaves the search on the decision the cursor is
// on, in its repo, among its neighbours: you searched to get there.
//
// What it costs is kept to what changes. The prose is cut up once per load, in
// the load (collectDecisions), never on the keyboard's thread; a word is
// matched once however often it is written, and every place it is written is
// then a lookup; and only the hits on screen are drawn and lit. Measured on 75
// checkouts of one repo — 14,526 decisions, 2.9 million words — a one-letter
// query took 1.1s and drawing its hits 1.3s more before any of that, and 22ms
// and 2ms after; cutting it up takes the load 0.4s.

import (
	"reflect"
	"slices"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// decRec is one decision as the search reads it: which repo lists it and
// where.
type decRec struct {
	repo string
	at   int // its index in decisions[repo]
	d    decision
}

// decIndex is every decision cut up for searching (textsearch.go), and what
// it was cut from. Record i of the text index is recs[i]: its head is "id
// title", or the title alone when it has no id, and its prose is decProse.
type decIndex struct {
	textIndex
	recs []decRec

	src   uintptr  // the decisions map it was cut from, by identity
	order []string // and the repo order it was walked in
}

// decProse is the prose a decision is searched in, in the order the body pane
// draws it.
func decProse(d decision) [3]string { return [3]string{d.context, d.decision, d.consequences} }

// buildDecIndex cuts up byRepo's decisions, walking the repos in order.
func buildDecIndex(order []string, byRepo map[string][]decision) *decIndex {
	ix := &decIndex{
		textIndex: newTextIndex(),
		src:       reflect.ValueOf(byRepo).Pointer(),
		order:     append([]string(nil), order...),
	}
	for _, repo := range order {
		for at, d := range byRepo[repo] {
			head := d.title
			if d.id != "" {
				head = d.id + " " + d.title
			}
			var keys []string
			if k := decIDKey(d.id); k != "" {
				keys = []string{k}
			}
			p := decProse(d)
			ix.recs = append(ix.recs, decRec{repo: repo, at: at, d: d})
			ix.add(head, keys, p[:]...)
		}
	}
	ix.seal()
	return ix
}

// currentDecIndex is the index of the decisions the lens is showing: the one
// the load built beside them, or — where they were put there some other way,
// as the tests do — one cut up now and kept for the next query.
func currentDecIndex() *decIndex {
	if ix := decisionIndex; ix != nil && ix.src == reflect.ValueOf(decisions).Pointer() &&
		slices.Equal(ix.order, decisionRepoOrder) {
		return ix
	}
	decisionIndex = buildDecIndex(decisionRepoOrder, decisions)
	return decisionIndex
}

// decIDKey is an id as a query compares it: lowercased, a leading "#" or "adr"
// taken off, and the zeros a number is padded with dropped — "4", "0004", "#4"
// and "ADR-0004" are all "4", "D18" and "d018" are "d18". "" for anything that
// is not shaped like an id, so a word of prose never names a decision by it.
func decIDKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimLeft(strings.TrimPrefix(s, "adr"), "-_ ")
	i := 0
	for i < len(s) && s[i] >= 'a' && s[i] <= 'z' {
		i++
	}
	prefix, num := s[:i], s[i:]
	if num == "" || num[0] < '0' || num[0] > '9' {
		return ""
	}
	for _, r := range num {
		if (r < '0' || r > '9') && r != '.' {
			return ""
		}
	}
	if num = strings.TrimLeft(num, "0"); num == "" || num[0] == '.' {
		num = "0" + num
	}
	return prefix + num
}

// decIDScore names a decision only by its id exactly: "4" is ADR 0004 and
// never ADR 0040.
func decIDScore(q, rec string) int {
	if q == rec {
		return textIDBonus
	}
	return 0
}

// ---- matching -------------------------------------------------------------

// decFound is what a query found: its hits best first, and what each of its
// words matched.
type decFound struct {
	ix    *decIndex
	terms []textTerm
	hits  []textHit
}

// decLit is what a search lit on one decision, as rune offsets into each part
// of it. The zero value lights nothing, which is how the lens draws a decision
// no search is up for.
type decLit struct {
	id, title map[int]bool
	prose     [3]map[int]bool // by decProse's order
}

// search is the query's hits, best first (textIndex.search). nil when the
// query has no words or there is nothing to search.
func (ix *decIndex) search(query string) *decFound {
	if ix == nil {
		return nil
	}
	terms, hits, ok := ix.textIndex.search(query, decIDKey, decIDScore)
	if !ok {
		return nil
	}
	return &decFound{ix: ix, terms: terms, hits: hits}
}

// rec is the decision a hit is.
func (f *decFound) rec(h textHit) decRec { return f.ix.recs[h.rec] }

// lit is what the query's words matched on a hit, worked out when it is drawn:
// every place in its id, title and prose that any of them matched.
func (f *decFound) lit(h textHit) decLit {
	var out decLit
	light := func(set *map[int]bool, at int) {
		if *set == nil {
			*set = map[int]bool{}
		}
		(*set)[at] = true
	}
	rec := f.ix.recs[h.rec]
	idLen := len([]rune(rec.d.id))
	for _, t := range f.terms {
		if t.byID[h.rec] {
			for p := 0; p < idLen; p++ {
				light(&out.id, p)
			}
		}
		if k := t.heads[h.rec]; k > 0 {
			for _, p := range t.headMt[k-1].Pos {
				switch {
				case rec.d.id == "":
					light(&out.title, p)
				case p < idLen:
					light(&out.id, p)
				case p > idLen:
					light(&out.title, p-idLen-1)
				}
			}
		}
	}
	copy(out.prose[:], litProse(f.terms, h.rec, len(out.prose)))
	return out
}

// ---- state ----------------------------------------------------------------

// decSearching is whether a query is up with anything in it to match.
func (m model) decSearching() bool { return strings.TrimSpace(m.decQuery) != "" }

// decSearchShown is whether the lens is drawn as a search: from the `/` that
// opens one, before anything is typed, until esc puts the repo's list back.
func (m model) decSearchShown() bool { return m.decTyping || m.decSearching() }

// decHits is the query's hits, best first.
func (m model) decHits() []textHit {
	if m.decFound == nil {
		return nil
	}
	return m.decFound.hits
}

// decSelected is the hit the cursor is on.
func (m model) decSelected() (textHit, bool) {
	hits := m.decHits()
	if len(hits) == 0 {
		return textHit{}, false
	}
	return hits[clampCursor(m.decAt, len(hits))], true
}

// decSelectedRec is the decision the cursor is on.
func (m model) decSelectedRec() (decRec, bool) {
	h, ok := m.decSelected()
	if !ok {
		return decRec{}, false
	}
	return m.decFound.rec(h), true
}

// decSearchStart gives the query line the keyboard. What was typed before is
// kept, so `/` on a search already up edits it rather than starting over.
func (m model) decSearchStart() model {
	m.decTyping = true
	return m
}

// decSearchRun matches the query as it now reads, cursor on the best hit.
func (m model) decSearchRun() model {
	m.decFound, m.decAt = currentDecIndex().search(m.decQuery), 0
	return m
}

// decSearchClear puts the repo's list back, no query and nothing lit.
func (m model) decSearchClear() model {
	m.decQuery, m.decTyping, m.decFound, m.decAt = "", false, nil, 0
	return m
}

// decSearchLand ends the search on the hit the cursor is on: its repo picked
// and the cursor on it in that repo's list.
func (m model) decSearchLand() model {
	if rec, ok := m.decSelectedRec(); ok {
		for i, r := range decisionRepoOrder {
			if r == rec.repo {
				m.decRepo, m.decCursor = i, rec.at
				break
			}
		}
	}
	return m.decSearchClear()
}

// decSearchRefresh runs the query again over what a load just brought in. The
// cursor stays on the decision it was on, found by where it is written, so a
// load landing between two keystrokes does not move it under the reader.
func (m model) decSearchRefresh() model {
	if !m.decSearching() {
		return m
	}
	prev, had := m.decSelectedRec()
	m = m.decSearchRun()
	if had {
		for i, h := range m.decHits() {
			if r := m.decFound.rec(h); r.repo == prev.repo && r.d.by == prev.d.by && r.d.title == prev.d.title {
				m.decAt = i
				break
			}
		}
	}
	return m
}

// updateDecSearch is the lens's keys while a search is up. handled is false
// for a key the lens beneath should still get.
//
// While the query is being typed it has the keyboard the way the naming prompt
// does: every letter is the query's, q and the digits included (handleKey
// routes here ahead of the global keys), and only the arrows move. enter gives
// the letters back — j/k walk the hits, o opens one, / edits the query again.
func (m model) updateDecSearch(k string) (model, tea.Cmd, bool) {
	last := maxi(len(m.decHits())-1, 0)
	if m.decTyping {
		switch k {
		case "esc":
			return m.decSearchClear(), nil, true
		case "enter":
			if !m.decSearching() {
				return m.decSearchClear(), nil, true
			}
			m.decTyping = false
			return m, nil, true
		case "up":
			m.decAt = maxi(m.decAt-1, 0)
			return m, nil, true
		case "down":
			m.decAt = mini(m.decAt+1, last)
			return m, nil, true
		case "backspace":
			if r := []rune(m.decQuery); len(r) > 0 {
				m.decQuery = string(r[:len(r)-1])
			}
			return m.decSearchRun(), nil, true
		case "ctrl+u":
			m.decQuery = ""
			return m.decSearchRun(), nil, true
		case "ctrl+w":
			q := strings.TrimRight(m.decQuery, " ")
			m.decQuery = q[:strings.LastIndex(q, " ")+1]
			return m.decSearchRun(), nil, true
		}
		// Text comes from the key message, never the key's name: a paste
		// arrives as one multi-rune message.
		if s, ok := typedTextFor(m.key, k); ok {
			m.decQuery += s
			return m.decSearchRun(), nil, true
		}
		return m, nil, true
	}

	if !m.decSearching() {
		return m, nil, false
	}
	switch k {
	case "j", "down":
		m.decAt = mini(m.decAt+1, last)
	case "k", "up":
		m.decAt = maxi(m.decAt-1, 0)
	case "/":
		m.decTyping = true
	case "esc":
		// One column shows the list or the body; esc out of the body is the
		// way back to the hits, as it is to the repo's list.
		if m.pane == "detail" && m.fit().dec <= 1 {
			m.pane = "list"
			return m, nil, true
		}
		return m.decSearchLand(), nil, true
	case "o":
		if rec, ok := m.decSelectedRec(); ok {
			mm, cmd := m.decOpen(rec.d)
			return mm, cmd, true
		}
	case "J", "K", "[", "]":
		// The hits are one list across every repo, best first: there is no
		// next repo or group in it to step to.
	default:
		return m, nil, false
	}
	return m, nil, true
}

// ---- drawing --------------------------------------------------------------

// decShown is the decision the body pane and the inline prose draw, and what
// the search lit on it. ok is false only when there is nothing to draw.
func (m model) decShown(list []decision, ci int) (decision, decLit, bool) {
	if m.decSearching() {
		h, ok := m.decSelected()
		if !ok {
			return decision{}, decLit{}, false
		}
		return m.decFound.rec(h).d, m.decFound.lit(h), true
	}
	if len(list) == 0 {
		return decision{}, decLit{}, false
	}
	return list[ci], decLit{}, true
}

// decHitCounts is how many hits each repo has, while a query is up.
func (m model) decHitCounts() map[string]int {
	if !m.decSearching() {
		return nil
	}
	n := map[string]int{}
	for _, h := range m.decHits() {
		n[m.decFound.rec(h).repo]++
	}
	return n
}

// decHitsBlock is the middle column while a search is up: the query, then
// each hit best first — its id and title lit where they matched, and on the
// line under them its repo and the words of its prose that matched, or where
// it is written when the title alone did. Only the hits that fit are drawn,
// around the cursor, with what is above and below them counted.
func (m model) decHitsBlock(cw, budget int) string {
	q := "/" + m.decQuery
	if m.decTyping {
		q += "▏"
	}
	hits := m.decHits()
	counts := m.decHitCounts()
	right := "every repo"
	switch {
	case m.decSearching() && len(hits) == 0:
		right = "no match"
	case m.decSearching():
		right = countOf(len(hits), "match", "matches") + " in " + countOf(len(counts), "repo", "repos")
	}
	lines := []string{row(cw, "",
		c(q, dispWidth(q)+2, cSearchHit),
		seg{text: right, flex: true, align: alignRight, hex: cFaint},
	)}

	if !m.decSearching() {
		return vjoin(append(lines, "",
			line("type to search every decision in every repo:", cw, cMid, ""),
			line("  by its number — 4 finds 0004, d18 finds D18", cw, cFaint, ""),
			line("  by its title, fuzzily, the way fzf matches", cw, cFaint, ""),
			line("  by what it says, fuzzily inside a word", cw, cFaint, ""),
			line("  words apart must all match · a capital matches case", cw, cFaint, ""),
		)...)
	}
	if len(hits) == 0 {
		return vjoin(append(lines, "", line("nothing in any repo matches that", cw, cFaint, ""))...)
	}

	// Two lines a hit. When they do not all fit, a line each above and below
	// says how many are not drawn.
	at := clampCursor(m.decAt, len(hits))
	avail := maxi(budget-1, 3)
	per := maxi(avail/2, 1)
	start := 0
	if len(hits) > per {
		per = maxi((avail-2)/2, 1)
		start = mini(maxi(at-per/2, 0), len(hits)-per)
	}
	end := mini(start+per, len(hits))
	if start > 0 {
		lines = append(lines, fg(cFaint, "↑ "+countOf(start, "more match", "more matches")+" above"))
	}
	words := len(strings.Fields(m.decQuery))
	for n := start; n < end; n++ {
		h := hits[n]
		rec, lit := m.decFound.rec(h), m.decFound.lit(h)
		bg, titleColor, marker := "", cFg, ""
		if n == at {
			bg, titleColor, marker = cSel, cWhite, "▸"
		}
		st := decisionStatusMeta[rec.d.status]
		lines = append(lines, row(cw, bg,
			c(marker, 2, cMid), c(st.glyph, 2, st.color),
			c(rec.d.id, 7, cMid).highlight(lit.id, cSearchHit),
			flexc(rec.d.title, titleColor).highlight(lit.title, cSearchHit),
		))
		// The prose is why a hit is here whenever any word of the query was
		// best matched there; a title that matched every word needs no quote.
		text, snipLit, hex := rec.d.by, map[int]bool(nil), cFaint
		if h.snip.ok && h.inHead < words {
			text, snipLit = textSnippet(decProse(rec.d)[h.snip.field], h.snip.at, lit.prose[h.snip.field])
			hex = cMid
		}
		lines = append(lines, row(cw, "",
			c("", 4, ""), c(rec.repo, dispWidth(rec.repo)+2, cDim),
			flexc(text, hex).highlight(snipLit, cSearchHit),
		))
	}
	if end < len(hits) {
		lines = append(lines, fg(cFaint, "↓ "+countOf(len(hits)-end, "more match", "more matches")+" below"))
	}
	return vjoin(lines...)
}

// decWrapLit is decWrap with what a search lit painted in: the same lines,
// each in hex but for the runes in lit, which go in cSearchHit. lit holds rune
// offsets into s as written, so the words are walked where they sit in it.
func decWrapLit(s string, w int, lit map[int]bool, hex string) []string {
	if len(lit) == 0 {
		lines := decWrap(s, w)
		for i, l := range lines {
			lines[i] = fg(hex, l)
		}
		return lines
	}
	if w < 1 {
		w = 1
	}
	var out []string
	var cur strings.Builder
	curLit := map[int]bool{}
	col, n := 0, 0 // the line's display width, and its length in runes
	flush := func() {
		out = append(out, paintRuns(hex, cSearchHit, "", cur.String(), curLit))
		cur.Reset()
		curLit = map[int]bool{}
		col, n = 0, 0
	}
	textWords(s, unicode.IsSpace, func(wd string, at int) {
		ww := dispWidth(wd)
		if n > 0 && col+1+ww > w {
			flush()
		}
		if n > 0 {
			cur.WriteString(" ")
			col, n = col+1, n+1
		}
		for i := range []rune(wd) {
			if lit[at+i] {
				curLit[n+i] = true
			}
		}
		cur.WriteString(wd)
		col, n = col+ww, n+len([]rune(wd))
	})
	if n > 0 || len(out) == 0 {
		flush()
	}
	return out
}
