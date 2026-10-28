//go:build !windows

package supervisor

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// SendKeys against a real tmux: the text arrives typed as written — key names
// and a leading dash included — and a session that is not there is an error,
// not a silent success. The plain "=name" target this replaced answered every
// send with "can't find pane", which the reply that used it never reported.
func TestSendKeysTypesLiterally(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	// Dot-free, like every real session name (slugs are [a-z0-9-]): tmux reads
	// a "." in a target as the window.pane separator.
	name := "disp-sendkeys-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	// On a server of its own, which is also the address a real reply uses: a
	// developer's default server is not the test's to add sessions to.
	sock := name
	if err := exec.Command("tmux", "-L", sock, "-f", "/dev/null", "new-session", "-d", "-s", name, "-x", "80", "-y", "10", "cat").Run(); err != nil {
		t.Skipf("tmux will not start a session here: %v", err)
	}
	defer func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() }()

	text := "-n C-c Enter merge it"
	if err := SendKeys(Session{Name: name, Socket: sock}, text); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	var pane string
	for i := 0; i < 20; i++ {
		out, _ := exec.Command("tmux", "-L", sock, "capture-pane", "-p", "-t", "="+name+":").Output()
		pane = string(out)
		if strings.Count(pane, text) >= 2 { // the tty's echo, then cat's copy
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pane never showed the line typed and entered:\n%s", pane)
}

func TestSendKeysNoSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	if err := SendKeys(Session{Name: "disp-no-such-session-9981"}, "hello"); err == nil {
		t.Fatal("a send to no session must say so")
	}
}

// A session of several panes is typed into nowhere: the current pane is
// whatever the human has in front of them, which in an adopted session need
// not be claude's — and Enter in vim or a shell does something.
func TestSendKeysRefusesASessionOfSeveralPanes(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	name := "disp-sendkeys-panes-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	sock := name
	if err := exec.Command("tmux", "-L", sock, "-f", "/dev/null", "new-session", "-d", "-s", name, "-x", "80", "-y", "10", "cat").Run(); err != nil {
		t.Skipf("tmux will not start a session here: %v", err)
	}
	defer func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() }()
	if err := exec.Command("tmux", "-L", sock, "new-window", "-d", "-t", "="+name+":", "cat").Run(); err != nil {
		t.Fatalf("new-window: %v", err)
	}

	err := SendKeys(Session{Name: name, Socket: sock}, "merge it")
	if err == nil || !strings.Contains(err.Error(), "2 panes") {
		t.Fatalf("SendKeys into two panes: err = %v, want a refusal naming them", err)
	}
	out, _ := exec.Command("tmux", "-L", sock, "capture-pane", "-p", "-t", "="+name+":").Output()
	if strings.Contains(string(out), "merge it") {
		t.Errorf("text was typed anyway:\n%s", out)
	}
}
