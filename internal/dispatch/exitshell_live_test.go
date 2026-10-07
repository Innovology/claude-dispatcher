//go:build !windows

package dispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/supervisor"
)

// End to end, through the real launcher, on the machine running the test: a
// pane whose claude has exited is sitting at the shell the server launches
// panes with. No config key is set here, so this is the half that needs no
// configuration — the tail asks tmux, inside the pane, at the moment it is
// needed.
//
// The expected value is read from the same server rather than written in,
// because that is the point: whatever this machine's tmux is set to is what
// the human should land in. On the box this was reported from it is nu.
func TestAPaneLandsInTheServersOwnShell(t *testing.T) {
	if !supervisor.Available() {
		t.Skip("no supervisor installed")
	}
	sock := "disp-test-exitsh-" + strconv.Itoa(os.Getpid())
	sess := supervisor.Session{Name: "s", Socket: sock}
	t.Cleanup(func() { _ = supervisor.KillSession(sess) })

	// The launch line with claude stood in for: `true` ends immediately, which
	// is exactly what a dispatcher looks like the moment its claude exits.
	line := "true" + exitShell("")
	if err := supervisor.NewSession(sess, t.TempDir(), line, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	time.Sleep(400 * time.Millisecond)

	want := filepath.Base(strings.TrimSpace(tmuxOut(t, sock, "show", "-gv", "default-shell")))
	got := strings.TrimPrefix(strings.TrimSpace(tmuxOut(t, sock, "display", "-p", "-t", "s", "#{pane_current_command}")), "-")
	if want == "" {
		t.Skip("this tmux reports no default-shell")
	}
	t.Logf("this machine's tmux launches panes with %q", want)
	if got != want {
		t.Errorf("the pane is sitting at %q, want the server's own %q", got, want)
	}
	// And the session is still there: a pane that drops to a shell is a
	// dispatcher you can still jump into, which is the reason for the tail.
	if !supervisor.HasSession(sess) {
		t.Error("the session ended instead of dropping to a shell")
	}
	// Idle, so the name can be reclaimed and Resume can reopen it — including
	// when that shell is one no list names.
	if idle, known := supervisor.SessionIdle(sess); !idle || !known {
		t.Errorf("the finished pane reads idle=%v known=%v, want idle", idle, known)
	}
}

func tmuxOut(t *testing.T, socket string, args ...string) string {
	t.Helper()
	out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).Output()
	if err != nil {
		t.Fatalf("tmux %v: %v", args, err)
	}
	return string(out)
}
