package cockpit

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

// x on an adopted session releases it: the session was the human's before the
// record and is theirs after it, so it keeps running, and the record lets go
// of it and goes to history. It used to take the kill path, which ended an
// orchestrator this cockpit never started.
func TestXReleasesAnAdoptedSessionAndLeavesItRunning(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	saved := captureVars()
	defer restoreVars(saved)

	sock := fmt.Sprintf("cd-adopt-release-%d", os.Getpid())
	if out, err := exec.Command("tmux", "-L", sock, "-f", "/dev/null", "new-session", "-d", "-s", "theirs", "sleep 60").CombinedOutput(); err != nil {
		t.Fatalf("starting a session: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })

	now := time.Now()
	rec := &state.Dispatch{
		ID: "adopted1", Feature: "their orchestrator", TmuxSession: "theirs", TmuxSocket: sock,
		SessionID: "sess-theirs", Status: state.StatusWorking, AdoptedAt: &now, WorktreePath: t.TempDir(),
	}
	if err := state.Save(rec); err != nil {
		t.Fatal(err)
	}
	liveRecords = map[string]*state.Dispatch{rec.Feature: rec}

	if act := cqKillAct(rec); act.d != "release" {
		t.Errorf("x on an adopted row offers %q, want release", act.d)
	}
	am, _ := killCmd([]string{rec.Feature})().(actionMsg)
	if !strings.Contains(am.notice, "released 1 adopted session") || strings.Contains(am.notice, "killed") {
		t.Errorf("notice = %q", am.notice)
	}
	if err := exec.Command("tmux", "-L", sock, "has-session", "-t", "=theirs").Run(); err != nil {
		t.Fatal("the human's session was killed")
	}
	if rec.ReleasedAt == nil || rec.SessionID != "" || rec.Status != state.StatusExited || rec.DismissedAt == nil {
		t.Errorf("record not released: released=%v session=%q status=%q dismissed=%v",
			rec.ReleasedAt, rec.SessionID, rec.Status, rec.DismissedAt)
	}
	if _, err := os.Stat(rec.WorktreePath); err != nil {
		t.Errorf("the human's directory went: %v", err)
	}
}
