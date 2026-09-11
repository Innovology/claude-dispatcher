package cockpit

import (
	"os"
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

// The server a repo's sessions live on is typed against the repo, saved into
// [sockets], and published into the running config only when the save worked.
func TestClSetSocketWritesAndPublishes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.cfg = &config.Config{Products: map[string][]string{"acme": {"acme-api"}}}

	mm, cmd := m.clSetSocket("pp-calendar-sync", "pp_calendar_isolated")
	if cmd == nil {
		t.Fatal("a save must report")
	}
	if got := mm.cfg.Sockets["pp-calendar-sync"]; got != "pp_calendar_isolated" {
		t.Errorf("running config has %q, want the typed name", got)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if got := cfg.Sockets["pp-calendar-sync"]; got != "pp_calendar_isolated" {
		t.Errorf("config.toml has %q, want the typed name", got)
	}
}

// Typing the repo's own name is how you get back to the default, from the same
// field that left it. It clears the entry rather than storing a value equal to
// the default, which would be a line that does nothing until the default moves.
func TestClSetSocketToTheRepoNameClearsTheEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.cfg = &config.Config{Sockets: map[string]string{"playerpulse": "pp-old"}}

	mm, cmd := m.clSetSocket("playerpulse", "playerpulse")
	if _, ok := mm.cfg.Sockets["playerpulse"]; ok {
		t.Errorf("naming the default stored it anyway: %v", mm.cfg.Sockets)
	}
	if msg, ok := cmd().(actionMsg); !ok || !strings.Contains(msg.notice, "its own server again") {
		t.Errorf("notice = %+v, want it to say the default is back", msg)
	}
}

// An empty name is an answer — the default server, shared with everything that
// names none — and not the same act as clearing the entry.
func TestClSetSocketEmptyMeansTheDefaultServer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := clFixture(t)
	m.cfg = &config.Config{}

	mm, cmd := m.clSetSocket("legacy", "  ")
	got, ok := mm.cfg.Sockets["legacy"]
	if !ok || got != "" {
		t.Errorf("sockets = %v, want an entry that exists and is empty", mm.cfg.Sockets)
	}
	if msg, ok := cmd().(actionMsg); !ok || !strings.Contains(msg.notice, "default server") {
		t.Errorf("notice = %+v, want it to name the default server", msg)
	}
}

// A save that could not be written is never published, and always reported —
// the same rule clPersist and clSetLinearKey follow.
func TestClSetSocketDoesNotPublishAFailedSave(t *testing.T) {
	blocker := t.TempDir() + "/home"
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", blocker)

	m := clFixture(t)
	m.cfg = &config.Config{Sockets: map[string]string{"acme-api": "acme-old"}}

	mm, cmd := m.clSetSocket("acme-api", "acme-new")
	if cmd == nil {
		t.Fatal("a failed save must still report")
	}
	if msg, ok := cmd().(actionMsg); !ok || !strings.Contains(msg.notice, "could not save") {
		t.Fatalf("notice = %+v, want a failure the human can see", msg)
	}
	if mm.cfg.Sockets["acme-api"] != "acme-old" {
		t.Errorf("a failed save was published anyway: %v", mm.cfg.Sockets)
	}
}

// The fold names the server even when it is the default one: a gap where a name
// goes reads as a failure to find one, not as an answer.
func TestTheFoldNamesTheServerAndWhatLaunchesThere(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  clRepoRow
		want []string
	}{
		{"a repo on its own server", clRepoRow{socket: "playerpulse"}, []string{"server playerpulse", "s to change"}},
		{"and what it launches under", clRepoRow{socket: "pp", env: "nix develop --command"}, []string{"server pp", "nix develop"}},
		{"the default server is named", clRepoRow{}, []string{"default server"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clServerLine(tc.row)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("clServerLine = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}
