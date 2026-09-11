package cockpit

// search_test.go covers `/` on the products lens: which list a query applies
// to, that the jump keys move the right cursor, and that the list is lit rather
// than filtered.

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

func searchFixture(t *testing.T) model {
	t.Helper()
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	products = []product{
		{name: "playerpulse"}, {name: "acme-shop"}, {name: "kolchurin.dev"},
	}
	reposByProduct = map[string][]repoRef{
		clUnassigned: {
			{name: "player-app", forge: "gh", last: "0d", path: "/src/player-app/main",
				worktrees: []repoWorktree{
					{path: "/src/player-app/main", branch: "main"},
					{path: "/src/player-app/ci-scheduling", branch: "ci-scheduling"},
					{path: "/src/player-app/r2w-wearables", branch: "r2w-wearables"},
				}},
			{name: "playerpulse", forge: "gh", last: "7d", path: "/src/playerpulse"},
			{name: "soccerpulse-app", forge: "gh", last: "1d", path: "/src/soccerpulse-app"},
		},
	}
	m := newModel()
	m.cfg = &config.Config{Products: map[string][]string{}}
	m.searchAt = -1
	return m
}

func typeQuery(m model, q string) model {
	for _, r := range q {
		m, _, _ = m.updateSearch(string(r))
	}
	return m
}

// The query applies to the list that has the keyboard. Nothing asks which one
// you meant, because there is only ever one in front of you.
func TestSearchTargetsTheListWithTheKeyboard(t *testing.T) {
	m := searchFixture(t)
	if got := m.searchWhere(); got != searchProducts {
		t.Errorf("with the editor closed the target is the portfolio, got %v", got)
	}
	m.clOpen = true
	if got := m.searchWhere(); got != searchRepos {
		t.Errorf("with the editor open the target is its repo list, got %v", got)
	}
	m.clFoldRow = "player-app"
	if got := m.searchWhere(); got != searchFold {
		t.Errorf("with a fold focused the target is its checkouts, got %v", got)
	}
}

// Typing takes the keyboard: every letter is text, so nothing beneath may
// claim one. `a` would otherwise open the assignment editor mid-query.
func TestSearchTypingOwnsTheKeyboard(t *testing.T) {
	m := searchFixture(t)
	m = m.searchStart()
	for _, k := range []string{"a", "n", "p"} {
		var handled bool
		m, _, handled = m.updateSearch(k)
		if !handled {
			t.Fatalf("%q should have been swallowed as text", k)
		}
	}
	if m.searchText != "anp" {
		t.Errorf("got %q, want the typed text", m.searchText)
	}
	if m.clOpen {
		t.Error("typing must not have opened the assignment editor")
	}
}

// Committing hands the letters back and lands on the first hit.
func TestSearchCommitJumpsToTheBestMatch(t *testing.T) {
	m := searchFixture(t)
	m.clOpen = true
	m = m.searchStart()
	m = typeQuery(m, "socc")
	m, _, _ = m.updateSearch("enter")

	if m.searchOpen {
		t.Error("enter should give the keyboard back")
	}
	rows := m.clRepos()
	if rows[m.clRepo].name != "soccerpulse-app" {
		t.Errorf("cursor landed on %q, want soccerpulse-app", rows[m.clRepo].name)
	}
}

// n and ctrl+n step through the hits and wrap, moving the list's own cursor.
func TestSearchJumpsForwardAndBack(t *testing.T) {
	m := searchFixture(t)
	m.clOpen = true
	m = m.searchStart()
	m = typeQuery(m, "pp")
	m, _, _ = m.updateSearch("enter")

	hits := m.searchHits()
	if len(hits) < 2 {
		t.Fatalf("expected several hits for pp, got %d", len(hits))
	}
	first := m.clRepo

	m, _, handled := m.updateSearch("n")
	if !handled {
		t.Fatal("n should be the search's while a query is up")
	}
	if m.clRepo == first {
		t.Error("n should have moved to another hit")
	}
	m, _, _ = m.updateSearch("ctrl+n")
	if m.clRepo != first {
		t.Errorf("ctrl+n should have gone back, got row %d want %d", m.clRepo, first)
	}

	// Wrapping: stepping past the end comes round rather than stopping.
	for range len(hits) {
		m, _, _ = m.updateSearch("n")
	}
	if m.clRepo != first {
		t.Errorf("a full cycle should return to where it started, got %d want %d", m.clRepo, first)
	}
}

// Without a query the jump keys are the lens's own again — `n` has to go on
// making products.
func TestSearchKeysAreGivenBackWhenTheQueryIsCleared(t *testing.T) {
	m := searchFixture(t)
	m.clOpen = true
	if _, _, handled := m.updateSearch("n"); handled {
		t.Error("with no query, n belongs to the lens")
	}
	m = m.searchStart()
	m = typeQuery(m, "pp")
	m, _, _ = m.updateSearch("enter")
	if _, _, handled := m.updateSearch("n"); !handled {
		t.Error("with a query up, n is the search's")
	}
	m, _, _ = m.updateSearch("esc")
	if m.searchText != "" {
		t.Error("esc should clear the query")
	}
	if _, _, handled := m.updateSearch("n"); handled {
		t.Error("once cleared, n belongs to the lens again")
	}
}

// The list is lit, not filtered: a row's neighbours are often the point of it,
// and a table that shrinks under a query makes the count at the top a different
// number from the one you were reading.
func TestSearchHighlightsWithoutFiltering(t *testing.T) {
	m := searchFixture(t)
	m.clOpen = true
	before := len(m.clLeft(120, 40))
	m = m.searchStart()
	m = typeQuery(m, "socc")
	m, _, _ = m.updateSearch("enter")
	body := m.clLeft(120, 40)

	if len(body) != before {
		t.Errorf("the list should be the same length, got %d then %d", before, len(body))
	}
	joined := strings.Join(body, "\n")
	for _, name := range []string{"player-app", "playerpulse", "soccerpulse-app"} {
		if !strings.Contains(joined, name) {
			t.Errorf("%q should still be on screen:\n%s", name, joined)
		}
	}
	// And the hit carries a highlight its neighbours do not. Asserted on the
	// positions rather than on escape codes: colour is stripped when the tests
	// run without a terminal, so the rendered string cannot show it.
	rows := m.clRepos()
	var lit, unlit int
	for i, r := range rows {
		if pos := m.searchPosFor(searchRepos, i); len(pos) > 0 {
			lit++
			if r.name != "soccerpulse-app" {
				t.Errorf("%q should not be lit for query socc", r.name)
			}
		} else {
			unlit++
		}
	}
	if lit != 1 || unlit != len(rows)-1 {
		t.Errorf("expected exactly one lit row, got %d lit and %d not", lit, unlit)
	}
}

// A search over the fold matches the branch as well as the folder, because both
// are how a checkout is referred to — but only offsets inside the folder can be
// painted, since that is the only part drawn as text.
func TestSearchInTheFoldMatchesBranchesAndPaintsOnlyTheFolder(t *testing.T) {
	m := searchFixture(t)
	m.clOpen = true
	m.clFoldRow = "player-app"
	m.clExpanded = map[string]bool{"player-app": true}
	m = m.searchStart()
	m = typeQuery(m, "wear")
	m, _, _ = m.updateSearch("enter")

	row, ok := clFoldTarget(m.clRepos(), "player-app")
	if !ok {
		t.Fatal("the fold row should exist")
	}
	if got := row.worktrees[m.clFoldIdx].branch; got != "r2w-wearables" {
		t.Errorf("the fold cursor landed on %q, want r2w-wearables", got)
	}
	for i, set := range m.clFoldHighlights(row) {
		n := len([]rune(baseName(row.worktrees[i].path)))
		for p := range set {
			if p >= n {
				t.Errorf("checkout %d highlights offset %d, outside the %d-rune folder name", i, p, n)
			}
		}
	}
}

// A query that matches nothing says so rather than moving the cursor somewhere
// arbitrary.
func TestSearchWithNoMatchSaysSo(t *testing.T) {
	m := searchFixture(t)
	m.clOpen = true
	before := m.clRepo
	m = m.searchStart()
	m = typeQuery(m, "zzzz")
	m, _, _ = m.updateSearch("enter")
	if m.clRepo != before {
		t.Errorf("nothing matched, so the cursor should not have moved: %d → %d", before, m.clRepo)
	}
	if !strings.Contains(m.notice, "no match") {
		t.Errorf("the notice should say so, got %q", m.notice)
	}
}
