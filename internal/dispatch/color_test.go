//go:build !windows

package dispatch

import (
	"testing"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

func coloredRec(id string, st state.Status) *state.Dispatch {
	return &state.Dispatch{
		ID: id, Feature: id, Slug: id, Status: st, SessionID: "s-" + id,
		TmuxSession: "disp-" + id, Color: "orange",
	}
}

// A session that has reported in is typed its colour once — mid-turn too,
// since /color is an immediate command and does not touch the turn.
func TestColorGoesInOnceTheSessionHasReported(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	now := time.Now()
	typed := withRetrySeams(t, true, false, true)
	saveAll(t, coloredRec("busy", state.StatusWorking))

	if got := DeliverColors(state.LoadAll(), now); got != 1 {
		t.Fatalf("delivered %d, want 1", got)
	}
	if len(*typed) != 1 || (*typed)[0] != "disp-busy: /color orange" {
		t.Errorf("typed %q", *typed)
	}
	if got := DeliverColors(state.LoadAll(), now); got != 0 {
		t.Errorf("sent again (%d)", got)
	}
	if recordOnDisk(t, "busy").ColorAt == nil {
		t.Error("the record does not say the colour was sent")
	}
}

// Nothing is typed before the REPL is up, onto a menu, into a pane claude has
// left or cannot be seen into, into a finished session, or into one the human
// adopted.
func TestColorWaitsForASafeMoment(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name              string
		rec               func() *state.Dispatch
		alive, idle, know bool
	}{
		{"no session yet", func() *state.Dispatch { d := coloredRec("a", state.StatusLaunching); d.SessionID = ""; return d }, true, false, true},
		{"on a menu", func() *state.Dispatch { return coloredRec("a", state.StatusBlocked) }, true, false, true},
		{"claude exited", func() *state.Dispatch { return coloredRec("a", state.StatusNeedsInput) }, true, true, true},
		{"cannot tell", func() *state.Dispatch { return coloredRec("a", state.StatusNeedsInput) }, true, false, false},
		{"session gone", func() *state.Dispatch { return coloredRec("a", state.StatusWorking) }, false, false, true},
		{"finished", func() *state.Dispatch { return coloredRec("a", state.StatusExited) }, true, false, true},
		{"adopted", func() *state.Dispatch { d := coloredRec("a", state.StatusWorking); d.AdoptedAt = &now; return d }, true, false, true},
		{"no colour", func() *state.Dispatch { d := coloredRec("a", state.StatusWorking); d.Color = ""; return d }, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
			typed := withRetrySeams(t, tc.alive, tc.idle, tc.know)
			saveAll(t, tc.rec())
			if got := DeliverColors(state.LoadAll(), now); got != 0 || len(*typed) != 0 {
				t.Errorf("typed %q", *typed)
			}
		})
	}
}

// The repo's colour goes on the record at launch.
func TestLaunchRecordsTheColor(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	stubLaunch(t)
	d, err := Launch(repos.Repo{Name: "acme", Path: repo, Color: "pink"}, "colour me", "go", ModeAuto, DefaultModel, DefaultRoot, false, ownAccount)
	if err != nil {
		t.Fatal(err)
	}
	if d.Color != "pink" || d.ColorAt != nil {
		t.Errorf("color %q at %v", d.Color, d.ColorAt)
	}
}
