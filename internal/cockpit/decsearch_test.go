package cockpit

// decsearch_test.go covers `/` on the decisions lens: what a query finds and in
// what order, what it lights, and how the query line takes the keyboard and
// gives it back.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func decSearchFixture(t *testing.T) model {
	t.Helper()
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	decisions = map[string][]decision{
		"claude-dispatcher": {
			{id: "0004", title: "A ghost cannot clear itself", status: "decided",
				by: "docs/adr/0004-a-ghost.md:1", file: "/r/cd/docs/adr/0004-a-ghost.md", line: 1, adr: true,
				context:      "A record whose session was taken away claimed working for as long as the cockpit stayed open.",
				decision:     "It is swept on every load, off one tmux listing.",
				consequences: "A suspended laptop makes the same row anywhere."},
			{id: "0010", title: "origin/HEAD is a cache, not an answer", status: "decided",
				by:       "docs/adr/0010-origin-head.md:1",
				context:  "refs/remotes/origin/HEAD is written once, by git clone.",
				decision: "The default is read from the remote with ls-remote --symref."},
			{id: "0014", title: "Every load gets a gate", status: "open",
				by:      "docs/adr/0014-a-gate.md:1",
				context: "Nothing in (README.md:163-181) says so, and it gets a test each time."},
			{title: "Who owns the introduction", status: "open", by: "docs/r2.md:7",
				context: "marked [NEEDS DECISION] in docs/r2.md"},
		},
		"player-app": {
			{id: "D4", title: "Monorepo", status: "superseded", group: "stack decisions",
				by:           "_docs/spec/stack-decisions.md:12",
				context:      "The tmux kill-session path was the only way the server learned.",
				consequences: "Superseded per D18."},
			{id: "D18", title: "Runtime: Node everywhere", status: "decided", group: "stack decisions",
				by:       "_docs/spec/stack-decisions.md:40",
				decision: "Node on every lane, so the dispatcher config stays in one file."},
		},
	}
	decisionRepoOrder = []string{"claude-dispatcher", "player-app"}
	decisionProduct = map[string]string{}
	m := newModel()
	m.width, m.height = 190, 44
	return press(m, "5")
}

// typeDec types q into the query line one key at a time, as a terminal would.
func typeDec(m model, q string) model {
	for _, r := range q {
		k := string(r)
		if r == ' ' {
			k = "space"
		}
		m = press(m, k)
	}
	return m
}

func hitIDs(m model) []string {
	var out []string
	for _, h := range m.decHits() {
		out = append(out, m.decFound.rec(h).d.id)
	}
	return out
}

// selected is the decision the cursor is on and what the search lit on it.
func selected(m model) (decision, decLit) {
	h, ok := m.decSelected()
	if !ok {
		return decision{}, decLit{}
	}
	return m.decFound.rec(h).d, m.decFound.lit(h)
}

func searchFor(m model, q string) model {
	m = press(m, "/")
	return typeDec(m, q)
}

func TestAnIDIsComparedAsANumber(t *testing.T) {
	for in, want := range map[string]string{
		"4": "4", "0004": "4", "#4": "4", "adr-0004": "4", "ADR0004": "4",
		"D18": "d18", "d018": "d18", "1.2": "1.2", "0": "0", "0.5": "0.5",
		"": "", "adr": "", "address": "", "ghost": "", "4.5s": "", "d": "",
	} {
		if got := decIDKey(in); got != want {
			t.Errorf("decIDKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// A number is the one query that means exactly one decision, however it is
// padded or prefixed, so the decision it names is first whatever else matches.
func TestADecisionIsFoundByItsNumberFirst(t *testing.T) {
	m := decSearchFixture(t)
	for _, q := range []string{"4", "0004", "#4", "adr-0004"} {
		got := hitIDs(searchFor(m, q))
		if len(got) == 0 || got[0] != "0004" {
			t.Errorf("%q should find 0004 first, got %v", q, got)
		}
	}
	// D18's own entry first, then the one that cites it — and not 0014, whose
	// "(README.md:163-181)" holds D, 1 and 8 in order inside one space-free
	// word, which is the noise matching letters and digits within their own
	// runs is for.
	got := hitIDs(searchFor(m, "D18"))
	if strings.Join(got, ",") != "D18,D4" {
		t.Errorf("D18 should find D18 then the decision citing it, got %v", got)
	}
	if got := hitIDs(searchFor(m, "d18")); len(got) == 0 || got[0] != "D18" {
		t.Errorf("an id ignores case, so d18 should find D18 first, got %v", got)
	}
}

// Titles match fuzzily, as fzf matches a line; prose matches fuzzily inside one
// word, because across a paragraph nearly anything matches.
func TestTitlesMatchFuzzilyAndProseWithinAWord(t *testing.T) {
	m := decSearchFixture(t)
	if got := hitIDs(searchFor(m, "ghostclr")); strings.Join(got, ",") != "0004" {
		t.Errorf("ghostclr should find the ghost decision by its title, got %v", got)
	}
	if got := hitIDs(searchFor(m, "dispatchr")); strings.Join(got, ",") != "D18" {
		t.Errorf("dispatchr should find dispatcher in D18's prose, got %v", got)
	}
	// Prose holds g, a, t and e in order across words — "it gets a test" in
	// 0014's — and none of it says gate, so none of it may match. The titles
	// match as fzf matches a line, scattered letters and all, and the one that
	// says gate outranks the ones that only spell it out.
	m = searchFor(m, "gate")
	if got := hitIDs(m); len(got) == 0 || got[0] != "0014" {
		t.Errorf("gate should find the decision titled with it first, got %v", got)
	}
	for _, h := range m.decHits() {
		for f, set := range m.decFound.lit(h).prose {
			if len(set) > 0 {
				t.Errorf("%s: nothing in its prose is a gate, but field %d lights %v", m.decFound.rec(h).d.id, f, set)
			}
		}
	}
}

// Words apart must all match, each anywhere in the decision; a word with
// punctuation of its own is matched in the space-separated words it can be part
// of; a capital makes a word match case.
func TestEveryWordMustMatchAndACapitalMatchesCase(t *testing.T) {
	m := decSearchFixture(t)
	if got := hitIDs(searchFor(m, "tmux kill")); strings.Join(got, ",") != "D4" {
		t.Errorf("tmux kill should need both words, got %v", got)
	}
	if got := hitIDs(searchFor(m, "kill-session")); strings.Join(got, ",") != "D4" {
		t.Errorf("kill-session should be found as written, got %v", got)
	}
	if got := hitIDs(searchFor(m, "origin/head")); len(got) == 0 || got[0] != "0010" {
		t.Errorf("origin/head should find 0010, got %v", got)
	}
	if got := hitIDs(searchFor(m, "Head")); len(got) != 0 {
		t.Errorf("Head with a capital matches case, and only HEAD is written, got %v", got)
	}
	if got := hitIDs(searchFor(m, "HEAD")); len(got) == 0 || got[0] != "0010" {
		t.Errorf("HEAD should find 0010, got %v", got)
	}
	if got := hitIDs(searchFor(m, "zzqx")); len(got) != 0 {
		t.Errorf("zzqx should find nothing, got %v", got)
	}
}

// What matched is lit where it sits: in the id, the title and the prose, as
// rune offsets into each.
func TestWhatMatchedIsLitWhereItSits(t *testing.T) {
	m := searchFor(decSearchFixture(t), "ghost swept")
	d, l := selected(m)
	if d.id != "0004" {
		t.Fatalf("ghost swept should select 0004, got %v", hitIDs(m))
	}
	lit := func(s string, set map[int]bool) string {
		var b strings.Builder
		for i, r := range []rune(s) {
			if set[i] {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	if got := lit(d.title, l.title); got != "ghost" {
		t.Errorf("the title should light ghost, lit %q", got)
	}
	if got := lit(d.decision, l.prose[1]); got != "swept" {
		t.Errorf("the decision should light swept, lit %q", got)
	}
	if d, l := selected(searchFor(decSearchFixture(t), "owns")); lit(d.title, l.title) != "owns" {
		t.Errorf("a decision with no id is matched on its title alone, lit %q", lit(d.title, l.title))
	}
	m = searchFor(decSearchFixture(t), "0004")
	if d, l := selected(m); lit(d.id, l.id) != "0004" {
		t.Errorf("an id match lights the whole id, lit %q", lit(d.id, l.id))
	}

	// The line under a prose hit quotes the prose, lit; a hit its title alone
	// explains says where it is written instead.
	out := ansi.Strip(m.decHitsBlock(80, 30))
	if !strings.Contains(out, "docs/adr/0004-a-ghost.md:1") {
		t.Errorf("a title hit should say where it is written:\n%s", out)
	}
	m = searchFor(decSearchFixture(t), "laptop")
	out = ansi.Strip(m.decHitsBlock(80, 30))
	if !strings.Contains(out, "A suspended laptop makes") {
		t.Errorf("a prose hit should quote what matched:\n%s", out)
	}
}

func TestASnippetStartsOnAWordBeforeTheMatch(t *testing.T) {
	text := "one two three four five six seven eight nine ten eleven twelve"
	at := strings.Index(text, "eleven")
	got, lit := textSnippet(text, at, map[int]bool{at: true, at + 1: true})
	if got != "…seven eight nine ten eleven twelve" {
		t.Errorf("a snippet should start on a word a little before the match, got %q", got)
	}
	r := []rune(got)
	for p := range lit {
		if r[p] != 'e' && r[p] != 'l' {
			t.Errorf("lit offset %d is %q, not the match", p, r[p])
		}
	}
	if got, _ := textSnippet(text, 4, nil); strings.HasPrefix(got, "…") {
		t.Errorf("a match near the start is quoted from the start, got %q", got)
	}
}

// decWrapLit wraps exactly as decWrap does; the light is the only difference.
func TestDecWrapLitWrapsAsDecWrapDoes(t *testing.T) {
	text := "a record whose  session was taken away claimed working for as long as the cockpit stayed open"
	for _, w := range []int{0, 8, 20, 200} {
		want := decWrap(text, w)
		for _, lit := range []map[int]bool{nil, {2: true, 3: true, 60: true}} {
			got := decWrapLit(text, w, lit, cMid)
			if len(got) != len(want) {
				t.Fatalf("w=%d: %d lines, want %d", w, len(got), len(want))
			}
			for i := range got {
				if ansi.Strip(got[i]) != want[i] {
					t.Errorf("w=%d line %d = %q, want %q", w, i, ansi.Strip(got[i]), want[i])
				}
			}
		}
	}
	if got := decWrapLit("", 10, map[int]bool{0: true}, cMid); len(got) != 1 || ansi.Strip(got[0]) != "" {
		t.Errorf("empty text should wrap to one empty line, got %q", got)
	}
}

// While the query is typed every letter is the query's — q does not quit, a
// digit does not switch lens, ? does not open help.
func TestTheQueryTakesEveryLetter(t *testing.T) {
	m := press(decSearchFixture(t), "/")
	for _, k := range []string{"q", "1", "?", ":", ",", "U", "j"} {
		next, cmd := m.handleKey(k)
		m = next.(model)
		if cmd != nil {
			t.Fatalf("%q returned a command while typing a query", k)
		}
	}
	if m.lens != "decisions" || m.helpOpen || m.paletteOpen || m.settings != nil || !m.decTyping {
		t.Fatalf("a key escaped the query: lens %q help %v palette %v typing %v", m.lens, m.helpOpen, m.paletteOpen, m.decTyping)
	}
	if m.decQuery != "q1?:,Uj" {
		t.Errorf("query = %q", m.decQuery)
	}
	// A paste arrives as one message of many runes.
	m.decQuery = ""
	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ghost")}
	m.key = paste
	m = press(m, paste.String())
	if m.decQuery != "ghost" {
		t.Errorf("a pasted query = %q, want ghost", m.decQuery)
	}
}

// Typing edits the query; enter keeps the hits and gives the letters back; /
// edits it again; esc then lands on the selected hit, in its repo's list.
func TestEnterKeepsTheHitsAndEscGoesToOne(t *testing.T) {
	m := searchFor(decSearchFixture(t), "stack")
	m = press(m, "backspace")
	m = typeDec(m, "k")
	if m.decQuery != "stack" {
		t.Fatalf("query = %q", m.decQuery)
	}
	m = press(m, "ctrl+w")
	if m.decQuery != "" {
		t.Errorf("ctrl+w should take the word back, got %q", m.decQuery)
	}
	m = typeDec(m, "node lane")
	m = press(m, "ctrl+w")
	if m.decQuery != "node " {
		t.Errorf("ctrl+w should take the last word, got %q", m.decQuery)
	}
	m = press(m, "ctrl+u")
	if m.decQuery != "" {
		t.Errorf("ctrl+u should clear the query, got %q", m.decQuery)
	}

	m = typeDec(m, "per")
	if len(m.decHits()) < 2 {
		t.Fatalf("per should match more than one decision, got %v", hitIDs(m))
	}
	m = press(m, "down")
	m = press(m, "up")
	m = press(m, "down")
	if m.decAt != 1 {
		t.Errorf("down/up while typing should move the cursor, at %d", m.decAt)
	}
	m = press(m, "enter")
	if m.decTyping || !m.decSearching() {
		t.Fatal("enter should keep the hits and give the keyboard back")
	}
	m = press(m, "k")
	m = press(m, "j")
	for _, k := range []string{"J", "K", "[", "]", "right", "left"} {
		m = press(m, k)
	}
	want, _ := selected(m)

	m = press(m, "/")
	if !m.decTyping || m.decQuery != "per" {
		t.Errorf("/ should edit the query as it was, got typing=%v %q", m.decTyping, m.decQuery)
	}
	m = press(m, "enter")

	m = press(m, "esc")
	if m.decSearchShown() {
		t.Fatal("esc should end the search")
	}
	if got := decisions[decisionRepoOrder[m.decRepo]][m.decCursor]; got.id != want.id {
		t.Errorf("esc should land on %s in its repo, landed on %s", want.id, got.id)
	}
}

// esc while typing is cancel: the repo's list comes back where it was.
func TestEscWhileTypingPutsTheListBack(t *testing.T) {
	m := decSearchFixture(t)
	m = press(m, "J")
	m = press(m, "j")
	repo, cur := m.decRepo, m.decCursor
	m = searchFor(m, "ghost")
	m = press(m, "esc")
	if m.decSearchShown() || m.decRepo != repo || m.decCursor != cur {
		t.Errorf("esc while typing should cancel and leave the list as it was: repo %d→%d cursor %d→%d",
			repo, m.decRepo, cur, m.decCursor)
	}
	// enter on nothing typed is a search abandoned, too.
	m = press(m, "/")
	m = press(m, "enter")
	if m.decSearchShown() {
		t.Error("enter on an empty query should close it")
	}
	// Once closed, the lens's own keys are its own again.
	if _, _, handled := m.updateDecSearch("j"); handled {
		t.Error("j should belong to the lens once the search is closed")
	}
}

// At one column esc first leaves the body, as it does on the repo's list.
func TestEscLeavesTheBodyFirstAtOneColumn(t *testing.T) {
	m := decSearchFixture(t)
	m.width = 90
	m = searchFor(m, "ghost")
	m = press(m, "enter")
	m = press(m, "right")
	renderClean(t, m, "search body at one column")
	m = press(m, "esc")
	if m.pane != "list" || !m.decSearching() {
		t.Fatalf("esc in the body should go back to the hits, pane %q searching %v", m.pane, m.decSearching())
	}
	m = press(m, "esc")
	if m.decSearchShown() {
		t.Error("esc on the hits should end the search")
	}
}

func TestOOpensTheSelectedHit(t *testing.T) {
	t.Setenv("VISUAL", "true")
	m := searchFor(decSearchFixture(t), "monorepo")
	m = press(m, "enter")
	if _, cmd := m.handleKey("o"); cmd == nil {
		t.Error("o on a hit should hand the terminal to the editor")
	}
	m = searchFor(decSearchFixture(t), "zzqx")
	m = press(m, "enter")
	if next, cmd := m.handleKey("o"); cmd != nil || !next.(model).decSearching() {
		t.Error("o with nothing matched should do nothing")
	}
}

// A load that lands while a search is up runs it again over what it brought,
// and the cursor stays on the decision it was on.
func TestALoadKeepsTheCursorOnItsDecision(t *testing.T) {
	m := searchFor(decSearchFixture(t), "a")
	m = press(m, "enter")
	for i, h := range m.decHits() {
		if m.decFound.rec(h).d.id == "D18" {
			m.decAt = i
		}
	}
	decisionRepoOrder = []string{"player-app", "claude-dispatcher"}
	m = m.decSearchRefresh()
	if d, _ := selected(m); d.id != "D18" {
		t.Errorf("after a load the cursor should still be on D18, is on %s", d.id)
	}
	// Without a query a load has nothing to run.
	if m = m.decSearchClear().decSearchRefresh(); m.decFound != nil {
		t.Error("a load with no query up should find nothing")
	}
}

// The load cuts the decisions up beside reading them, and a query uses that
// cut rather than making its own; decisions put there any other way are cut
// up when first searched, and the cut is kept for the next query.
func TestAQueryUsesTheIndexTheLoadBuilt(t *testing.T) {
	decSearchFixture(t)
	var s snapshot
	s.decisions, s.decisionRepoOrder = decisions, decisionRepoOrder
	s.decisionIndex = buildDecIndex(s.decisionRepoOrder, s.decisions)
	applySnapshot(s)
	if currentDecIndex() != s.decisionIndex {
		t.Error("a query should use the index its load built")
	}
	decisionRepoOrder = []string{"player-app", "claude-dispatcher"}
	ix := currentDecIndex()
	if ix == s.decisionIndex || ix.recs[0].repo != "player-app" {
		t.Error("an index of other decisions, or another order, must not be used")
	}
	if currentDecIndex() != ix {
		t.Error("an index cut up for a query should be kept for the next")
	}
}

// Only the hits that fit are drawn, around the cursor, with the rest counted.
func TestOnlyTheHitsThatFitAreDrawn(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	var list []decision
	for i := 0; i < 40; i++ {
		list = append(list, decision{id: itoa(i + 1), title: "entry " + itoa(i+1), status: "decided", by: "d.md:" + itoa(i+1)})
	}
	decisions, decisionRepoOrder = map[string][]decision{"r": list}, []string{"r"}
	m := newModel()
	m.decQuery = "entry"
	m = m.decSearchRun()
	m.decAt = 20
	out := ansi.Strip(m.decHitsBlock(60, 13))
	if n := strings.Count(out, "\n") + 1; n > 13 {
		t.Errorf("the hits should fit 13 lines, took %d:\n%s", n, out)
	}
	for _, want := range []string{"more matches above", "more matches below", "▸"} {
		if !strings.Contains(out, want) {
			t.Errorf("a windowed list lacks %q:\n%s", want, out)
		}
	}
	m.decAt = 0
	if out := ansi.Strip(m.decHitsBlock(60, 13)); strings.Contains(out, "above") {
		t.Errorf("at the top nothing is above:\n%s", out)
	}
}

// Leaving the lens leaves its search, as it leaves the other lenses' modes.
func TestLeavingTheLensLeavesTheSearch(t *testing.T) {
	m := searchFor(decSearchFixture(t), "ghost")
	m = press(m, "enter")
	m = press(m, "1")
	m = press(m, "5")
	if m.decSearchShown() {
		t.Error("coming back to decisions should find the repo's list, not the old search")
	}
}

// The search is drawn in the lens's own columns at every width: the query and
// its count over the hits, each repo's share on the left, the selected hit in
// full on the right, and keys in the footer that say what the keys now do.
func TestTheSearchIsDrawnAtEveryWidth(t *testing.T) {
	for _, w := range []int{80, 130, 190} {
		m := decSearchFixture(t)
		m.width = w
		m = press(m, "/")
		out := ansi.Strip(m.View())
		if !strings.Contains(out, "type to search every decision") {
			t.Errorf("w=%d: an empty query should say what it searches:\n%s", w, out)
		}
		if !strings.Contains(out, "esc cancels") {
			t.Errorf("w=%d: the footer should name the typing keys", w)
		}
		m = typeDec(m, "every")
		renderClean(t, m, "typing a search")
		out = ansi.Strip(m.View())
		for _, want := range []string{"/every▏", "3 matches in 2 repos"} {
			if !strings.Contains(out, want) {
				t.Errorf("w=%d: the search lacks %q:\n%s", w, want, out)
			}
		}
		m = press(m, "enter")
		out = ansi.Strip(m.View())
		if !strings.Contains(out, "esc go to it") {
			t.Errorf("w=%d: the footer should say where esc goes:\n%s", w, out)
		}
		m = typeDec(m, "/zzqx")
		m = press(m, "enter")
		renderClean(t, m, "a search with no match")
		if out := ansi.Strip(m.View()); !strings.Contains(out, "no match") {
			t.Errorf("w=%d: a query with no hits should say so:\n%s", w, out)
		}
	}

	// The left column counts each repo's hits, a dot for none, and marks the
	// selected hit's repo.
	m := searchFor(decSearchFixture(t), "monorepo")
	left := ansi.Strip(m.decLeftBlock(40, "claude-dispatcher"))
	if !strings.Contains(left, "▸ player-app") || !strings.Contains(left, "matches in each repo") {
		t.Errorf("the left column should mark the hit's repo:\n%s", left)
	}
	var dot bool
	for _, l := range strings.Split(left, "\n") {
		if strings.Contains(l, "claude-dispatcher") && strings.HasSuffix(strings.TrimSpace(l), "·") {
			dot = true
		}
	}
	if !dot {
		t.Errorf("a repo with no hits should count a dot:\n%s", left)
	}
	body := ansi.Strip(m.decBodyBlock(70, nil, 0))
	if !strings.Contains(body, "Monorepo") || !strings.Contains(body, "kill-session") {
		t.Errorf("the body should be the selected hit in full:\n%s", body)
	}
	m = searchFor(decSearchFixture(t), "zzqx")
	if body := ansi.Strip(m.decBodyBlock(70, nil, 0)); !strings.Contains(body, "no decision matches") {
		t.Errorf("with nothing matched the body should say so, got %q", body)
	}
	narrow := ansi.Strip(strings.Join(searchFor(decSearchFixture(t), "laptop").decNarrowBlock(60, nil, 0, true), "\n"))
	if !strings.Contains(narrow, "suspended laptop") || !strings.Contains(narrow, "·") {
		t.Errorf("one column should carry the hit's prose and each repo's count:\n%s", narrow)
	}
}

// / does nothing where there is nothing to search.
func TestSlashWithNoDecisionsDoesNothing(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	decisions, decisionRepoOrder = map[string][]decision{}, nil
	m := newModel()
	m.width, m.height = 190, 44
	m = press(press(m, "5"), "/")
	if m.decSearchShown() {
		t.Error("with no decisions anywhere / should not open a search")
	}
}

// The index does not trust a plain comparison where fzf would fold: a word
// with anything outside ASCII always reaches the matcher.
func TestAWordOutsideASCIIAlwaysReachesTheMatcher(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	decisions = map[string][]decision{"r": {{id: "1", title: "t", context: "un café crème"}}}
	decisionRepoOrder = []string{"r"}
	ix := buildDecIndex(decisionRepoOrder, decisions)
	if cands, _ := ix.runs.candidates("cafe"); len(cands) != 2 {
		t.Errorf("café and crème should reach the matcher, got %v", cands)
	}
	if f := ix.search("cafe"); f == nil || len(f.hits) != 1 {
		t.Errorf("fzf folds é, so cafe should find café, got %+v", f)
	}
	var none *decIndex
	if none.search("x") != nil || ix.search("  ") != nil {
		t.Error("no index, or no words, is no hits")
	}
}
