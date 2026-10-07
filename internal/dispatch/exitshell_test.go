//go:build !windows

package dispatch

import (
	"os/exec"
	"strings"
	"testing"
)

// Whatever the tail resolves to, the line claude is launched with is POSIX and
// must stay that way: the shell setting names what the pane BECOMES, never what
// parses the launch. That distinction is the whole of the nu report.
func TestTheLaunchLineStaysPOSIXWhateverTheShellIs(t *testing.T) {
	line := launchCommand("abc123", "/prompts/abc123.txt", "/bin/nu", ModeAuto, DefaultModel)
	head, _, found := strings.Cut(line, "; cdsh=")
	if !found {
		t.Fatalf("no exit-shell tail in:\n%s", line)
	}
	if strings.Contains(head, "nu") {
		t.Errorf("the configured shell reached the launch itself:\n%s", head)
	}
	if !strings.HasPrefix(head, "CLAUDE_DISPATCHER_ID=abc123 claude ") {
		t.Errorf("the launch is not the plain claude line any more:\n%s", head)
	}
	// /bin/sh has to be able to parse the whole thing, tail included.
	if out, err := exec.Command("/bin/sh", "-n", "-c", line).CombinedOutput(); err != nil {
		t.Errorf("/bin/sh cannot parse the launch line: %v: %s\n%s", err, out, line)
	}
}

// The four answers, in order, and the reason each one is there.
func TestExitShellFallsBackFromConfigToTmuxToSHELL(t *testing.T) {
	configured := exitShell("/usr/bin/fish")
	if !strings.Contains(configured, `cdsh='/usr/bin/fish'`) {
		t.Errorf("the configured shell is not used first:\n%s", configured)
	}
	for _, want := range []string{
		`tmux display -p '#{default-shell}'`, // the human's own tmux setting
		`${SHELL:-/bin/sh}`,                  // then their login shell, then sh
		`exec "$cdsh"`,
	} {
		if !strings.Contains(configured, want) {
			t.Errorf("the tail is missing %q:\n%s", want, configured)
		}
	}
	// Unset is not "bash": it leaves the first answer empty so the pane asks
	// its own server, which is the half of this that needs no configuration.
	if unset := exitShell(""); !strings.Contains(unset, "cdsh=''") {
		t.Errorf("an unset shell should leave the slot empty:\n%s", unset)
	}
	// A shell that is named but not there must not kill the pane the moment
	// claude exits — `-x` is what sends it on to the next answer.
	if !strings.Contains(configured, `[ -x "$cdsh" ]`) {
		t.Errorf("a missing configured shell has no fallback:\n%s", configured)
	}
}

// The value is a path from a config file, so it is quoted like every other one.
func TestExitShellQuotesTheShell(t *testing.T) {
	line := exitShell("/opt/my shells/it's")
	if !strings.Contains(line, `cdsh='/opt/my shells/it'\''s'`) {
		t.Errorf("the shell was not quoted safely:\n%s", line)
	}
	if out, err := exec.Command("/bin/sh", "-n", "-c", "true"+line).CombinedOutput(); err != nil {
		t.Errorf("/bin/sh cannot parse it: %v: %s\n%s", err, out, line)
	}
}

// Resume reopens the way the dispatch went out, which includes the pane it
// leaves behind when the reopened session ends.
func TestResumeCarriesTheShellToo(t *testing.T) {
	line := resumeCommand("abc123", "sess-1", "", "/usr/bin/fish", ModeAuto, DefaultModel)
	if !strings.Contains(line, `cdsh='/usr/bin/fish'`) {
		t.Errorf("resume dropped the shell:\n%s", line)
	}
}
