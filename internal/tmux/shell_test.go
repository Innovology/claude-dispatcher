package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A shell the list does not name reads as busy, and a busy pane is never
// reclaimed: Resume will not reopen the session and a second dispatch of that
// feature is refused. So a shell we were told about has to count, whatever it
// is called.
func TestKnowShellMakesAnUnknownShellIdle(t *testing.T) {
	const name = "cdtest-told-shell"
	if paneIdle(name) {
		t.Fatalf("precondition: %q is already known", name)
	}
	KnowShell("/opt/weird/" + name)
	if !paneIdle(name) {
		t.Errorf("%q should read as a shell once we have been told about it", name)
	}
	// A login shell announces itself with a dash, and the name may arrive as a
	// full path from config either way.
	if !paneIdle("-" + name) {
		t.Errorf("the login form of %q should read as a shell too", name)
	}
	// Being told about a shell teaches one name, not every name.
	if paneIdle("cdtest-never-mentioned") {
		t.Error("an unrelated command read as a shell")
	}
	// Nothing is not a shell.
	KnowShell("   ")
	if paneIdle("") {
		t.Error("an empty pane command read as a shell")
	}
}

// The other half: nobody configured anything, and the human's tmux launches
// panes with a shell under a name we have never heard of. The server knows,
// and it is the only thing that does — their tmux.conf may compute the value
// in a run-shell job, so there is nothing in the file to read.
func TestAnUnknownPaneAsksTheServerWhatItsShellIs(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to copy")
	}
	real, err := os.ReadFile(sh)
	if err != nil {
		t.Skipf("cannot read %s: %v", sh, err)
	}
	// A real shell under a name nothing could have in a list: the wrapper
	// script case, which is why the list cannot be the whole answer.
	tmp := t.TempDir()
	fake := filepath.Join(tmp, "cdtestsh"+itoa(os.Getpid()))
	if err := os.WriteFile(fake, real, 0o755); err != nil {
		t.Fatal(err)
	}
	// macOS kills a copied platform binary on sight (arm64e /bin/sh outside the
	// system paths), so the pane would die before there was anything to read.
	if err := exec.Command(fake, "-c", "true").Run(); err != nil {
		t.Skipf("a copy of %s will not run here: %v", sh, err)
	}
	conf := filepath.Join(tmp, "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g exit-empty off\nset -g default-shell "+fake+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := Server{Socket: "disp-test-defsh-" + itoa(os.Getpid())}
	t.Cleanup(func() { _ = srv.cmd("kill-server").Run() })
	if out, err := exec.Command("tmux", "-L", srv.Socket, "-f", conf, "start-server").CombinedOutput(); err != nil {
		t.Fatalf("start-server: %v: %s", err, out)
	}
	// The pane sits at that shell, which is what a dispatch pane looks like
	// once claude has exited. Two arguments, so tmux execs it directly: one
	// would be run as `<default-shell> -c fake`, and a sh that does not exec
	// its last command (Ubuntu's dash, on CI) leaves that wrapper as the pane
	// with the shell under it — a child, so busy, for as long as you wait.
	if out, err := srv.cmd("new-session", "-d", "-s", "s", "-c", tmp, "--", fake, "-i").CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v: %s", err, out)
	}
	var idle, known bool
	eventually(t, func() bool { idle, known = srv.SessionIdle("s"); return idle && known })
	if !known {
		t.Fatal("the session could not be read at all")
	}
	if !idle {
		cmd, _ := srv.cmd("display", "-p", "-t", "s", "#{pane_current_command}").Output()
		t.Fatalf("a pane parked at the server's own default-shell read as busy (pane command %q)", cmd)
	}
	// Asking is worth it only because it is rare: the answer is cached per
	// server, so a busy session does not spawn a tmux call on every sweep.
	if _, asked := askedDefaultShell.Load(srv.Socket); !asked {
		t.Error("the server's default-shell was not remembered")
	}
}

// Something actually running is still something running: learning shells must
// not turn the idle check into a yes-machine.
func TestARunningCommandIsStillBusy(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
	srv := Server{Socket: "disp-test-busy-" + itoa(os.Getpid())}
	t.Cleanup(func() { _ = srv.cmd("kill-server").Run() })
	if err := srv.NewSession("s", t.TempDir(), "sleep 30", ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	// Until the shell has started sleep, the pane is a shell with nothing under
	// it, which is idle. Busy has to arrive, and a pane that never reads busy is
	// the yes-machine.
	var idle, known bool
	if !eventually(t, func() bool { idle, known = srv.SessionIdle("s"); return !idle && known }) {
		t.Errorf("a pane running sleep read as idle=%v known=%v", idle, known)
	}
}

// eventually polls cond until it holds or a few seconds pass, and says which.
// A live tmux test waits for the state it is about to assert rather than for
// a fixed time, which a loaded CI runner can outlast.
func eventually(t *testing.T, cond func() bool) bool {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return cond()
}
