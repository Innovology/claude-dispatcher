package cockpit

// cluster_linear_test.go covers entering a product's Linear token in the
// assignment editor. `[linear]` is keyed by product NAME, and this screen is
// where product names are made — so the whole point of the key is that the name
// it saves against cannot be mistyped. These tests pin that join, the two
// states an empty entry has to tell apart, and the two ways a secret typed into
// a TUI leaks: onto the screen, and out of the buffer when a global key steals
// a keystroke.

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
)

// key drives the editor the way the cockpit does — through handleKey, so the
// global keys get their chance at the keystroke first.
func clKey(m model, k string) (model, tea.Cmd) {
	m.key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	next, cmd := m.handleKey(k)
	return next.(model), cmd
}

func clType(m model, s string) model {
	for _, r := range s {
		m, _ = clKey(m, string(r))
	}
	return m
}

// The join this exists for: the token is saved against the product the cursor
// is on, so it can never name a product that does not exist.
func TestClLinearKeySavesAgainstTheProductUnderTheCursor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.lens = "products"
	m.cfg = &config.Config{Products: map[string][]string{
		"acme":    {"acme-api"},
		"bluefin": {"orbit-billing"},
	}}

	// acme and bluefin, in name order; l opens the entry on the second.
	m, _ = clKey(m, "tab")
	m, _ = clKey(m, "j")
	m, _ = clKey(m, "l")
	if !m.clKeying {
		t.Fatal("l must open the token entry")
	}
	if m.clKeyFor != "bluefin" {
		t.Fatalf("entry is for %q, want the product under the cursor", m.clKeyFor)
	}

	m = clType(m, "lin_api_blu1")
	m, cmd := clKey(m, "enter")
	if cmd == nil {
		t.Fatal("enter must return a save command")
	}
	if m.clKeying {
		t.Error("enter must close the entry")
	}
	if got := m.cfg.Linear["bluefin"]; got != "lin_api_blu1" {
		t.Errorf("running config has %q for bluefin", got)
	}
	if _, ok := m.cfg.Linear["acme"]; ok {
		t.Error("only the product under the cursor is written")
	}

	// And it reached the file, in the shape linearReads will read back.
	out, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.Linear["bluefin"] != "lin_api_blu1" {
		t.Errorf("config on disk = %v", out.Linear)
	}
	t.Setenv("LINEAR_API_KEY", "")
	reads := linearReads(out)
	if len(reads) != 1 || reads[0].product != "bluefin" || reads[0].key != "lin_api_blu1" {
		t.Errorf("the token just typed does not scope a read: %+v", reads)
	}
}

// An empty entry removes the line rather than storing a blank: no entry means
// "read this product with the unscoped key", and a blank would say the same
// thing while making the product look configured on this screen.
func TestClLinearKeyEmptyClearsTheEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.lens = "products"
	m.cfg = &config.Config{
		Products: map[string][]string{"acme": {"acme-api"}},
		Linear:   map[string]string{"acme": "lin_api_old"},
	}

	m, _ = clKey(m, "tab")
	m, _ = clKey(m, "l")
	m, cmd := clKey(m, "enter")
	if cmd == nil {
		t.Fatal("enter must return a save command")
	}
	if _, ok := m.cfg.Linear["acme"]; ok {
		t.Errorf("an empty entry must remove the line, got %v", m.cfg.Linear)
	}
	out, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Linear["acme"]; ok {
		t.Errorf("config on disk still names a token: %v", out.Linear)
	}
}

// esc leaves the stored token alone. Nothing is pre-filled into the buffer, so
// an abandoned entry must not read as "cleared".
func TestClLinearKeyEscKeepsTheStoredToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.lens = "products"
	m.cfg = &config.Config{
		Products: map[string][]string{"acme": {"acme-api"}},
		Linear:   map[string]string{"acme": "lin_api_old"},
	}

	m, _ = clKey(m, "tab")
	m, _ = clKey(m, "l")
	m = clType(m, "half")
	m, _ = clKey(m, "esc")
	if m.clKeying || m.clKeyText != "" {
		t.Error("esc must close the entry and drop what was typed")
	}
	if m.cfg.Linear["acme"] != "lin_api_old" {
		t.Errorf("esc changed the stored token to %q", m.cfg.Linear["acme"])
	}
}

// The buffer owns the keyboard while it is open. A Linear token is mostly
// lowercase and digits: without the guard in handleKey, "lin_api_1…" switches
// lens on its first digit and `q` quits the cockpit, both silently discarding a
// pasted secret.
func TestClLinearKeyOwnsTheKeyboard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.lens = "products"
	m.cfg = &config.Config{Products: map[string][]string{"acme": {"acme-api"}}}

	m, _ = clKey(m, "tab")
	m, _ = clKey(m, "l")
	m = clType(m, "lin_api_1q3")
	if m.clKeyText != "lin_api_1q3" {
		t.Errorf("buffer = %q — a global key ate a keystroke", m.clKeyText)
	}
	if m.lens != "products" {
		t.Errorf("a digit in the token switched lens to %q", m.lens)
	}
	if !m.clKeying {
		t.Error("the entry closed itself mid-token")
	}
}

// The token never reaches the screen. This pane is what a shoulder reads, and a
// screen-shared cockpit must not be how a key leaks.
func TestClLinearKeyIsNeverRendered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.lens = "products"
	m.cfg = &config.Config{
		Products: map[string][]string{"acme": {"acme-api"}},
		Linear:   map[string]string{"acme": "lin_api_stored_secret"},
	}

	// Stored, with the entry closed: the pane says a token is set, not what it
	// is.
	view := strings.Join(m.clRight(40, 30), "\n")
	if strings.Contains(view, "lin_api_stored_secret") {
		t.Error("the stored token is printed in the products pane")
	}
	if !strings.Contains(view, "linear token") {
		t.Error("nothing on the pane says the product has a token of its own")
	}

	// And while it is being typed.
	m, _ = clKey(m, "tab")
	m, _ = clKey(m, "l")
	m = clType(m, "lin_api_typed_secret")
	view = strings.Join(m.clRight(40, 30), "\n")
	if strings.Contains(view, "lin_api_typed_secret") {
		t.Error("the token is echoed as it is typed")
	}
	if !strings.Contains(view, "••") {
		t.Error("the entry shows nothing at all — the human cannot tell it is receiving")
	}
}

// A real Linear key is about fifty characters and this pane is a third of the
// terminal, so a mask of one dot per rune is a line wider than the column it is
// drawn in — which pushes the pane past its own rule and wraps the editor.
func TestClLinearKeyMaskStaysInsideThePane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.lens = "products"
	m.cfg = &config.Config{Products: map[string][]string{"acme": {"acme-api"}}}

	m, _ = clKey(m, "tab")
	m, _ = clKey(m, "l")
	m = clType(m, strings.Repeat("k", 60))

	const cw = 30
	for _, ln := range m.clRight(cw, 30) {
		if w := dispWidth(ln); w > cw {
			t.Errorf("line %d columns wide in a %d-column pane: %q", w, cw, ln)
		}
	}

	// The naming prompt is the same prose in the same column, and was drawn
	// past the rule the same way.
	m, _ = clKey(m, "esc")
	m, _ = clKey(m, "n")
	m = clType(m, "a-product-with-a-long-name")
	for _, ln := range m.clRight(cw, 30) {
		if w := dispWidth(ln); w > cw {
			t.Errorf("naming: line %d columns wide in a %d-column pane: %q", w, cw, ln)
		}
	}
}

// A save that fails must leave the cockpit reading with what is still on disk,
// the same rule clPersist follows: publishing regardless would put the running
// config and the file permanently at odds under a notice saying nothing saved.
func TestClLinearKeyDoesNotPublishAFailedSave(t *testing.T) {
	blocker := t.TempDir() + "/home"
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", blocker)

	m := clFixture(t)
	m.cfg = &config.Config{
		Products: map[string][]string{"acme": {"acme-api"}},
		Linear:   map[string]string{"acme": "lin_api_old"},
	}
	mm, cmd := m.clSetLinearKey("acme", "lin_api_new")
	if cmd == nil {
		t.Fatal("a failed save must still report")
	}
	msg, ok := cmd().(actionMsg)
	if !ok || !strings.Contains(msg.notice, "could not save") {
		t.Fatalf("notice = %+v, want a failure the human can see", msg)
	}
	if mm.cfg.Linear["acme"] != "lin_api_old" {
		t.Errorf("a failed save was published anyway: %v", mm.cfg.Linear)
	}
}
