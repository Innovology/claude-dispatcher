//go:build !windows

package supervisor

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These exercise the Unix surface. They are best-effort like the code itself:
// tmux may be absent on the runner, so calls must degrade rather than fail.

func TestBackendName(t *testing.T) {
	if Backend() != "tmux" {
		t.Errorf("Backend() = %q, want tmux", Backend())
	}
	// Available() is whatever the host reports; just ensure it does not panic.
	_ = Available()
}

func TestUniqueNameFallsBackWhenFree(t *testing.T) {
	// A session name this specific will not exist, so UniqueName returns it as-is.
	name := "disp-supervisor-test-" + strings.Repeat("z", 8)
	if got := UniqueName(Session{Name: name}); got != name {
		t.Errorf("UniqueName(free) = %q, want %q", got, name)
	}
}

// A session that does not exist is not an idle one. Resume reads "unknown" as
// "leave it alone", so answering idle here would have it kill and replace
// sessions it cannot see.
func TestSessionIdleIsUnknownForAMissingSession(t *testing.T) {
	idle, known := SessionIdle(Session{Name: "definitely-not-a-real-session-xyz"})
	if known {
		t.Error("a session that does not exist cannot be reported on")
	}
	if idle {
		t.Error("unknown must never read as idle")
	}
}

func TestNonMutatingCallsAreSafe(t *testing.T) {
	// None of these should panic whether or not tmux is installed.
	bogus := Session{Name: "definitely-not-a-real-session-xyz"}
	onSocket := Session{Name: bogus.Name, Socket: "definitely-not-a-real-socket-xyz"}
	for _, s := range []Session{bogus, onSocket} {
		if HasSession(s) {
			t.Errorf("HasSession reported a bogus session as alive (socket %q)", s.Socket)
		}
		SetStatusHint(s)
		if cmd := AttachCmd(s, ""); cmd == nil {
			t.Error("AttachCmd returned nil")
		}
		// SendKeys/KillSession target a non-existent session; they may error,
		// which is fine — we only require they return without panicking.
		_ = SendKeys(s, "hello")
		_ = KillSession(s)
	}
	// A socket with no server behind it lists nothing; it must not be an error
	// anyone acts on, and it must not start a server to find that out.
	if got := Sessions("definitely-not-a-real-socket-xyz"); len(got) != 0 {
		t.Errorf("Sessions(dead socket) = %v, want none", got)
	}
	// EnsureBackKey and EnsureFocusEvents are left out on purpose: they bind a
	// key and set an option on the default server, which on a developer's
	// machine is their own tmux.
}

// AttachSwitches answers one question — does AttachCmd's exit mean the human is
// back, or that they have just left — and the cockpit's return-trip recheck
// hangs off it. It must agree with the command AttachCmd actually builds, or
// the cockpit waits for a return that already happened (or rechecks one that
// has not).
func TestAttachSwitchesMatchesTheCommandBuilt(t *testing.T) {
	for _, tc := range []struct {
		name, tmux, socket, wantArg string
		wantSwitches                bool
	}{
		{name: "inside tmux", tmux: "/tmp/tmux-501/default,1,0", wantArg: "switch-client", wantSwitches: true},
		{name: "bare terminal", tmux: "", wantArg: "attach-session", wantSwitches: false},
		// A repo names its own server, so the cockpit is routinely in a client
		// of a different one. switch-client cannot cross servers, so this has
		// to be a nested attach — which is also the honest answer for the
		// caller, because that attach runs in the cockpit's own pane and exits
		// when the human detaches.
		{name: "another repo's server", tmux: "/tmp/tmux-501/default,1,0", socket: "player-app", wantArg: "attach-session", wantSwitches: false},
		{name: "this session's own server", tmux: "/tmp/tmux-501/player-app,1,0", socket: "player-app", wantArg: "switch-client", wantSwitches: true},
		{name: "a named server from a bare terminal", tmux: "", socket: "player-app", wantArg: "attach-session", wantSwitches: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMUX", tc.tmux)
			sess := Session{Name: "some-session", Socket: tc.socket}
			if got := AttachSwitches(sess); got != tc.wantSwitches {
				t.Errorf("AttachSwitches() = %v, want %v", got, tc.wantSwitches)
			}
			args := AttachCmd(sess, "").Args
			if !slices.Contains(args, tc.wantArg) {
				t.Errorf("AttachCmd args = %v, want %q — it disagrees with AttachSwitches", args, tc.wantArg)
			}
			if tc.socket != "" && !slices.Contains(args, "-L") {
				t.Errorf("AttachCmd args = %v, want the session's own server named", args)
			}
		})
	}
}

// privateServer starts a tmux server of the test's own, on a socket under a
// TMUX_TMPDIR of its own, so nothing here can see or touch the developer's
// sessions — or be seen by Servers() as one of theirs. The socket directory is
// kept short: a unix socket path is capped near 104 bytes.
func privateServer(t *testing.T) string {
	t.Helper()
	if !Available() {
		t.Skip("tmux not installed")
	}
	dir, err := os.MkdirTemp("", "sv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
	sock := "disp-sup-" + strconv.Itoa(os.Getpid())
	conf := filepath.Join(dir, "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g exit-empty off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("tmux", "-L", sock, "-f", conf, "start-server").CombinedOutput(); err != nil {
		t.Skipf("tmux will not start a server here: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", sock, "kill-server").Run() })
	return sock
}

// A session's whole life through the backend, on a repo's own server: every
// call takes the full address, because a repo's sessions live on that repo's
// server (ADR 0014) and a call that dropped the socket would ask the default
// server about a session that is not there — and find nothing, every time.
func TestASessionsLifeOnItsOwnServer(t *testing.T) {
	sock := privateServer(t)
	dir := t.TempDir()
	s := Session{Name: "disp-roundtrip", Socket: sock}

	if err := NewSession(s, dir, "sleep 30", ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if !HasSession(s) {
		t.Fatal("the session just started is not there")
	}
	if HasSession(Session{Name: s.Name}) {
		t.Error("the session was found on the default server; it was started on its repo's")
	}
	if got := Sessions(sock); !slices.Contains(got, s.Name) {
		t.Errorf("Sessions(%q) = %v, want it to name %s", sock, got, s.Name)
	}
	// The listing carries the address back whole, and the directory, which is
	// what attributes a session nobody recorded.
	var found *SessionInfo
	for _, si := range SessionList(sock) {
		if si.Session == s {
			found = &si
		}
	}
	if found == nil {
		t.Fatalf("SessionList(%q) does not list %+v", sock, s)
	}
	resolved := func(p string) string { r, _ := filepath.EvalSymlinks(p); return r }
	if resolved(found.Path) != resolved(dir) {
		t.Errorf("session path = %q, want %q", found.Path, dir)
	}
	if !slices.Contains(Servers(), sock) {
		t.Errorf("Servers() = %v, want it to find %s", Servers(), sock)
	}

	// Something is running in it, so it is not idle — and that is known, not
	// guessed.
	if idle, known := SessionIdle(s); idle || !known {
		t.Errorf("SessionIdle on a running session = idle %v, known %v", idle, known)
	}
	if pids := SessionPIDs(s); len(pids) == 0 || pids[0] <= 0 {
		t.Errorf("SessionPIDs = %v, want the pane's process first", pids)
	}
	// A taken name gets a suffix, on the server that has it.
	if got := UniqueName(s); got != s.Name+"-2" {
		t.Errorf("UniqueName(taken) = %q, want %s-2", got, s.Name)
	}

	if err := KillSession(s); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	if HasSession(s) {
		t.Error("the session outlived KillSession")
	}
	// EnsureBackKey and EnsureFocusEvents stay out of this: both act on the
	// default server too, which on a developer's machine is their own tmux.
}

// A shell the backend is told about is a shell: a session parked at it reads
// idle, where before it read busy and Resume would never reclaim it. Told
// through the backend, because the cockpit only ever speaks to the supervisor.
func TestAShellTheBackendIsToldAboutReadsIdle(t *testing.T) {
	sock := privateServer(t)
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to copy")
	}
	real, err := os.ReadFile(sh)
	if err != nil {
		t.Skipf("cannot read %s: %v", sh, err)
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "cdsupsh"+strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(fake, real, 0o755); err != nil {
		t.Fatal(err)
	}
	// macOS kills a copied platform binary on sight (arm64e /bin/sh outside the
	// system paths), so the pane would die before there was anything to read.
	if err := exec.Command(fake, "-c", "true").Run(); err != nil {
		t.Skipf("a copy of %s will not run here: %v", sh, err)
	}
	s := Session{Name: "parked", Socket: sock}
	if err := NewSession(s, dir, "exec "+fake+" -i", ""); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	busy := func() bool { idle, known := SessionIdle(s); return known && !idle }
	if !eventually(busy) {
		t.Fatal("precondition: a shell nobody named should read busy")
	}
	KnowShell(fake)
	if !eventually(func() bool { idle, known := SessionIdle(s); return known && idle }) {
		t.Error("a shell the backend was told about still reads busy")
	}
}

func eventually(cond func() bool) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return cond()
}
