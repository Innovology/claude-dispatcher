package steward

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSup struct {
	running bool
	idle    bool // claude has exited in the session
	known   bool
	started []string // "name|dir|cmd"
	killed  []string
	trusted bool
}

func withFakes(t *testing.T, f *fakeSup, exe string) {
	t.Helper()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	ph, pn, pk, pt, pe, pi := hasSession, newSession, killSession, trustDir, executable, sessionIdle
	t.Cleanup(func() {
		hasSession, newSession, killSession, trustDir, executable, sessionIdle = ph, pn, pk, pt, pe, pi
	})
	sessionIdle = func(string) (bool, bool) { return f.idle, f.known }
	hasSession = func(string) bool { return f.running }
	newSession = func(name, dir, cmd string) error {
		f.started = append(f.started, name+"|"+dir+"|"+cmd)
		f.running = true
		return nil
	}
	killSession = func(name string) error { f.killed = append(f.killed, name); f.running = false; return nil }
	trustDir = func(string) bool { return f.trusted }
	executable = func() (string, error) { return exe, nil }
}

func TestStartWritesTheBriefAndStartsTheSession(t *testing.T) {
	f := &fakeSup{trusted: true}
	exe := filepath.Join(t.TempDir(), "claude-dispatcher")
	withFakes(t, f, exe)
	if err := Start(); err != nil {
		t.Fatal(err)
	}
	if len(f.started) != 1 || !strings.HasPrefix(f.started[0], Session+"|"+Dir()+"|") {
		t.Fatalf("started %q", f.started)
	}
	// tmux gives a session its server's environment, so a chosen store has to
	// ride the command or the steward reads the default one.
	if !strings.Contains(f.started[0], "CLAUDE_DISPATCHER_STATE="+"'"+os.Getenv("CLAUDE_DISPATCHER_STATE")+"'") {
		t.Errorf("the store is not carried into the session: %s", f.started[0])
	}
	if strings.Contains(f.started[0], "CLAUDE_DISPATCHER_ID") {
		t.Error("the steward is not a dispatcher; its hooks must not be attributed to one")
	}
	brief, err := os.ReadFile(filepath.Join(Dir(), "CLAUDE.md"))
	if err != nil || !strings.Contains(string(brief), exe+" status --next") {
		t.Fatalf("brief does not name this binary's verbs: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(Dir(), ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Permissions struct{ Allow, Deny []string } `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	allowed := strings.Join(st.Permissions.Allow, "\n")
	for _, verb := range []string{"status", "reply", "note", "park"} {
		if !strings.Contains(allowed, "Bash("+exe+" "+verb+":*)") {
			t.Errorf("%s is not allowed without asking — the steward would stop on a prompt nobody watches", verb)
		}
	}
	if strings.Contains(allowed, "gh pr merge") || strings.Contains(allowed, "git push") {
		t.Error("the steward must not be allowed to merge or push")
	}
	if strings.Join(st.Permissions.Deny, ",") != "Edit,Write,NotebookEdit" {
		t.Errorf("deny = %v", st.Permissions.Deny)
	}

	if err := Start(); !errors.Is(err, ErrRunning) || len(f.started) != 1 {
		t.Fatalf("a second start must leave the running steward alone: %v", err)
	}
	if err := Stop(); err != nil || len(f.killed) != 1 {
		t.Fatalf("stop: %v %v", err, f.killed)
	}
	if err := Stop(); err == nil {
		t.Error("stopping a steward that is not running must say so")
	}
}

func TestStartUntrustedStillStarts(t *testing.T) {
	f := &fakeSup{trusted: false}
	withFakes(t, f, filepath.Join(t.TempDir(), "claude-dispatcher"))
	if err := Start(); !errors.Is(err, ErrUntrusted) || len(f.started) != 1 {
		t.Fatalf("got %v, started %v", err, f.started)
	}
}

func TestStartRefusesAGoRunBinary(t *testing.T) {
	f := &fakeSup{trusted: true}
	withFakes(t, f, "/tmp/go-build123/b001/exe/claude-dispatcher")
	if err := Start(); err == nil || len(f.started) != 0 {
		t.Fatalf("started a steward pointed at a temporary build: %v", err)
	}
}

// No dispatcher can ever be given the steward's session name.
func TestSessionCannotCollideWithADispatcher(t *testing.T) {
	if strings.HasPrefix(Session, "disp-") {
		t.Fatalf("%q is in the dispatchers' namespace", Session)
	}
}

// The switch is the human's whole involvement: Start turns it on, Stop turns
// it off, and Ensure brings a switched-on steward back when its session was
// taken away — and leaves a switched-off one alone.
func TestEnsureKeepsASwitchedOnStewardRunning(t *testing.T) {
	f := &fakeSup{trusted: true, known: true}
	withFakes(t, f, filepath.Join(t.TempDir(), "claude-dispatcher"))

	if started, err := Ensure(); started || err != nil || len(f.started) != 0 {
		t.Fatalf("switched off, Ensure must do nothing: %v %v", started, err)
	}
	if err := Start(); err != nil || !Enabled() {
		t.Fatalf("Start must switch it on: %v", err)
	}
	if started, _ := Ensure(); started {
		t.Fatal("a running steward must be left alone")
	}

	f.running = false // a reboot took tmux with it
	if started, err := Ensure(); !started || err != nil || len(f.started) != 2 {
		t.Fatalf("a switched-on steward must come back: %v %v %d", started, err, len(f.started))
	}

	f.idle = true // claude exited; the shell it dropped to is still there
	if started, _ := Ensure(); !started || len(f.killed) != 1 || len(f.started) != 3 {
		t.Fatalf("an exited claude must be replaced: killed=%v started=%d", f.killed, len(f.started))
	}
	f.idle, f.known = true, false // cannot see into the pane
	if started, _ := Ensure(); started {
		t.Fatal("an unknown answer must never replace a steward that may be working")
	}

	if err := Stop(); err != nil || Enabled() {
		t.Fatalf("Stop must switch it off: %v", err)
	}
	if started, _ := Ensure(); started {
		t.Fatal("a switched-off steward must stay off")
	}
}

// Switching on a steward that is already up records the intent.
func TestStartOnARunningStewardSwitchesItOn(t *testing.T) {
	f := &fakeSup{trusted: true, running: true, known: true}
	withFakes(t, f, filepath.Join(t.TempDir(), "claude-dispatcher"))
	if err := Start(); !errors.Is(err, ErrRunning) || !Enabled() {
		t.Fatalf("got %v, enabled=%v", err, Enabled())
	}
}
