package cockpit

// textsearch.go is the matcher behind every `/` that filters a list of
// records by what they say: the decisions lens (decsearch.go) and the backlog
// (blsearch.go). Each record is a head — the line a reader knows it by, matched
// as fzf matches a line — the ids a query can name it by, and prose.
//
// The query is read the way fzf reads one: words separated by spaces must all
// match, and each is smart-case — all lowercase ignores case, a capital makes
// it literal. A word can match a record three ways:
//
//   - as one of its ids, compared as the caller says (an exact number, a
//     prefix of one), and a record a query names by its id is above every
//     other hit, because a number is the one thing a reader types that means
//     exactly one record.
//   - in its head, fuzzily, as fzf matches a line: "ghostclr" finds "A ghost
//     cannot clear itself".
//   - in its prose, fuzzily WITHIN ONE WORD. fzf over a whole paragraph
//     matches nearly anything: measured on this repo's CLAUDE.md, "gate" was
//     found in 46 of its 50 decisions, the second best of them "get a te",
//     because 2,400 characters of prose hold most short subsequences
//     somewhere. Kept inside a word a fuzzy match is still forgiving —
//     "dispatchr" finds dispatcher, "cfg" finds config — and stops being noise.
//
// What it costs is kept to what changes: the prose is cut up once, a word is
// matched once however often it is written, and every place it is written is
// then a lookup. See decsearch.go for what that measured.

import (
	"sort"
	"strings"
	"unicode"

	"claude-dispatcher/internal/fuzzy"
)

// textOcc is one place a word is written: which distinct word, in which field
// of its record, at which rune offset.
type textOcc struct {
	word, at int32
	field    uint8
}

// textVocab is the prose of every record cut one way: each distinct word
// once, and every place one is written, record by record.
type textVocab struct {
	words []string // each distinct word, as written
	fold  []string // the same, lowercased, for the prefilter
	ascii []bool   // whether it is plain ASCII (see candidates)
	occ   []textOcc
	from  []int // occ[from[r]:from[r+1]] are record r's
	ids   map[string]int32
}

func (v *textVocab) add(w string, field, at int) {
	id, ok := v.ids[w]
	if !ok {
		id = int32(len(v.words))
		v.ids[w] = id
		v.words = append(v.words, w)
		v.fold = append(v.fold, strings.ToLower(w))
		v.ascii = append(v.ascii, isASCII(w))
	}
	v.occ = append(v.occ, textOcc{word: id, at: int32(at), field: uint8(field)})
}

// of is record r's occurrences.
func (v *textVocab) of(r int) []textOcc { return v.occ[v.from[r]:v.from[r+1]] }

// textIndex is records cut up for searching.
//
// The prose is cut two ways, because what a word is depends on what is being
// looked for. Split on spaces, "(README.md:163-181)." is one word, and "D18"
// matched it fuzzily — D in README, then 1, then 8 — on a decision that
// mentions no D18 at all. So a query word made only of letters and digits is
// matched inside the runs of letters and digits the prose is made of
// ("README", "md", "163", "181"), and only one with punctuation of its own —
// "kill-session", "origin/head", "4.5s" — inside the space-separated words it
// can only be part of.
type textIndex struct {
	heads []string   // each record's head, as the candidates fuzzy.Rank takes
	keys  [][]string // the ids a query can name each record by, as idKey spells them
	runs  textVocab  // the prose's runs of letters and digits
	words textVocab  // the prose's space-separated words
}

func newTextIndex() textIndex {
	return textIndex{
		runs:  textVocab{ids: map[string]int32{}},
		words: textVocab{ids: map[string]int32{}},
	}
}

// add cuts one record up: its head, the ids a query can name it by, and its
// prose, field by field in the order the record draws it.
func (ix *textIndex) add(head string, keys []string, prose ...string) {
	ix.heads = append(ix.heads, head)
	ix.keys = append(ix.keys, keys)
	ix.runs.from = append(ix.runs.from, len(ix.runs.occ))
	ix.words.from = append(ix.words.from, len(ix.words.occ))
	for f, text := range prose {
		textWords(text, unicode.IsSpace, func(w string, off int) { ix.words.add(w, f, off) })
		textWords(text, notAlnum, func(w string, off int) { ix.runs.add(w, f, off) })
	}
}

// seal closes the last record. The lookup is for building; what is kept is
// read by position.
func (ix *textIndex) seal() {
	ix.runs.from = append(ix.runs.from, len(ix.runs.occ))
	ix.words.from = append(ix.words.from, len(ix.words.occ))
	ix.runs.ids, ix.words.ids = nil, nil
}

// textWords calls fn with each word of s, words being what lies between the
// runes apart says separate them, and the rune offset each starts at:
// strings.FieldsFunc, keeping where each word was.
func textWords(s string, apart func(rune) bool, fn func(w string, at int)) {
	r := []rune(s)
	for i := 0; i < len(r); {
		for i < len(r) && apart(r[i]) {
			i++
		}
		j := i
		for j < len(r) && !apart(r[j]) {
			j++
		}
		if j > i {
			fn(string(r[i:j]), i)
		}
		i = j
	}
}

func notAlnum(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }

// isAlnum is whether s is only letters and digits.
func isAlnum(s string) bool { return strings.IndexFunc(s, notAlnum) < 0 }

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// candidates is the distinct words term could match, and each one's index in
// v.words. fuzzy.Rank would find the same set; this only spares it the words
// that do not hold term's letters in order at all, which is nearly all of
// them. A word with anything outside ASCII is always passed on, because fzf
// folds "é" to "e" and a plain comparison here would not.
func (v *textVocab) candidates(term string) ([]string, []int32) {
	exact := strings.ToLower(term) != term
	var cands []string
	var back []int32
	for i, w := range v.words {
		hay := v.fold[i]
		if exact {
			hay = w
		}
		if !v.ascii[i] || subsequence(term, hay) {
			cands = append(cands, w)
			back = append(back, int32(i))
		}
	}
	return cands, back
}

// subsequence reports whether p's runes appear in s in order.
func subsequence(p, s string) bool {
	pr := []rune(p)
	i := 0
	for _, r := range s {
		if i < len(pr) && r == pr[i] {
			i++
		}
	}
	return i == len(pr)
}

// ---- matching -------------------------------------------------------------

// textTerm is what one word of a query matched, kept whole so a hit can be lit
// when it is drawn rather than for every hit there is.
type textTerm struct {
	key    string        // the word as an id (idKey), "" when it is not one
	byID   []bool        // record → whether the word named it by an id
	heads  []int32       // record → 1 + its match in headMt, 0 for none
	headMt []fuzzy.Match //
	v      *textVocab    // which cut of the prose the word is matched in
	words  []int32       // distinct word → 1 + its match in wordMt, 0 for none
	wordMt []fuzzy.Match
}

// textHit is one record the query found.
type textHit struct {
	rec    int // into the index's records
	score  int
	inHead int // how many of the query's words matched best in the id or head
	// snip is the prose word that matched best, for the line under the head,
	// among the query's words that matched best in the prose: a word the head
	// already shows explains nothing quoted. ok is false when no word did.
	snip textSnip
}

// textSnip is a place in a record's prose a query word matched: the field it
// is in, the rune offset of its first matched rune, and how well it matched.
type textSnip struct {
	field, at, score int
	ok               bool
}

// textIDBonus is the score of a record a query names by its id: above any
// fuzzy score one word can reach, which stays in the hundreds.
const textIDBonus = 1 << 20

// textIDs is how a query word names a record by one of its ids.
type textIDs struct {
	// key spells a query word as an id, "" when it cannot be one.
	key func(word string) string
	// score is how well that spelling names one of a record's ids: 0 for not
	// at all, textIDBonus for exactly.
	score func(q, rec string) int
	// only says the word names ids and nothing else, so it is not matched in
	// any head or prose. nil for never.
	only func(word string) bool
}

// search is the query's hits, best first: the highest score summed over its
// words, then the more of them that matched in the head, then the order the
// records were added in — so a redraw never reshuffles hits that tie. ok is
// false when the query has no words or there is nothing to search.
//
// ids says how a query word names a record by its id.
func (ix *textIndex) search(query string, ids textIDs) (terms []textTerm, hits []textHit, ok bool) {
	words := strings.Fields(query)
	n := len(ix.heads)
	if len(words) == 0 || n == 0 {
		return nil, nil, false
	}
	all := make([]textHit, n)
	matched := make([]int, n)

	for _, word := range words {
		t := textTerm{key: ids.key(word), byID: make([]bool, n), heads: make([]int32, n), v: &ix.words}
		if isAlnum(word) {
			t.v = &ix.runs
		}
		best := make([]int, n)
		found := make([]bool, n)
		inHead := make([]bool, n)
		snip := make([]textSnip, n) // this word's best place in each record's prose
		take := func(r, score int, head bool) {
			if !found[r] || score > best[r] {
				best[r], inHead[r] = score, head
			}
			found[r] = true
		}

		if t.key != "" {
			for r, keys := range ix.keys {
				top := 0
				for _, k := range keys {
					top = maxi(top, ids.score(t.key, k))
				}
				if top > 0 {
					t.byID[r] = true
					take(r, top, true)
				}
			}
		}
		t.words = make([]int32, len(t.v.words))
		if ids.only == nil || !ids.only(word) {
			ix.matchText(word, &t, snip, take)
		}

		for r := 0; r < n; r++ {
			if !found[r] {
				continue
			}
			matched[r]++
			all[r].score += best[r]
			if inHead[r] {
				all[r].inHead++
			} else if s := snip[r]; s.ok && (!all[r].snip.ok || s.score > all[r].snip.score) {
				all[r].snip = s
			}
		}
		terms = append(terms, t)
	}

	for r := 0; r < n; r++ {
		if matched[r] == len(words) {
			all[r].rec = r
			hits = append(hits, all[r])
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return hits[a].inHead > hits[b].inHead
	})
	return terms, hits, true
}

// matchText matches one query word in every record's head, as fzf matches a
// line, and inside one word of every record's prose, filling in what t matched
// and handing each record's best to take. snip gets the word's best place in
// each record's prose, which is where a quote comes from if the word matched
// best there.
func (ix *textIndex) matchText(word string, t *textTerm, snip []textSnip, take func(r, score int, head bool)) {
	t.headMt = fuzzy.Rank(word, ix.heads)
	for i, mt := range t.headMt {
		t.heads[mt.Index] = int32(i + 1)
		take(mt.Index, mt.Score, true)
	}

	cands, back := t.v.candidates(word)
	t.wordMt = fuzzy.Rank(word, cands)
	for i := range t.wordMt {
		t.wordMt[i].Index = int(back[t.wordMt[i].Index])
		t.words[t.wordMt[i].Index] = int32(i + 1)
	}
	if len(t.wordMt) == 0 {
		return
	}
	for r := range snip {
		for _, o := range t.v.of(r) {
			k := t.words[o.word]
			if k == 0 {
				continue
			}
			mt := t.wordMt[k-1]
			if s := &snip[r]; !s.ok || mt.Score > s.score {
				s.field, s.score, s.ok = int(o.field), mt.Score, true
				s.at = int(o.at)
				if len(mt.Pos) > 0 {
					s.at += mt.Pos[0]
				}
			}
			take(r, mt.Score, false)
		}
	}
}

// litProse is what the query's words matched in record r's prose, as rune
// offsets into each field.
func litProse(terms []textTerm, r, fields int) []map[int]bool {
	out := make([]map[int]bool, fields)
	for _, t := range terms {
		for _, o := range t.v.of(r) {
			k := t.words[o.word]
			if k == 0 || int(o.field) >= fields {
				continue
			}
			if out[o.field] == nil {
				out[o.field] = map[int]bool{}
			}
			for _, p := range t.wordMt[k-1].Pos {
				out[o.field][int(o.at)+p] = true
			}
		}
	}
	return out
}

// textSnipMax is how much of a prose field the line under a hit is cut from:
// more than any row is wide, and short enough that truncating it to the row
// costs nothing.
const textSnipMax = 240

// textSnippet is the stretch of a prose field a hit's second line shows: from
// a few words before at to past the width of any row, an ellipsis where it
// starts mid-text, and lit moved to match.
func textSnippet(text string, at int, lit map[int]bool) (string, map[int]bool) {
	r := []rune(text)
	start := maxi(at-24, 0)
	for start > 0 && start < at && !unicode.IsSpace(r[start-1]) {
		start++
	}
	end := mini(start+textSnipMax, len(r))
	prefix := ""
	if start > 0 {
		prefix = "…"
	}
	shift := len([]rune(prefix)) - start
	out := map[int]bool{}
	for p := range lit {
		if p >= start && p < end {
			out[p+shift] = true
		}
	}
	return prefix + string(r[start:end]), out
}

// litHead is what the query's words lit in record r's head: its id, a space and
// its title, or the title alone when idLen is 0. A word that named the record
// by an id lights the whole id; a head match lights the letters fzf matched,
// in the id or the title as they fall.
func litHead(terms []textTerm, r, idLen int) (id, title map[int]bool) {
	light := func(set *map[int]bool, at int) {
		if *set == nil {
			*set = map[int]bool{}
		}
		(*set)[at] = true
	}
	for _, t := range terms {
		if t.byID[r] {
			for p := 0; p < idLen; p++ {
				light(&id, p)
			}
		}
		if k := t.heads[r]; k > 0 {
			for _, p := range t.headMt[k-1].Pos {
				switch {
				case idLen == 0:
					light(&title, p)
				case p < idLen:
					light(&id, p)
				case p > idLen:
					light(&title, p-idLen-1)
				}
			}
		}
	}
	return id, title
}
