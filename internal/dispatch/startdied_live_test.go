//go:build !windows

package dispatch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// standInClaude puts a `claude` on PATH that runs body. The pane takes its PATH
// from the client that asked for it (ADR 0014), which is this test.
func standInClaude(t *testing.T, body string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// End to end, through Launch and a real tmux server: the launch line runs a
// claude that says what 2.1.332 said in a deleted directory and exits 1, the
// exit shell takes the pane, and the session lives on. Launch reports success
// for it, as it did on the day; the wait is what hears that claude died, in
// its own words, read off the real pane.
func TestALaunchWhoseClaudeDiesIsHeardFromItsPane(t *testing.T) {
	if !supervisor.Available() {
		t.Skip("no supervisor installed")
	}
	t.Setenv("HOME", t.TempDir()) // trust is written into the account's .claude.json
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	said := "The current directory no longer exists (it was deleted or moved). Start Claude Code from an existing directory."
	standInClaude(t, "echo '"+said+"'; exit 1")
	sock := "disp-test-died-" + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { _ = supervisor.KillSession(supervisor.Session{Name: "disp-died", Socket: sock}) })

	d, err := Launch(repos.Repo{Name: "acme", Path: initRepo(t), Socket: sock}, "died", "go", ModeAuto, DefaultModel, DefaultRoot, false, ownAccount)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	o, now := AwaitStart(d.ID, StartSettle+10*time.Second)
	if o != StartFailed {
		t.Fatalf("AwaitStart = %v (%s), want failed — claude exited at once", o, StartReport(o, now, 0))
	}
	if want := diedLead + " (status 1): " + said; now.StatusReason != want {
		t.Errorf("reason = %q\nwant %q", now.StatusReason, want)
	}
	kept, err := os.ReadFile(now.StartOutput)
	if err != nil || !strings.Contains(string(kept), ExitMark+"1") {
		t.Errorf("the pane was not kept whole (%v):\n%s", err, kept)
	}
	// Left at its shell for inspection, not killed.
	if !supervisor.HasSession(SessionOf(now)) {
		t.Error("the dead launch's session was taken away")
	}
}

// And the other side, on the same real server: a claude that is running and
// has not reported in — slow, or stopped on a startup prompt — is never called
// dead, however long the wait.
func TestALaunchStillRunningIsNeverCalledDead(t *testing.T) {
	if !supervisor.Available() {
		t.Skip("no supervisor installed")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	standInClaude(t, "echo 'Do you trust the files in this folder?'; sleep 60")
	sock := "disp-test-busy-" + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { _ = supervisor.KillSession(supervisor.Session{Name: "disp-busy", Socket: sock}) })

	d, err := Launch(repos.Repo{Name: "acme", Path: initRepo(t), Socket: sock}, "busy", "go", ModeAuto, DefaultModel, DefaultRoot, false, ownAccount)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if o, now := AwaitStart(d.ID, StartSettle+time.Second); o != StartUnconfirmed || now.Status != state.StatusLaunching {
		t.Errorf("AwaitStart = %v, status %q — a running claude was judged", o, now.Status)
	}
}
