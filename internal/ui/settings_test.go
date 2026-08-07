package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestSettingsAddRoot(t *testing.T) {
	dir := t.TempDir()
	s := newSettings(nil, false)
	if res, _ := s.update(key("a")); res != settingsActive || !s.adding {
		t.Fatal("a must open the add input")
	}
	s.update(key(dir))
	if res, _ := s.update(key("enter")); res != settingsActive {
		t.Fatal("adding a root must not close settings")
	}
	if s.adding || len(s.roots) != 1 || s.roots[0] != dir {
		t.Fatalf("root not added: adding=%v roots=%v", s.adding, s.roots)
	}
}

func TestSettingsRejectsMissingDir(t *testing.T) {
	s := newSettings(nil, false)
	s.update(key("a"))
	s.update(key("/does/not/exist-anywhere"))
	s.update(key("enter"))
	if len(s.roots) != 0 || s.errMsg == "" {
		t.Fatalf("nonexistent dir must be rejected: roots=%v err=%q", s.roots, s.errMsg)
	}
}

func TestSettingsRejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	s := newSettings([]string{dir}, false)
	s.update(key("a"))
	s.update(key(dir))
	s.update(key("enter"))
	if len(s.roots) != 1 || s.errMsg == "" {
		t.Fatalf("duplicate root must be rejected: roots=%v err=%q", s.roots, s.errMsg)
	}
}

func TestSettingsSaveRequiresRoot(t *testing.T) {
	s := newSettings([]string{"/a"}, false)
	s.update(key("x"))
	if len(s.roots) != 0 {
		t.Fatalf("x must remove the selected root, got %v", s.roots)
	}
	if res, _ := s.update(key("enter")); res != settingsActive || s.errMsg == "" {
		t.Fatal("saving with no roots must be refused")
	}
}

func TestSettingsSaveAndCancel(t *testing.T) {
	s := newSettings([]string{"/a", "/b"}, false)
	if res, _ := s.update(key("enter")); res != settingsSaved {
		t.Fatal("enter with roots must save")
	}
	if res, _ := s.update(key("esc")); res != settingsCancelled {
		t.Fatal("esc must cancel")
	}
}
