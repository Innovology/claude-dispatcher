package cockpit

import (
	"slices"
	"strings"
	"testing"
)

// blType opens the backlog's search and types q into it, a key at a time.
func blType(m model, q string) model {
	m = press(m, "/")
	for _, r := range q {
		k := string(r)
		if r == ' ' {
			k = "space"
		}
		m = press(m, k)
	}
	return m
}

// blSearchFixture is a backlog across the three sources and two products.
func blSearchFixture(t *testing.T) {
	t.Helper()
	backlogFixture(t, []string{"alpha", "beta"}, []ticket{
		{id: "alpha-api#12", src: "gh", title: "Checkout times out", repo: "alpha-api", product: "alpha", pri: "med",
			body: "The cart page hangs when a coupon is applied.", tags: []string{"bug"}},
		{id: "alpha-api#120", src: "gh", title: "Add dark mode", repo: "alpha-api", product: "alpha", pri: "low",
			body: "Version 2 of the theme work.", tags: []string{"ui"}},
		{id: "beta-svc#3", src: "gh", title: "Retry webhooks", repo: "beta-svc", product: "beta", pri: "high",
			body: "Stripe sends them twice; see #12 for the cart side."},
		{id: "ENG-124", src: "lin", title: "Rotate the signing keys", product: "beta", pri: "urgent",
			body: "Quarterly.", tags: []string{"security"}},
		{id: "AB#12", src: "ado", title: "Board cleanup", pri: "med"},
	})
}

func TestBlIDKeysAndQueryWords(t *testing.T) {
	for id, want := range map[string][]string{
		"shop-api#12":  {"shop-api#12", "12"},
		"ENG-0124":     {"eng-0124", "124"},
		"AB#42":        {"ab#42", "42"},
		"Player-App-2": {"player-app-2", "2"},
		"no-number":    {"no-number"},
	} {
		if got := blIDKeys(id); !slices.Equal(got, want) {
			t.Errorf("blIDKeys(%q) = %q, want %q", id, got, want)
		}
	}
	for word, want := range map[string]string{
		"#12": "12", "0012": "12", "#0": "0", "ENG-124": "eng-124", "ab#42": "ab#42", "cart": "", "#": "",
	} {
		if got := blIDKey(word); got != want {
			t.Errorf("blIDKey(%q) = %q, want %q", word, got, want)
		}
	}
	if blIDScore("12", "12") != textIDBonus || blIDScore("12", "120") != textIDBonus/2 || blIDScore("12", "21") != 0 {
		t.Error("an exact number beats one it begins, and anything else is no match")
	}
	if blIDScore("eng-12", "eng-124") != 0 {
		t.Error("only a bare number names tickets by what it begins")
	}
	if !blIDOnly("#12") || blIDOnly("12") || blIDOnly("#cart") {
		t.Error("only # and digits is a number and nothing else")
	}
}

// "#12" is a number and nothing else. It names GitHub issue 12 first, whatever
// product it is in; then the tickets whose number starts with it, since a query
// is typed a digit at a time; and never the issue whose body mentions #12 —
// the reader typed the sign that means "issue number".
func TestBacklogSearchByNumber(t *testing.T) {
	blSearchFixture(t)
	m := blType(backlogModel(40), "#12")
	got := ids(m.blRows())
	if len(got) == 0 || (got[0] != "alpha-api#12" && got[0] != "AB#12") {
		t.Fatalf("an exact number first: %v", got)
	}
	for _, want := range []string{"alpha-api#12", "AB#12", "alpha-api#120", "ENG-124"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s missing from %v", want, got)
		}
	}
	if slices.Contains(got, "beta-svc#3") {
		t.Errorf("#12 matched a body that mentions it: %v", got)
	}
	if at := slices.Index(got, "alpha-api#120"); at < slices.Index(got, "alpha-api#12") {
		t.Errorf("120 only begins with 12 and comes after it: %v", got)
	}
	if m.ticketSelected().id != got[0] {
		t.Errorf("the cursor goes to the best hit, got %q", m.ticketSelected().id)
	}

	// Linear's identifier names its ticket whole, and its number alone too.
	m = blType(backlogModel(40), "eng-124")
	if got := ids(m.blRows()); len(got) == 0 || got[0] != "ENG-124" {
		t.Errorf("eng-124: %v", got)
	}
	m = blType(backlogModel(40), "#124")
	if got := ids(m.blRows()); !slices.Equal(got, []string{"ENG-124"}) {
		t.Errorf("#124: %v", got)
	}
}

// A word matches the title fuzzily, the way fzf matches a line, and the body
// and labels fuzzily inside one word. A hit found only in its body says why on
// the line under it.
func TestBacklogSearchByWhatItSays(t *testing.T) {
	blSearchFixture(t)
	m := blType(backlogModel(40), "chkout")
	if got := ids(m.blRows()); !slices.Equal(got, []string{"alpha-api#12"}) {
		t.Errorf("chkout: %v", got)
	}

	m = blType(backlogModel(40), "coupon")
	if got := ids(m.blRows()); !slices.Equal(got, []string{"alpha-api#12"}) {
		t.Fatalf("coupon: %v", got)
	}
	out := stripAnsi(m.View())
	if !strings.Contains(out, "a coupon is applied") {
		t.Errorf("a body hit should quote the words that matched:\n%s", out)
	}
	if !strings.Contains(out, "1 match in 1 product") {
		t.Errorf("the query line should count the hits:\n%s", out)
	}

	m = blType(backlogModel(40), "security")
	if got := ids(m.blRows()); !slices.Equal(got, []string{"ENG-124"}) {
		t.Errorf("a label: %v", got)
	}
	if out := stripAnsi(m.View()); !strings.Contains(out, "labels: security") {
		t.Errorf("a label hit says it was a label:\n%s", out)
	}

	// Words apart must all match.
	m = blType(backlogModel(40), "cart hangs")
	if got := ids(m.blRows()); !slices.Equal(got, []string{"alpha-api#12"}) {
		t.Errorf("cart hangs: %v", got)
	}
	m = blType(backlogModel(40), "zzqx")
	if got := m.blRows(); len(got) != 0 {
		t.Errorf("zzqx matched %v", ids(got))
	}
	if out := stripAnsi(m.View()); !strings.Contains(out, "no ticket matches /zzqx") {
		t.Errorf("an empty search should say so:\n%s", out)
	}
}

// While the query is typed it has the keyboard: q, digits and the backlog's own
// letters are text, so typing "p2 o" does not quit, switch lens, filter or
// re-sort. enter keeps the hits for j/k; esc puts the backlog back with the
// cursor on the ticket it was on.
func TestBacklogSearchOwnsTheKeyboardWhileTyped(t *testing.T) {
	blSearchFixture(t)
	m := blType(backlogModel(40), "q3 po")
	if m.lens != "backlog" || m.blQuery != "q3 po" || m.blProduct != "" || m.blOrder.by != "" {
		t.Fatalf("typed keys leaked: lens %q, query %q, product %q, order %q", m.lens, m.blQuery, m.blProduct, m.blOrder.by)
	}

	m = blType(backlogModel(40), "cart")
	hits := ids(m.blRows())
	if len(hits) != 2 {
		t.Fatalf("cart: %v", hits)
	}
	m = press(m, "enter")
	if m.blTyping || !m.blSearching() {
		t.Fatal("enter keeps the hits and gives the keyboard back")
	}
	m = press(m, "j")
	sel := m.ticketSelected().id
	if sel != hits[1] {
		t.Errorf("j walks the hits: %q, want %q", sel, hits[1])
	}
	// o while searching re-sorts the hits and leaves the browsing order alone.
	if m = press(m, "o"); m.blOrderNow().by != blByPriority || m.blOrder.by != "" {
		t.Errorf("o while searching: hits by %q, browsing by %q", m.blOrderNow().by, m.blOrder.by)
	}
	m = press(m, "esc")
	if m.blSearchShown() || len(m.blRows()) != len(backlogTickets) {
		t.Fatal("esc puts the whole backlog back")
	}
	if m.ticketSelected().id != sel {
		t.Errorf("esc moved the cursor off %q to %q", sel, m.ticketSelected().id)
	}
	if m.blOrderNow().by != blByPriority {
		t.Errorf("the browsing order is the one left, got %q", m.blOrderNow().by)
	}

	// / on a search already up edits it; backspace and ctrl+u edit the query.
	m = blType(m, "cartx")
	m = press(m, "backspace")
	if m.blQuery != "cart" {
		t.Errorf("backspace: %q", m.blQuery)
	}
	if m = press(m, "ctrl+u"); m.blQuery != "" || m.blFound != nil {
		t.Errorf("ctrl+u: %q", m.blQuery)
	}
	m = press(m, "esc")

	// Leaving the lens leaves its search.
	m = blType(m, "cart")
	m = press(m, "enter")
	m = press(m, "1")
	if m.blSearchShown() {
		t.Error("switching lens should clear the backlog's search")
	}
}

// A load replaces the backlog under a search. The hits name tickets by their
// place in it, so they are found again rather than read off the old one.
func TestBacklogSearchSurvivesALoad(t *testing.T) {
	blSearchFixture(t)
	m := blType(backlogModel(40), "coupon")
	m = press(m, "enter")
	backlogTickets = append([]ticket{{id: "new#1", title: "Coupon codes expire", pri: "med"}}, backlogTickets...)
	got := ids(m.blRows())
	if !slices.Equal(got, []string{"new#1", "alpha-api#12"}) && !slices.Equal(got, []string{"alpha-api#12", "new#1"}) {
		t.Errorf("after a load: %v", got)
	}
	m = m.blSearchRefresh()
	if m.blFound == nil || m.blFound.ix != currentBlIndex() {
		t.Error("the refresh should keep the search it ran")
	}
	if !slices.Contains(ids(m.blRows()), m.ticketSelected().id) {
		t.Errorf("the cursor is on a ticket the table holds, got %q", m.ticketSelected().id)
	}
}
