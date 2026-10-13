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
