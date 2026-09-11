// Package fuzzy is the cockpit's `/` search. It is fzf — the matcher itself,
// `github.com/junegunn/fzf/src/algo`, linked in and called in process — rather
// than something fzf-like written here.
//
// Two things follow from using the real one. The ranking is fzf's, so a query
// behaves the way it does in the terminal the human already searches in:
// consecutive characters, word and path-boundary starts, and camelCase
// transitions all score, which is why "ppapp" finds "playerpulse-app" above
// "pp-calendar-sync". And the matcher returns the POSITIONS it matched at, so
// the hit can be highlighted in place, in the table, with its columns and
// colours intact — an in-place search is the whole reason not to shell out to
// the binary and hand the screen away.
package fuzzy

import (
	"sort"
	"strings"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// algo.Init builds the character-class and bonus tables the scorer reads. It is
// package state that fzf's own main sets up, and without it the matcher does
// not merely score differently — it misses matches outright: before this,
// "papp" found no match at all in "Player-App-2", and ranked "soccerpulse-app"
// above "player-app", which `fzf -f papp` does not.
//
// "default" is the scheme fzf's CLI uses unless told otherwise, and the point
// of linking fzf is to behave as fzf does.
func init() { algo.Init("default") }

// Match is one hit: where it was found, how well, and which characters matched.
type Match struct {
	Index int   // index into the candidates passed to Rank
	Score int   // fzf's score; higher is better
	Pos   []int // rune offsets in the candidate that matched, ascending
}

// Rank scores every candidate against query and returns the hits, best first.
//
// An empty query matches nothing rather than everything. The callers use this
// to light up a table, and "every row is a hit" is not a search result — it is
// the table, with the cost of having drawn it twice.
//
// Matching is smart-case, as fzf's own default is: a lowercase query ignores
// case, and a query with any capital in it is taken literally. Anything else
// would make searching for a repo actually named `Player-App-2` impossible from
// a list where nearly every other name is lowercase.
func Rank(query string, candidates []string) []Match {
	if query == "" || len(candidates) == 0 {
		return nil
	}
	caseSensitive := strings.ToLower(query) != query
	// The matcher does NOT fold the pattern: case-insensitively it lowercases
	// the text it is scanning and compares against the pattern as given, so an
	// unfolded pattern would silently match nothing. fzf lowercases it when it
	// builds the term; so do we.
	if !caseSensitive {
		query = strings.ToLower(query)
	}
	pattern := []rune(query)
	// One slab, reused across the whole pass: it is fzf's scratch space for the
	// scoring matrices, and allocating one per row is the difference between a
	// keystroke costing nothing and costing a redraw.
	slab := util.MakeSlab(slab16, slab32)

	var out []Match
	for i, cand := range candidates {
		chars := util.ToChars([]byte(cand))
		res, pos := algo.FuzzyMatchV2(caseSensitive, true, true, &chars, pattern, true, slab)
		if res.Score <= 0 {
			continue
		}
		m := Match{Index: i, Score: res.Score}
		if pos != nil {
			m.Pos = append(m.Pos, *pos...)
			sort.Ints(m.Pos)
		}
		out = append(out, m)
	}
	// Best first, then fzf's own tiebreak: the shorter line wins, and after that
	// the order the caller gave us, so a redraw never reshuffles rows that
	// scored and measured the same. Score alone is not fzf's ranking — on this
	// portfolio "papp" scores `soccerpulse-app` one point above `player-app`,
	// and the binary still puts `player-app` first because it is shorter.
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		la, lb := len(candidates[out[a].Index]), len(candidates[out[b].Index])
		if la != lb {
			return la < lb
		}
		return out[a].Index < out[b].Index
	})
	return out
}

// Slab sizes: fzf's own defaults for its scoring scratch space.
const (
	slab16 = 100 * 1024
	slab32 = 2048
)

// PosSet turns a match's positions into a lookup, for a renderer walking a
// string rune by rune deciding what to highlight.
func PosSet(pos []int) map[int]bool {
	if len(pos) == 0 {
		return nil
	}
	set := make(map[int]bool, len(pos))
	for _, p := range pos {
		set[p] = true
	}
	return set
}
