package fuzzy

import (
	"reflect"
	"testing"
)

// The portfolio this was written against, as the repo list actually reads.
var repos = []string{
	"Player-App-2", "attributor-ai", "better-altegio", "claude-dispatcher",
	"coding-practice", "dataviz", "design-system", "jobtracker", "k3s-personal",
	"kolchurin.dev", "new-web-app", "ordain", "player-app", "playerpulse",
	"pp-calendar-sync", "pp-oneshot-functions", "soccerpulse-app", "tabtracker",
}

func best(t *testing.T, query string) string {
	t.Helper()
	hits := Rank(query, repos)
	if len(hits) == 0 {
		t.Fatalf("%q matched nothing", query)
	}
	return repos[hits[0].Index]
}

// This is the reason for linking fzf rather than writing a matcher: the
// ranking is the one the human already has in their fingers.
func TestRankIsFzfRanking(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"papp", "player-app"},
		{"ppulse", "playerpulse"},
		{"cldisp", "claude-dispatcher"},
		{"kolch", "kolchurin.dev"},
		{"tabt", "tabtracker"},
	} {
		if got := best(t, tc.query); got != tc.want {
			t.Errorf("%q ranked %q first, want %q", tc.query, got, tc.want)
		}
	}
}

// Smart case, as fzf defaults: lowercase is case-insensitive, and a capital
// makes the query literal — otherwise a repo really named Player-App-2 could
// not be picked out of a list that is lowercase everywhere else.
func TestRankSmartCase(t *testing.T) {
	if got := len(Rank("player", repos)); got < 2 {
		t.Errorf("a lowercase query should ignore case, got %d hits", got)
	}
	hits := Rank("Player", repos)
	for _, h := range hits {
		if repos[h.Index] != "Player-App-2" {
			t.Errorf("a capitalised query should be literal, got %q", repos[h.Index])
		}
	}
	if len(hits) != 1 {
		t.Errorf("want only the literal match, got %d", len(hits))
	}
}

// An empty query is not "everything": the callers light up a table with this,
// and every row lit is the table again, drawn twice.
func TestRankEmptyQueryMatchesNothing(t *testing.T) {
	if got := Rank("", repos); got != nil {
		t.Errorf("empty query should match nothing, got %d", len(got))
	}
	if got := Rank("zzzz", repos); got != nil {
		t.Errorf("a query that matches nothing should return nothing, got %v", got)
	}
}

// Positions are what makes this an in-place search rather than a picker: they
// are what the table highlights.
func TestRankReturnsAscendingPositions(t *testing.T) {
	hits := Rank("pap", []string{"player-app"})
	if len(hits) != 1 {
		t.Fatalf("want one hit, got %d", len(hits))
	}
	pos := hits[0].Pos
	if len(pos) != 3 {
		t.Fatalf("want a position per query rune, got %v", pos)
	}
	for i := 1; i < len(pos); i++ {
		if pos[i] <= pos[i-1] {
			t.Fatalf("positions must ascend for a renderer walking the string: %v", pos)
		}
	}
	// And they must point at the characters they claim to.
	name := []rune("player-app")
	var got []rune
	for _, p := range pos {
		if p < 0 || p >= len(name) {
			t.Fatalf("position %d is outside %q", p, string(name))
		}
		got = append(got, name[p])
	}
	if string(got) != "pap" {
		t.Errorf("positions point at %q, want the query's own characters", string(got))
	}
}

func TestPosSet(t *testing.T) {
	if PosSet(nil) != nil {
		t.Error("no positions is no set, not an empty one")
	}
	set := PosSet([]int{2, 0, 5})
	want := map[int]bool{0: true, 2: true, 5: true}
	if !reflect.DeepEqual(set, want) {
		t.Errorf("got %v, want %v", set, want)
	}
}

// Ties keep the caller's order, so a redraw never reshuffles the table under
// the cursor.
func TestRankIsStableOnTies(t *testing.T) {
	cands := []string{"aaa-x", "aaa-y", "aaa-z"}
	for range 8 {
		hits := Rank("aaa", cands)
		if len(hits) != 3 {
			t.Fatalf("want all three, got %d", len(hits))
		}
		if hits[0].Index != 0 || hits[1].Index != 1 || hits[2].Index != 2 {
			t.Fatalf("ties should keep input order, got %v %v %v",
				hits[0].Index, hits[1].Index, hits[2].Index)
		}
	}
}
