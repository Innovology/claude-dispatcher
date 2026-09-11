package tmux

import (
	"os"
	"strconv"
	"testing"
)

func itoa(i int) string { return strconv.Itoa(i) }

// SessionIdle decides whether a dispatch session's claude process has ended and
// its name can be taken back, so what counts as "a shell" is the whole of it. A
// login shell reports with a leading dash, and anything that is not a shell is
// something running.
func TestShellCommandsCoverALoginShell(t *testing.T) {
	idle := paneIdle
	for _, sh := range []string{"zsh", "-zsh", "bash", "-bash", "sh", "fish"} {
		if !idle(sh) {
			t.Errorf("%q should read as an idle shell", sh)
		}
	}
	for _, busy := range []string{"claude", "node", "vim", "git", "python3"} {
		if idle(busy) {
			t.Errorf("%q should read as something running", busy)
		}
	}
}

// A session tmux cannot answer for is unknown, never idle: the caller reclaims
// an idle session and leaves an unknown one alone.
func TestSessionIdleIsUnknownForAMissingSession(t *testing.T) {
	idle, known := Default.SessionIdle("definitely-not-a-real-tmux-session-xyz")
	if known || idle {
		t.Errorf("SessionIdle(missing) = idle:%v known:%v", idle, known)
	}
}

// The bug this function shipped with, and the reason a dispatcher could be
// killed out from under itself: a launched session runs
// `<shell> -c "… claude …; exec $SHELL"`, so the pane's foreground process
// group leader is the shell whether or not claude is running in it, and tmux
// reports "zsh" either way. What separates the two is what is running under the
// shell.
func TestALiveClaudeUnderTheShellIsNotIdle(t *testing.T) {
	running := func() map[string]bool { return map[string]bool{"51122": true} }
	idle, known := panesIdle([]pane{{pid: "51122", cmd: "zsh"}}, running)
	if idle || !known {
		t.Errorf("a shell with a process under it = idle:%v known:%v, want busy", idle, known)
	}

	// And the session claude has left: `exec` replaced that same shell with the
	// login shell, which sits there with nothing under it.
	idle, known = panesIdle([]pane{{pid: "51122", cmd: "-zsh"}}, func() map[string]bool {
		return map[string]bool{"1": true}
	})
	if !idle || !known {
		t.Errorf("a childless login shell = idle:%v known:%v, want idle", idle, known)
	}
}

// Where tmux can name a real command — an interactive shell's job control does
// give claude its own process group — that answer still stands on its own, and
// costs no process-table read.
func TestANamedCommandNeedsNoProcessTable(t *testing.T) {
	idle, known := panesIdle([]pane{{pid: "1", cmd: "claude"}}, func() map[string]bool {
		t.Error("the process table was read for a pane tmux had already named")
		return nil
	})
	if idle || !known {
		t.Errorf("claude in the pane = idle:%v known:%v", idle, known)
	}
}

// A process table we cannot read is not evidence of an empty one: unknown,
// which leaves the session alone.
func TestAnUnreadableProcessTableIsUnknown(t *testing.T) {
	idle, known := panesIdle([]pane{{pid: "7", cmd: "zsh"}}, func() map[string]bool { return nil })
	if idle || known {
		t.Errorf("SessionIdle = idle:%v known:%v, want unknown", idle, known)
	}
}

// Any busy pane makes the session busy: the human may have split the window.
func TestOneBusyPaneMakesTheSessionBusy(t *testing.T) {
	free := func() map[string]bool { return map[string]bool{"1": true} }
	idle, known := panesIdle([]pane{{pid: "2", cmd: "-zsh"}, {pid: "3", cmd: "vim"}}, free)
	if idle || !known {
		t.Errorf("idle:%v known:%v, want busy", idle, known)
	}
}

func TestParsePanesReadsPidAndCommand(t *testing.T) {
	got := parsePanes("51122 zsh\n\n83316 claude\nrubbish\n")
	if len(got) != 2 || got[0] != (pane{"51122", "zsh"}) || got[1] != (pane{"83316", "claude"}) {
		t.Errorf("parsePanes = %+v", got)
	}
}

// The real read, against this test's own process: `ps` has to answer, and it
// has to name the parent of a process we can point at.
func TestProcessParentsSeesThisProcess(t *testing.T) {
	parents := processParents()
	if parents == nil {
		t.Skip("no readable process table here")
	}
	if !parents[itoa(os.Getppid())] {
		t.Error("the parent of this test process is not in the parent set")
	}
}
