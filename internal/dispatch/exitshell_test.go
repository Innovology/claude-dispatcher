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
	line := launchCommand("abc123", "/prompts/abc123.txt", "/bin/nu", ModeAuto, DefaultModel, "")
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
	line := resumeCommand("abc123", "sess-1", "", "/usr/bin/fish", ModeAuto, DefaultModel, "")
	if !strings.Contains(line, `cdsh='/usr/bin/fish'`) {
		t.Errorf("resume dropped the shell:\n%s", line)
	}
}

// The tail says that claude has gone, and how, before the exit shell takes the
// pane: the status is read while $? is still claude's, so whatever the tail
// runs after it cannot change what the mark says.
func TestThePaneSaysClaudeExitedAndWithWhatStatus(t *testing.T) {
	sh, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true(1) to stand in for the exit shell")
	}
	out, err := exec.Command("/bin/sh", "-c", "echo claude said this; (exit 3)"+exitShell(sh)).CombinedOutput()
	if err != nil {
		t.Fatalf("the line did not run: %v: %s", err, out)
	}
	said, status, found := cutAtExit(string(out))
	if !found || status != "3" {
		t.Errorf("the mark reads status %q (found %v), want 3:\n%s", status, found, out)
	}
	if !strings.Contains(said, "claude said this") {
		t.Errorf("what claude printed is not before the mark:\n%s", out)
	}
}

// The incident's screen, as the pane showed it: claude's one line and its exit,
// then the exit shell complaining about the directory it was left in. What
// claude said is what comes before the mark; the shell's lines are not it.
func TestWhatClaudeSaidIsWhatCameBeforeItsExit(t *testing.T) {
	screen := "shell-init: error retrieving current directory: getcwd: cannot access parent directories: No such file or directory\n" +
		"The current directory no longer exists (it was deleted or moved). Start Claude Code from an existing directory.\n" +
		"\n" + ExitMark + "1\n" +
		"Error: nu::shell::directory_not_found\n" +
		"~/gone> \n\n"
	said, status, found := cutAtExit(screen)
	if !found || status != "1" {
		t.Fatalf("cutAtExit = status %q, found %v", status, found)
	}
	if strings.Contains(said, "nu::shell") || strings.Contains(said, ExitMark) {
		t.Errorf("the exit shell's lines were kept as claude's:\n%s", said)
	}
	if _, _, found := cutAtExit("no mark here\n"); found {
		t.Error("a pane with no mark was read as one whose claude exited")
	}
}
