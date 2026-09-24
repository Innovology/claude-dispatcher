//go:build !windows

package supervisor

import (
	"os/exec"
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
	name := "disp-sendkeys-test-" + time.Now().Format("150405.000")
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "80", "-y", "10", "cat").Run(); err != nil {
		t.Skipf("tmux will not start a session here: %v", err)
	}
	defer func() { _ = exec.Command("tmux", "kill-session", "-t", "="+name).Run() }()

	text := "-n C-c Enter merge it"
	if err := SendKeys(name, text); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	var pane string
	for i := 0; i < 20; i++ {
		out, _ := exec.Command("tmux", "capture-pane", "-p", "-t", "="+name+":").Output()
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
	if err := SendKeys("disp-no-such-session-9981", "hello"); err == nil {
		t.Fatal("a send to no session must say so")
	}
}
