//go:build !windows

package dispatch

import (
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// A dispatch is started on its repo's own server, under its repo's own
// environment, and the record says so.
//
// Both are on the record rather than resolved again later because the pane
// inherits the environment of whatever asked for it: the answer is settled at
// the moment of asking, and nothing that reads the record afterwards — resume,
// attach, the sweep — gets to reach a different one.
func TestLaunchRecordsTheServerAndTheEnvironmentItUsed(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)

	var started supervisor.Session
	var startedEnv string
	prevNew, prevUniq, prevAlive := newSession, uniqueName, sessionAlive
	newSession = func(s supervisor.Session, _, _, env string) error {
		started, startedEnv = s, env
		return nil
	}
	uniqueName = func(s supervisor.Session) string { return s.Name }
	sessionAlive = func(supervisor.Session) bool { return false }
	t.Cleanup(func() { newSession, uniqueName, sessionAlive = prevNew, prevUniq, prevAlive })

	r := repos.Repo{Name: "playerpulse", Path: repo, Socket: "player-app", Env: repos.NixDevelop}
	d, err := Launch(r, "login fix", "fix it", ModeAuto, DefaultModel, DefaultRoot, false)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if d.TmuxSocket != "player-app" {
		t.Errorf("record's socket = %q, want the repo's own server", d.TmuxSocket)
	}
	if d.EnvCommand != repos.NixDevelop {
		t.Errorf("record's env = %q, want %q", d.EnvCommand, repos.NixDevelop)
	}
	if started.Socket != "player-app" {
		t.Errorf("the session was started on %q, not the repo's server", started.Socket)
	}
	if startedEnv != repos.NixDevelop {
		t.Errorf("the session was started under %q, want %q", startedEnv, repos.NixDevelop)
	}
	if got := SessionOf(d); got != started {
		t.Errorf("SessionOf(record) = %+v, but the session started was %+v", got, started)
	}
}

// A repo that names nothing keeps the default server and no prefix — which is
// every dispatch made before any of this existed, and every repo on a machine
// this does not apply to.
func TestLaunchWithoutARepoEnvironmentIsUnchanged(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)

	var started supervisor.Session
	var startedEnv string
	prevNew, prevUniq, prevAlive := newSession, uniqueName, sessionAlive
	newSession = func(s supervisor.Session, _, _, env string) error {
		started, startedEnv = s, env
		return nil
	}
	uniqueName = func(s supervisor.Session) string { return s.Name }
	sessionAlive = func(supervisor.Session) bool { return false }
	t.Cleanup(func() { newSession, uniqueName, sessionAlive = prevNew, prevUniq, prevAlive })

	d, err := Launch(repos.Repo{Name: "acme", Path: repo}, "plain", "do it", ModeAuto, DefaultModel, DefaultRoot, false)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if d.TmuxSocket != "" || d.EnvCommand != "" {
		t.Errorf("a repo naming nothing recorded socket %q env %q", d.TmuxSocket, d.EnvCommand)
	}
	if started.Socket != "" || startedEnv != "" {
		t.Errorf("a repo naming nothing started on socket %q under %q", started.Socket, startedEnv)
	}
}

// Resume reopens where the dispatcher ran, from the record — not from whatever
// the repo's config says today. A session put back on a different server is a
// different session, and the transcript it was reopened for is on the old one.
func TestResumeReopensOnTheRecordedServer(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	d := finished(t, repo)
	d.TmuxSocket, d.EnvCommand = "player-app", repos.NixDevelop
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}

	s := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, s)

	if _, _, err := Resume(d, ""); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if s.started.socket != "player-app" {
		t.Errorf("resumed on socket %q, want the one it ran on", s.started.socket)
	}
	if s.started.env != repos.NixDevelop {
		t.Errorf("resumed under %q, want %q", s.started.env, repos.NixDevelop)
	}
	if d.TmuxSocket != "player-app" {
		t.Errorf("the record's socket changed on resume: %q", d.TmuxSocket)
	}
}

// Two repos can hold a session of the same name, and asking the wrong server
// about one gets a confident "no such session". liveDispatch must therefore
// refuse a name on the strength of the server it actually ran on.
func TestLivenessIsAskedOfTheRecordedServer(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	rec := &state.Dispatch{
		ID: state.NewID(), Feature: "login", Slug: "login",
		TmuxSession: "disp-login", TmuxSocket: "player-app",
		Status: state.StatusWorking,
	}
	if err := state.Save(rec); err != nil {
		t.Fatal(err)
	}

	var asked []supervisor.Session
	prevAlive, prevIdle := sessionAlive, sessionIdle
	sessionAlive = func(s supervisor.Session) bool {
		asked = append(asked, s)
		return s.Socket == "player-app"
	}
	sessionIdle = func(supervisor.Session) (bool, bool) { return false, true }
	t.Cleanup(func() { sessionAlive, sessionIdle = prevAlive, prevIdle })

	if got := liveDispatch("login"); got == nil {
		t.Fatal("a live dispatcher on a named server was not found")
	}
	for _, s := range asked {
		if s.Socket != "player-app" {
			t.Errorf("asked server %q about a session that runs on player-app", s.Socket)
		}
	}
}

// The sweep asks each SERVER once, not each record once.
//
// The rule it has to keep is the one a ghost depends on: a listing that failed
// comes back empty, and an empty listing must never retire a fleet — so a
// record the listing does not name is asked about directly before anything is
// written. With a server per repo there are more listings, and each one can be
// empty for a reason that has nothing to do with the records under it.
func TestTheSweepAsksEachServerOnceAndStillProbes(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	live := &state.Dispatch{
		ID: state.NewID(), Feature: "live", Slug: "live", TmuxSession: "disp-live",
		TmuxSocket: "player-app", Status: state.StatusWorking,
	}
	// Its server answers nothing at all — the shape of a listing that failed.
	ghost := &state.Dispatch{
		ID: state.NewID(), Feature: "ghost", Slug: "ghost", TmuxSession: "disp-ghost",
		TmuxSocket: "playerpulse", Status: state.StatusWorking,
	}
	saveAll(t, live, ghost)

	listed := map[string]int{}
	var probed []supervisor.Session
	prevNames, prevAlive, prevReady := sessionNames, sessionAlive, supervisorReady
	sessionNames = func(socket string) []string {
		listed[socket]++
		if socket == "player-app" {
			return []string{"disp-live"}
		}
		return nil
	}
	sessionAlive = func(s supervisor.Session) bool {
		probed = append(probed, s)
		return true // the listing was wrong; the session is there
	}
	supervisorReady = func() bool { return true }
	t.Cleanup(func() { sessionNames, sessionAlive, supervisorReady = prevNames, prevAlive, prevReady })

	retired, liveN := ReconcileSessions([]*state.Dispatch{live, ghost})

	if listed["player-app"] != 1 || listed["playerpulse"] != 1 {
		t.Errorf("listings per server = %v, want one each", listed)
	}
	if retired != 0 || liveN != 2 {
		t.Errorf("ReconcileSessions = retired:%d live:%d, want 0 and 2", retired, liveN)
	}
	if len(probed) != 1 || probed[0].Socket != "playerpulse" {
		t.Errorf("probes = %+v, want exactly the record its server did not name", probed)
	}
}
