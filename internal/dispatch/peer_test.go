package dispatch

import (
	"os"
	"path/filepath"
	"testing"
)

// A live session's name is read off Claude Code's registry by its session id;
// a file whose process is gone, or whose pid is someone else's now, is not.
func TestPeerNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	proc := t.TempDir()
	prev := procRoot
	procRoot = proc
	t.Cleanup(func() { procRoot = prev })
	stat := func(pid, start string) {
		_ = os.MkdirAll(filepath.Join(proc, pid), 0o755)
		// Field 22 is the start time; the name in parens may hold spaces.
		_ = os.WriteFile(filepath.Join(proc, pid, "stat"), []byte(pid+" (claude x) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 "+start+" 20"), 0o644)
	}
	reg := func(dir, pid, sid, name, start string) {
		_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o755)
		_ = os.WriteFile(filepath.Join(dir, "sessions", pid+".json"),
			[]byte(`{"pid":`+pid+`,"sessionId":"`+sid+`","name":"`+name+`","procStart":"`+start+`"}`), 0o644)
	}
	own := filepath.Join(os.Getenv("HOME"), ".claude")
	work := t.TempDir()
	stat("100", "555")
	reg(own, "100", "s-live", "goldens-1b", "555")
	reg(own, "200", "s-dead", "gone-2c", "777") // no process
	stat("300", "999")
	reg(own, "300", "s-reused", "old-3d", "111") // the pid is someone else's now
	stat("400", "444")
	reg(work, "400", "s-work", "on-work", "444")

	got := PeerNames(work)
	if got["s-live"] != "goldens-1b" || got["s-work"] != "on-work" {
		t.Fatalf("names = %v", got)
	}
	if _, ok := got["s-dead"]; ok {
		t.Error("a dead session's name was offered")
	}
	if _, ok := got["s-reused"]; ok {
		t.Error("a reused pid's name was offered")
	}
}
