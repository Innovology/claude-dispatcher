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
	// once claude has exited.
	if out, err := srv.cmd("new-session", "-d", "-s", "s", "-c", tmp, "--", fake).CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v: %s", err, out)
	}
	time.Sleep(300 * time.Millisecond)

	idle, known := srv.SessionIdle("s")
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
	time.Sleep(300 * time.Millisecond)
	if idle, known := srv.SessionIdle("s"); idle || !known {
		t.Errorf("a pane running sleep read as idle=%v known=%v", idle, known)
	}
}
