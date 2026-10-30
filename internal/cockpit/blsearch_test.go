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

// A hit is lit where it matched: the whole id when a number named it, the
// letters of the title fzf matched, and nothing quoted under a ticket whose
// every word matched its id or title.
func TestBacklogSearchLightsWhatMatched(t *testing.T) {
	blSearchFixture(t)
	at := func(id string) int {
		return slices.IndexFunc(backlogTickets, func(tk ticket) bool { return tk.id == id })
	}

	m := blType(backlogModel(40), "#12")
	lit := m.blFound.lit(at("alpha-api#12"))
	if len(lit.id) != len("alpha-api#12") || len(lit.title) != 0 {
		t.Errorf("#12 lights the whole id and no title: %+v", lit)
	}
	if _, _, ok := m.blFound.snippet(at("alpha-api#12")); ok {
		t.Error("a hit named by its number has nothing to quote")
	}

	m = blType(backlogModel(40), "chkout")
	lit = m.blFound.lit(at("alpha-api#12"))
	// fzf lights the letters it matched: the C and h of "Checkout", never its e.
	if !lit.title[0] || !lit.title[1] || lit.title[2] || len(lit.id) != 0 {
		t.Errorf("chkout lit %v in the title, %v in the id", lit.title, lit.id)
	}
	if _, _, ok := m.blFound.snippet(at("alpha-api#12")); ok {
		t.Error("a title hit is not quoted")
	}
	var none *blFound
	if l := none.lit(0); l.id != nil || none.quotes(0) {
		t.Error("no search lights nothing")
	}
}

// The query line's own keys: the arrows move through the hits while typing,
// ctrl+w takes a word back, a key that types nothing is ignored, and enter on
// an empty query is the same as esc.
func TestBacklogSearchEditingKeys(t *testing.T) {
	blSearchFixture(t)
	m := blType(backlogModel(40), "cart")
	hits := ids(m.blRows())
	if m = press(m, "down"); m.ticketSelected().id != hits[1] {
		t.Errorf("down while typing: %q", m.ticketSelected().id)
	}
	if m = press(m, "up"); m.ticketSelected().id != hits[0] {
		t.Errorf("up while typing: %q", m.ticketSelected().id)
	}
	m = press(m, "space")
	for _, r := range "hangs" {
		m = press(m, string(r))
	}
	if m = press(m, "ctrl+w"); m.blQuery != "cart " {
		t.Errorf("ctrl+w: %q", m.blQuery)
	}
	if m = press(m, "tab"); m.blQuery != "cart " || !m.blTyping {
		t.Errorf("a key that types nothing changed the query: %q", m.blQuery)
	}
	if out := m.footerView(); !strings.Contains(stripAnsi(out), "type to search") {
		t.Errorf("the footer while typing: %q", stripAnsi(out))
	}
	m = press(m, "enter")
	if out := stripAnsi(m.footerView()); !strings.Contains(out, "esc clears the search") {
		t.Errorf("the footer with a search kept: %q", out)
	}
	m = press(m, "esc")
	if m = press(m, "esc"); m.blSearchShown() || m.lens != "backlog" {
		t.Error("esc with no search up does nothing")
	}
	m = press(m, "/")
	if m = press(m, "enter"); m.blSearchShown() {
		t.Error("enter on an empty query closes it")
	}

	// An empty backlog has nothing to search.
	backlogFixture(t, nil, nil)
	m = blType(backlogModel(40), "cart")
	if m.blFound != nil || len(m.blRows()) != 0 {
		t.Error("an empty backlog finds nothing")
	}
	if out := stripAnsi(m.View()); !strings.Contains(out, "nothing in the backlog") {
		t.Errorf("an empty backlog says so, search or not:\n%s", out)
	}
}

// Leaving the lens clears its search the way esc does, so the cursor comes back
// on the ticket it was on. Clearing the fields alone left it on whatever row
// held that index in the whole backlog, drawn there until the next load moved
// it back to the ticket with no key pressed — and enter dispatched what was
// drawn in between.
func TestLeavingTheLensKeepsTheCursorOnItsTicket(t *testing.T) {
	var ts []ticket
	for i := 0; i < 20; i++ {
		ts = append(ts, ticket{id: "r#" + itoa(i), title: "ticket " + itoa(i), pri: "med", filed: daysAgo(40 - i)})
	}
	ts[7].title = "needle in the backlog"
	backlogFixture(t, nil, ts)
	m := blType(backlogModel(40), "needle")
	m = press(m, "enter")
	if m.ticketSelected().id != "r#7" {
		t.Fatalf("selected %q", m.ticketSelected().id)
	}
	m = press(press(m, "1"), "3")
	if m.blSearchShown() {
		t.Fatal("the search should be gone")
	}
	if got := m.ticketSelected().id; got != "r#7" {
		t.Errorf("back on the backlog the cursor is on %q, want r#7", got)
	}
	if got := m.blSearchRefresh().ticketSelected().id; got != "r#7" {
		t.Errorf("a load moved it to %q", got)
	}
}

// Every search starts best first. The order a search's hits were re-sorted in
// is that search's, and went on into the next one: re-sort "#123" by priority,
// clear it, search "#123" again, and the urgent 1230 sat above the issue the
// number names, with the cursor on it.
func TestEverySearchStartsBestFirst(t *testing.T) {
	backlogFixture(t, []string{"alpha", "beta"}, []ticket{
		{id: "alpha#1230", product: "alpha", pri: "urgent"},
		{id: "beta#123", product: "beta", pri: "low"},
	})
	m := blType(backlogModel(40), "#123")
	if m.ticketSelected().id != "beta#123" {
		t.Fatalf("the number's own ticket first, got %q", m.ticketSelected().id)
	}
	m = press(press(m, "enter"), "o")
	if m.blOrderNow().by != blByPriority || m.ticketSelected().id != "beta#123" {
		t.Fatalf("o re-sorts the hits: %q, cursor %q", m.blOrderNow().by, m.ticketSelected().id)
	}
	m = blType(press(m, "esc"), "#123")
	if m.blOrderNow().by != blByMatch || m.ticketSelected().id != "beta#123" {
		t.Errorf("a new search reads %q with the cursor on %q", m.blOrderNow().by, m.ticketSelected().id)
	}

	// Typed back to nothing, a query is no search and drops its order too.
	m = press(press(m, "enter"), "o")
	m = press(m, "/")
	for range "#123" {
		m = press(m, "backspace")
	}
	m = blType(press(m, "esc"), "#123")
	if m.blOrderNow().by != blByMatch {
		t.Errorf("after the query was emptied: %q", m.blOrderNow().by)
	}
}

// When the window's cut takes a ticket whose quote is the next line, the quote
// goes too. It was left under the "↑ more" line, quoting a ticket that was not
// on screen.
func TestACutTakesATicketsQuoteWithIt(t *testing.T) {
	var ts []ticket
	for i := 0; i < 8; i++ {
		ts = append(ts, ticket{id: "r#" + itoa(i), title: "ticket " + itoa(i), pri: "med",
			filed: daysAgo(40 - i), body: "a zebra crossing number " + itoa(i)})
	}
	backlogFixture(t, nil, ts)
	m := press(blType(backlogModel(40), "zebra"), "enter")
	rows := m.blRows()
	// Every hit is quoted: the divider, then a ticket and its quote each.
	// Five lines centred on r#2 (line 5) start at r#1, whose quote follows.
	m = m.blTo(2)
	out := m.blTable(rows, 80, 5)
	got := make([]string, len(out))
	for i, l := range out {
		got[i] = strings.TrimSpace(stripAnsi(l))
	}
	if !strings.HasPrefix(got[0], "── unassigned") || strings.Contains(got[0], "↑") {
		t.Errorf("line 0 = %q, want the product's divider", got[0])
	}
	if got[1] != "↑ 2 more tickets above" {
		t.Errorf("line 1 = %q, want the count where r#1's quote was", got[1])
	}
	if !strings.Contains(got[2], "r#2") || !strings.Contains(got[3], "zebra crossing number 2") {
		t.Errorf("the cursor's ticket and its quote follow: %q", got)
	}
	for _, l := range got {
		if strings.Contains(l, "number 1") {
			t.Errorf("r#1's quote is still drawn: %q", got)
		}
	}
}

// An empty table has nothing under the cursor to pick or dispatch: space used
// to pick the placeholder that says why the table is empty, a ticket with no
// id, and enter told the human that "" names no repo.
func TestAnEmptyTablePicksAndDispatchesNothing(t *testing.T) {
	blSearchFixture(t)
	m := press(blType(backlogModel(40), "zzqx"), "enter")
	if len(m.blRows()) != 0 {
		t.Fatal("the search should leave nothing")
	}
	m = press(m, "space")
	if len(m.picked) != 0 {
		t.Errorf("space picked %v", m.picked)
	}
	next, cmd := m.handleKey("enter")
	if m = next.(model); cmd != nil || m.notice != "" {
		t.Errorf("enter on an empty table: cmd %v, notice %q", cmd != nil, m.notice)
	}
}
