package cockpit

import (
	"strings"
	"testing"
)

// `A` on a session of your own opens the naming input, and the input is on the
// screen. It shipped as a bar no view drew: the press looked like nothing, and
// the next letters typed went into a name nobody could see.
func TestAdoptInputIsDrawnWhereTheKeyOpensIt(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	prevOwn := ownSessions
	t.Cleanup(func() { ownSessions = prevOwn })

	products = []product{{name: "playerpulse-player"}}
	dir := t.TempDir() // not a checkout, so the name starts from the folder
	ownSessions = map[string][]ownSession{"playerpulse-player": {
		{name: "player-app__main", socket: "player-app", repo: "player-app", path: dir},
	}}

	m := newModel()
	m.width, m.height = 190, 44
	m.lens = "product"
	m.rightTab = "sessions"

	if v := m.View(); !strings.Contains(v, "A adopts it") {
		t.Error("the sessions row does not name the key that adopts it")
	}

	next, _ := m.handleKey("A")
	mm := next.(model)
	if !mm.adoptOpenInput {
		t.Fatal("A on a session did not open the naming input")
	}
	v := mm.View()
	for _, want := range []string{"adopt this session", "player-app__main in player-app", "enter adopts it"} {
		if !strings.Contains(v, want) {
			t.Errorf("the naming input is not on screen: missing %q", want)
		}
	}
	if !mm.inputPending() {
		t.Error("an upgrade could re-exec over a half-typed name")
	}

	next, _ = mm.handleKey("esc")
	if mm = next.(model); mm.adoptOpenInput || strings.Contains(mm.View(), "adopt this session") {
		t.Error("esc did not close the naming input")
	}
}

// space marks a session for A, and A then acts on the marks rather than the
// cursor — so a mark has to be on screen, and has to go once it is spent. It
// was neither: nothing drew it, and a session released and listed again kept
// its mark, so the next A adopted it instead of the row under the cursor.
func TestASessionMarkShowsAndIsSpentByTheAdoption(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	prevOwn := ownSessions
	t.Cleanup(func() { ownSessions = prevOwn })

	products = []product{{name: "pp"}}
	ownSessions = map[string][]ownSession{"pp": {
		{name: "one", socket: "s", repo: "r", path: t.TempDir()},
		{name: "two", socket: "s", repo: "r", path: t.TempDir()},
	}}
	m := newModel()
	m.width, m.height = 190, 44
	m.lens = "product"
	m.rightTab = "sessions"

	marks := func(m model) int { return strings.Count(m.View(), "◆") }
	if marks(m) != 0 {
		t.Fatal("a mark is drawn before anything was marked")
	}
	next, _ := m.handleKey(" ")
	m = next.(model)
	next, _ = m.handleKey("j")
	m = next.(model)
	next, _ = m.handleKey(" ")
	m = next.(model)
	if marks(m) != 2 {
		t.Fatalf("two marked sessions draw %d marks", marks(m))
	}

	next, cmd := m.handleKey("A")
	m = next.(model)
	if cmd == nil {
		t.Fatal("A with two marks should adopt both")
	}
	if len(m.sessionMarked) != 0 || marks(m) != 0 {
		t.Errorf("the marks outlived the adoption: %v", m.sessionMarked)
	}
}
