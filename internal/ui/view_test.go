package ui

import (
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "he…"},
		{"hello", 1, "…"},
		{"hello", 0, ""},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.w); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

func TestHumanAge(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "30s",
		90 * time.Second: "1m",
		2 * time.Hour:    "2h",
		72 * time.Hour:   "3d",
	}
	for d, want := range cases {
		if got := humanAge(d); got != want {
			t.Errorf("humanAge(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestPRLabel(t *testing.T) {
	if got := prLabel(&state.Dispatch{}); got != "—" {
		t.Errorf("no PR should render dash, got %q", got)
	}
	d := &state.Dispatch{PRNumber: 7, PRState: "MERGED"}
	if got := prLabel(d); got != "#7 merged" {
		t.Errorf("prLabel = %q", got)
	}
	now := time.Now().Add(-2 * time.Hour)
	d.DeployedAt = &now
	if got := prLabel(d); got != "#7 merged · live 2h ago" {
		t.Errorf("prLabel with deploy = %q", got)
	}
}

func TestStatusGlyphCoversAllStatuses(t *testing.T) {
	for _, s := range []state.Status{
		state.StatusLaunching, state.StatusWorking, state.StatusNeedsInput,
		state.StatusBlocked, state.StatusDone, state.StatusExited,
	} {
		if statusGlyph(s) == "?" {
			t.Errorf("no glyph for %s", s)
		}
		if statusLabel(s) == "" {
			t.Errorf("no label for %s", s)
		}
	}
}
