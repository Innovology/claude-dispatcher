package cockpit

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Ctrl-L must return a redraw command (tea.ClearScreen), so a garbled screen
// can be repainted.
func TestCtrlLRedraw(t *testing.T) {
	m := newModel()
	m.width, m.height = 120, 40
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd == nil {
		t.Fatal("ctrl+l should return a redraw cmd, got nil")
	}
}
