package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

// The sockets we are looking for were made by somebody else's tmux, so the only
// way to find them is tmux's own rule for where they go: TMUX_TMPDIR, or /tmp.
func TestSocketDirFollowsTmuxsOwnRule(t *testing.T) {
	t.Setenv("TMUX_TMPDIR", "/run/user/1000")
	if got := SocketDir(); !strings.HasPrefix(got, "/run/user/1000/tmux-") {
		t.Errorf("SocketDir() = %q, want it under TMUX_TMPDIR", got)
	}
	t.Setenv("TMUX_TMPDIR", "")
	if got := SocketDir(); !strings.HasPrefix(got, "/tmp/tmux-") {
		t.Errorf("SocketDir() with no TMUX_TMPDIR = %q, want it under /tmp", got)
	}
}

// Every socket found is a candidate, never a server: the file outlives the
// process that made it. Listing one that is dead must answer nothing, and must
// not start a server to find that out — only new-session may do that.
func TestServersFindsSocketsAndADeadOneAnswersNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", dir)
	sockets := filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()))
	if err := os.MkdirAll(sockets, 0o700); err != nil {
		t.Fatal(err)
	}
	// A socket file with nothing behind it — a reboot's leftover.
	dead := filepath.Join(sockets, "dead-project")
	if err := os.WriteFile(dead, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sockets, "a-directory"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := Servers()
	if len(got) != 1 || got[0].Socket != "dead-project" {
		t.Fatalf("Servers() = %+v, want just the socket file", got)
	}
	if s := got[0].SessionList(); len(s) != 0 {
		t.Errorf("a dead socket listed %+v, want nothing", s)
	}
	// The probe must not have brought a server into being.
	if HasSession := (Server{Socket: "dead-project"}).HasSession("anything"); HasSession {
		t.Error("probing a dead socket started a server")
	}
	if fi, err := os.Stat(dead); err != nil || fi.Size() != 0 {
		t.Errorf("the dead socket file changed under the probe: %v", err)
	}
}

// The launch that reported this: dispatching into a flake repo from a cockpit
// started in ~ died with `path "/home/…" does not contain a 'flake.nix'`,
// because the `nix develop` wrapping the client looked for its flake where the
// cockpit stood. The prefix here is a script that records where it was run, on
// a private server, so the test is about the directory and not about nix.
func TestTheEnvironmentPrefixStandsInTheSession(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
	tmp := t.TempDir()
	worktree := filepath.Join(tmp, "worktree")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(tmp, "cwd")
	prefix := filepath.Join(tmp, "record-cwd")
	script := "#!/bin/sh\npwd > " + marker + "\nexec \"$@\"\n"
	if err := os.WriteFile(prefix, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := Server{Socket: "disp-test-cwd-" + itoa(os.Getpid())}
	t.Cleanup(func() { _ = srv.cmd("kill-server").Run() })
	if err := srv.NewSession("s", worktree, "sleep 30", prefix); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the prefix never ran: %v", err)
	}
	if want, _ := filepath.EvalSymlinks(worktree); strings.TrimSpace(string(got)) != want {
		t.Errorf("the environment prefix ran in %q, want the session's %q", strings.TrimSpace(string(got)), want)
	}

	if c := srv.AttachCmd("s", prefix); c.Dir != worktree {
		t.Errorf("attach ran its prefix in %q, want the session's %q", c.Dir, worktree)
	}
	// No prefix, nothing to stand anywhere for: the plain client is unchanged.
	if c := srv.AttachCmd("s", ""); c.Dir != "" {
		t.Errorf("a plain attach was given a directory: %q", c.Dir)
	}
	// A directory that has gone is left out, not a chdir failure.
	if c := srv.clientIn(filepath.Join(tmp, "gone"), prefix, "list-sessions"); c.Dir != "" {
		t.Errorf("a missing dir was kept: %q", c.Dir)
	}
}

func TestPathOfSessionMatchesTheWholeName(t *testing.T) {
	out := "disp-app-2\t/src/app-2\ndisp-app\t/src/app\n"
	if got := pathOfSession(out, "disp-app"); got != "/src/app" {
		t.Errorf("pathOfSession(disp-app) = %q", got)
	}
	if got := pathOfSession(out, "disp"); got != "" {
		t.Errorf("a prefix matched: %q", got)
	}
}

// The report: on a machine whose tmux.conf makes nu the default shell, every
// dispatch sat at "starting session" for good. Handed one argument, tmux runs a
// pane's command through default-shell, nu could not parse the POSIX launch
// line, and the pane died with the server behind it — after new-session had
// returned 0. The default shell here is the extreme case, one that runs
// nothing at all: a session must survive it, because the launch line names
// its own shell.
func TestALaunchDoesNotRideTheHumansDefaultShell(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
	tmp := t.TempDir()
	refuse := filepath.Join(tmp, "refuse-everything")
	if err := os.WriteFile(refuse, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(tmp, "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g exit-empty off\nset -g default-shell "+refuse+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := Server{Socket: "disp-test-shell-" + itoa(os.Getpid())}
	t.Cleanup(func() { _ = srv.cmd("kill-server").Run() })
	if out, err := exec.Command("tmux", "-L", srv.Socket, "-f", conf, "start-server").CombinedOutput(); err != nil {
		t.Fatalf("start-server: %v: %s", err, out)
	}

	if err := srv.NewSession("s", tmp, `X="posix only"; sleep 30; exec ${SHELL:-/bin/sh}`, ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if !srv.HasSession("s") {
		t.Fatal("the session died: its command went through the server's default-shell")
	}
}
