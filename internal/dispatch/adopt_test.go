//go:build !windows

package dispatch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// withPane stands in for the process table under a session's panes.
func withPane(t *testing.T, pids ...int) {
	t.Helper()
	// The registry is read from ~/.claude when a process names no config dir
	// of its own, and this machine's real one has real pids in it.
	t.Setenv("HOME", t.TempDir())
	prev := paneChildren
	paneChildren = func(supervisor.Session) []int { return pids }
	t.Cleanup(func() { paneChildren = prev })
}

// fakeProc writes a /proc-shaped directory where pid holds fd links to each
// path, so the binding can be tested without a claude to look at.
func fakeProc(t *testing.T, byPID map[int][]string) {
	t.Helper()
	root := t.TempDir()
	for pid, targets := range byPID {
		fdDir := filepath.Join(root, strconv.Itoa(pid), "fd")
		if err := os.MkdirAll(fdDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for i, target := range targets {
			if err := os.Symlink(target, filepath.Join(fdDir, strconv.Itoa(i))); err != nil {
				t.Fatal(err)
			}
		}
	}
	prev := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = prev })
}

// The binding has to come from the process, not the log: a human running two
// orchestrators in one checkout — the case adoption exists for — has two
// conversations reporting from that directory and no way to tell them apart
// by path.
func TestBindSessionReadsTheConversationOffTheProcess(t *testing.T) {
	const want = "77ac6360-68d4-45dc-9487-c76ee230894b"
	withPane(t, 4242)
	fakeProc(t, map[int][]string{4242: {
		"/dev/pts/3",
		"/home/u/src/app/.git/index.lock",
		"/home/u/.claude/projects/-home-u-src-app/" + want + ".jsonl",
	}})

	got, err := BindSession(supervisor.Session{Name: "orchestrator"}, "/home/u/src/app")
	if err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	if got != want {
		t.Errorf("bound %q, want %q", got, want)
	}
}

// writeRegistry puts Claude Code's own file for pid under dir/sessions.
func writeRegistry(t *testing.T, dir string, pid int, sessionID, procStart string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"pid":` + strconv.Itoa(pid) + `,"sessionId":"` + sessionID + `","procStart":"` + procStart +
		`","cwd":"/home/u/src/app","kind":"interactive","status":"idle"}`
	if err := os.WriteFile(filepath.Join(dir, "sessions", strconv.Itoa(pid)+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeProcFile puts one file into the fake /proc entry for pid.
func writeProcFile(t *testing.T, pid int, name, body string) {
	t.Helper()
	dir := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// statLine is a /proc/<pid>/stat whose start time (field 22) is started, with
// a command name holding a space and a ')' so the parse cannot count fields
// from the front.
func statLine(pid int, started string) string {
	f := []string{strconv.Itoa(pid), "(claude (x) y)", "S"}
	for i := 4; i <= 21; i++ {
		f = append(f, "0")
	}
	return strings.Join(append(f, started, "0", "0"), " ")
}

// An idle claude holds no transcript open, which is the state an orchestrator
// waiting on its human spends most of its life in. Claude Code's own registry
// names the conversation anyway, and it is asked first.
func TestBindSessionReadsClaudeCodesOwnRegistry(t *testing.T) {
	const want = "51423193-d9d5-49ac-96ea-335dd6d7b9ab"
	withPane(t, 133069, 133072) // the pane's shell, then claude
	fakeProc(t, map[int][]string{133072: {"/dev/pts/3"}})
	writeProcFile(t, 133072, "stat", statLine(133072, "121031"))
	writeRegistry(t, filepath.Join(os.Getenv("HOME"), ".claude"), 133072, want, "121031")
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())

	got, err := BindSession(supervisor.Session{Name: "player-app__main", Socket: "player-app"}, t.TempDir())
	if err != nil || got != want {
		t.Fatalf("BindSession = %q, %v — want the conversation the registry names", got, err)
	}
}

// A registry file is left behind by a claude that died without cleaning up,
// and its pid is handed to something else. The start time is what tells the
// two processes apart.
func TestBindSessionRefusesARegistryFileFromAnEarlierProcess(t *testing.T) {
	withPane(t, 9)
	fakeProc(t, map[int][]string{9: nil})
	writeProcFile(t, 9, "stat", statLine(9, "500"))
	writeRegistry(t, filepath.Join(os.Getenv("HOME"), ".claude"), 9, "stale-conversation", "400")
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())

	if got, err := BindSession(supervisor.Session{Name: "s"}, t.TempDir()); err == nil {
		t.Fatalf("bound %q from a registry file whose process is gone", got)
	}
}

// A claude run under an account keeps its registry in that account's config
// dir, which its own environment names.
func TestBindSessionReadsTheRegistryOfTheProcesssAccount(t *testing.T) {
	const want = "acc0un75-0000-4000-8000-000000000001"
	withPane(t, 31)
	fakeProc(t, map[int][]string{31: nil})
	account := t.TempDir()
	writeProcFile(t, 31, "environ", "PATH=/bin\x00CLAUDE_CONFIG_DIR="+account+"\x00")
	writeRegistry(t, account, 31, want, "")

	got, err := BindSession(supervisor.Session{Name: "s"}, t.TempDir())
	if err != nil || got != want {
		t.Fatalf("BindSession = %q, %v — want the account's registry read", got, err)
	}
}

// Any other open .jsonl is not a transcript. The projects directory is what
// makes one, and a session with a data file open must not be adopted under
// that file's name.
func TestBindSessionIgnoresOtherJSONL(t *testing.T) {
	withPane(t, 7)
	fakeProc(t, map[int][]string{7: {"/home/u/src/app/fixtures/events.jsonl"}})
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())

	if _, err := BindSession(supervisor.Session{Name: "s"}, t.TempDir()); err == nil {
		t.Error("a data file was accepted as a transcript")
	}
}

// With no process to read, the log answers — and refuses when it cannot
// choose, because a record bound to the wrong conversation silently takes
// another session's status.
func TestBindSessionFallsBackToTheLogAndRefusesAmbiguity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	withPane(t) // nothing readable under the pane
	fakeProc(t, nil)

	state.AppendEvent(state.Event{Event: "SessionStart", SessionID: "one", Cwd: dir})
	got, err := BindSession(supervisor.Session{Name: "s"}, dir)
	if err != nil || got != "one" {
		t.Fatalf("BindSession = %q, %v — want the one conversation that reported", got, err)
	}

	state.AppendEvent(state.Event{Event: "SessionStart", SessionID: "two", Cwd: dir})
	if _, err := BindSession(supervisor.Session{Name: "s"}, dir); err == nil {
		t.Error("two conversations in one directory should refuse, not pick")
	}
}

// Adoption writes a record around a running session and changes nothing about
// the session itself: no branch is cut, no worktree added, no claude started.
func TestAdoptWrapsTheSessionWithoutTouchingIt(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	prevAlive := sessionAlive
	sessionAlive = func(supervisor.Session) bool { return true }
	t.Cleanup(func() { sessionAlive = prevAlive })

	sess := supervisor.Session{Name: "playerpulse__main", Socket: "player-app"}
	d, err := Adopt(Adoption{
		Session:   sess,
		Dir:       repo,
		Repo:      repos.Repo{Name: "player-app", Path: repo, Product: "playerpulse-player"},
		Feature:   "robustness formula",
		SessionID: "sid-1",
	}, time.Now())
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if d.WorktreePath != repo {
		t.Errorf("worktree = %q, want the session's own directory %q", d.WorktreePath, repo)
	}
	if d.OwnsWorktree() {
		t.Error("an adopted dispatcher claims to own the human's checkout")
	}
	if !d.Adopted() {
		t.Error("the record does not say it was adopted")
	}
	if d.SessionID != "sid-1" || d.TmuxSocket != "player-app" {
		t.Errorf("the record does not address the session: %q on %q", d.SessionID, d.TmuxSocket)
	}
	if d.Prompt != "" || d.Mode != "" {
		t.Errorf("adoption invented a prompt (%q) or a mode (%q)", d.Prompt, d.Mode)
	}
	if d.BaseSHA == "" {
		t.Error("no base commit recorded — provenance would credit the whole branch")
	}
	// And it is on disk, which is what makes the hooks find it.
	if got := statusOnDisk(t, d.ID); got != state.StatusWorking {
		t.Errorf("status on disk = %q", got)
	}
}

// A name already live is refused: the record map and the worktree path are
// both keyed by it, and two live dispatchers of one name is the collision
// Launch has always refused.
func TestAdoptRefusesALiveName(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	prevAlive, prevIdle := sessionAlive, sessionIdle
	sessionAlive = func(supervisor.Session) bool { return true }
	sessionIdle = func(supervisor.Session) (bool, bool) { return false, true }
	t.Cleanup(func() { sessionAlive, sessionIdle = prevAlive, prevIdle })

	saveAll(t, &state.Dispatch{
		ID: "live", Feature: "robustness", Slug: "robustness",
		Status: state.StatusWorking, TmuxSession: "disp-robustness",
	})

	_, err := Adopt(Adoption{
		Session: supervisor.Session{Name: "other"}, Dir: repo,
		Repo: repos.Repo{Name: "player-app", Path: repo}, Feature: "robustness", SessionID: "sid",
	}, time.Now())
	if err == nil {
		t.Fatal("adopting under a live feature's name was allowed")
	}
}

// Releasing gives the session back: the record stops claiming it, stops
// answering its hooks, and keeps its place in history.
func TestReleaseStopsClaimingTheSession(t *testing.T) {
	now := time.Now()
	d := &state.Dispatch{ID: "a", Feature: "orchestrator", Status: state.StatusWorking,
		TmuxSession: "s", SessionID: "sid-1", AdoptedAt: &now, WorktreePath: "/home/u/src/app"}

	d.Release(now)

	if d.Adopted() {
		t.Error("a released record still claims the session")
	}
	if d.SessionID != "" {
		t.Error("a released record would still answer the session's hooks")
	}
	if !d.Finished() || d.FinishedAt == nil || d.DismissedAt == nil {
		t.Error("a release should finish the record and count as read")
	}
	if d.OwnsWorktree() {
		t.Error("a released record claims the human's checkout")
	}
}
