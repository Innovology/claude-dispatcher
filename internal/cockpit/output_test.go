package cockpit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/state"
)

// A row whose session would not start offers what it printed, when there is
// more of it than the lead's one line — and not when there is not, because a
// key that opens what the row already says is a key that does nothing.
func TestARowThatDidNotStartOffersWhatItPrinted(t *testing.T) {
	rec := &state.Dispatch{Feature: "byga-integration-sitrep", Status: state.StatusExited,
		StatusReason: "did not start: tmux new-session: error: Cannot build 'chrome.deb.drv' — cannot download chrome.deb from any mirror",
		StartOutput:  "/state/output/abc.txt"}
	for _, kind := range []string{"done", "past"} {
		if !hasAct(cqActs(rec, kind), "l", "full error") {
			t.Errorf("a %s row with kept output does not offer it: %+v", kind, cqActs(rec, kind))
		}
	}
	rec.StartOutput = ""
	if hasAct(cqActs(rec, "done"), "l", "") {
		t.Error("a row with nothing kept offers to open it")
	}
}

func hasAct(acts []cqAct, k, d string) bool {
	for _, a := range acts {
		if a.k == k && (d == "" || a.d == d) {
			return true
		}
	}
	return false
}

// The editor opens at the error, not at the top of the downloads.
func TestTheOutputOpensAtItsError(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "nvim"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nvim")

	out := filepath.Join(t.TempDir(), "abc.txt")
	text := "tmux new-session: error: Cannot build 'x.drv' — boom\n\n" +
		"copying path '/nix/store/a' from 'https://cache.nixos.org'...\n" +
		"building '/nix/store/x.drv'...\n" +
		"error: Cannot build '/nix/store/x.drv'.\n" +
		"       > error: boom\n"
	if err := os.WriteFile(out, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd, err := startOutputCommand(out)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(cmd.Args[1:], " "), "+5 "+out; got != want {
		t.Errorf("editor args = %q, want %q", got, want)
	}

	if _, err := startOutputCommand(filepath.Join(t.TempDir(), "gone.txt")); err == nil {
		t.Error("a missing output opened anyway")
	}
	t.Setenv("EDITOR", "")
	if _, err := startOutputCommand(out); err != errNoEditor {
		t.Errorf("with no editor on PATH: %v, want errNoEditor", err)
	}
}

// l on the row reaches the editor through the keymap; a record with nothing
// kept says so instead of opening nothing.
func TestLOnARowWithNothingKeptSaysSo(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	rec := &state.Dispatch{ID: "r1", Feature: "doomed", Status: state.StatusExited}
	liveByID = map[string]*state.Dispatch{rec.ID: rec}

	m := newModel()
	mm, cmd := m.openStartOutput(fleetRow{id: rec.ID, feature: rec.Feature})
	if cmd != nil || !strings.Contains(mm.notice, "nothing was kept") {
		t.Errorf("notice = %q, cmd = %v", mm.notice, cmd != nil)
	}

	rec.StartOutput = filepath.Join(t.TempDir(), "gone.txt")
	mm, cmd = m.openStartOutput(fleetRow{id: rec.ID, feature: rec.Feature})
	if cmd != nil || !strings.Contains(mm.notice, "is gone") {
		t.Errorf("notice = %q, cmd = %v", mm.notice, cmd != nil)
	}
}

// l on the table reaches the row's act: the confirmation flashes on the row
// and the editor is what runs.
func TestLOnTheTableOpensTheRowsOutput(t *testing.T) {
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "nvim"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nvim")

	m := cqModel(t)
	out := filepath.Join(t.TempDir(), "abc.txt")
	if err := os.WriteFile(out, []byte("tmux new-session: error: x\n\nerror: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := &state.Dispatch{ID: "nix1", Feature: "nix shell broken", Status: state.StatusExited,
		StatusReason: "did not start: tmux new-session: error: x", StartOutput: out}
	liveByID = map[string]*state.Dispatch{rec.ID: rec}
	fleet = append(fleet, fleetRow{id: rec.ID, kind: "done", feature: rec.Feature, acts: cqActs(rec, "done")})
	m = m.fleetSync()
	m = press(m, "G")
	if r, _ := m.fleetSel(); r.id != rec.ID {
		t.Fatalf("cursor on %q, want the failed row", r.feature)
	}

	next, cmd := m.handleKey("l")
	m = next.(model)
	if cmd == nil || m.notice != "" {
		t.Fatalf("l did not open the output: notice %q", m.notice)
	}
	if m.cqFlashID != rec.ID || !strings.Contains(m.cqFlash, "printed") {
		t.Errorf("flash = %q on %q, want the act's confirmation on the failed row", m.cqFlash, m.cqFlashID)
	}
}
