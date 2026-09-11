package cockpit

import (
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

func withServers(t *testing.T, servers map[string][]supervisor.SessionInfo) {
	t.Helper()
	prevS, prevL := listServers, listSessions
	names := []string{}
	for s := range servers {
		names = append(names, s)
	}
	listServers = func() []string { return names }
	listSessions = func(sock string) []supervisor.SessionInfo { return servers[sock] }
	t.Cleanup(func() { listServers, listSessions = prevS, prevL })
}

func sessAt(sock, name, path string) supervisor.SessionInfo {
	return supervisor.SessionInfo{Session: supervisor.Session{Name: name, Socket: sock}, Path: path}
}

// A record claims a session by its whole address. The same name on two servers
// is two sessions, and claiming a name everywhere would hide a human's own
// session because a dispatcher elsewhere happens to share it.
func TestASessionIsClaimedByAddressNotByName(t *testing.T) {
	withServers(t, map[string][]supervisor.SessionInfo{
		"player-app":  {sessAt("player-app", "disp-login", "/repos/player-app")},
		"playerpulse": {sessAt("playerpulse", "disp-login", "/repos/playerpulse")},
	})
	records := []*state.Dispatch{
		{TmuxSession: "disp-login", TmuxSocket: "player-app", Status: state.StatusWorking},
	}

	got := unclaimedSessions(records)
	if len(got) != 1 {
		t.Fatalf("unclaimed = %+v, want the one on the other server", got)
	}
	if got[0].Socket != "playerpulse" {
		t.Errorf("unclaimed = %+v, want the playerpulse one", got[0])
	}
}

// A finished dispatcher's session outlives its claude by design, and it is
// still the dispatcher's. Records of every status claim.
func TestAFinishedDispatchersSessionIsStillClaimed(t *testing.T) {
	withServers(t, map[string][]supervisor.SessionInfo{
		"acme": {sessAt("acme", "disp-old", "/repos/acme")},
	})
	records := []*state.Dispatch{
		{TmuxSession: "disp-old", TmuxSocket: "acme", Status: state.StatusDone},
	}
	if got := unclaimedSessions(records); len(got) != 0 {
		t.Errorf("unclaimed = %+v, want nothing — that session is a dispatcher's", got)
	}
}

// Attribution: the socket when it names a repo, the working directory when it
// does not. The second is the case that matters — a server a human named
// themselves matches no repo, and only where it is working says whose it is.
func TestSessionsAreFiledUnderTheProductOfTheRepoTheyWorkIn(t *testing.T) {
	found := []repos.Repo{
		{Name: "player-app", Product: "playerpulse", Socket: "player-app", Path: "/src/player-app/main",
			Worktrees: []repos.Worktree{{Path: "/src/player-app/main", Main: true}}},
		{Name: "pp-calendar-sync", Product: "playerpulse", Socket: "pp-calendar-sync", Path: "/src/pp-calendar-sync",
			Worktrees: []repos.Worktree{{Path: "/src/pp-calendar-sync", Main: true}}, Env: "nix develop --command"},
		{Name: "steve", Product: "steve", Socket: "steve", Path: "/src/steve"},
	}
	sessions := []supervisor.SessionInfo{
		sessAt("player-app", "player-app__main", "/src/player-app/main"),
		// The socket is named for nothing in the fleet; the path is the only
		// thing tying it to a repo.
		sessAt("pp_calendar_isolated", "cal", "/src/pp-calendar-sync"),
		sessAt("steve", "steve", "/src/steve"),
		// Neither a known socket nor a known path.
		sessAt("somewhere-else", "misc", "/tmp/scratch"),
	}

	got := sessionsByProduct(found, sessions)

	if n := len(got["playerpulse"]); n != 2 {
		t.Fatalf("playerpulse got %d sessions, want 2: %+v", n, got["playerpulse"])
	}
	byName := map[string]ownSession{}
	for _, s := range got["playerpulse"] {
		byName[s.name] = s
	}
	if byName["cal"].repo != "pp-calendar-sync" {
		t.Errorf("a session on an unrecognised socket was not placed by its path: %+v", byName["cal"])
	}
	if byName["cal"].env != "nix develop --command" {
		t.Errorf("the repo's environment did not reach the row: %+v", byName["cal"])
	}
	if len(got["steve"]) != 1 {
		t.Errorf("steve got %+v, want its one session", got["steve"])
	}
	for p, rows := range got {
		for _, r := range rows {
			if r.name == "misc" {
				t.Errorf("a session belonging to no repo was filed under %q anyway: %+v", p, r)
			}
		}
	}
}

// A repo in no product goes where its repo already goes. Dropping these made
// the tab invisible on a machine with no products assigned, which is every
// machine on its first run — and the portfolio already lists unassigned repos
// in a bucket of their own, so there is one to use rather than one to invent.
func TestSessionsInAnUnassignedRepoGoToTheBucketThatAlreadyExists(t *testing.T) {
	found := []repos.Repo{{Name: "loose", Socket: "loose", Path: "/src/loose"}}
	got := sessionsByProduct(found, []supervisor.SessionInfo{sessAt("loose", "x", "/src/loose")})
	if len(got[clUnassigned]) != 1 {
		t.Fatalf("sessionsByProduct = %+v, want it under %q", got, clUnassigned)
	}
	if got[clUnassigned][0].repo != "loose" {
		t.Errorf("row = %+v, want it to name its repo", got[clUnassigned][0])
	}
	// Still nothing invented for a session belonging to no repo at all.
	none := sessionsByProduct(found, []supervisor.SessionInfo{sessAt("elsewhere", "y", "/tmp/x")})
	if len(none) != 0 {
		t.Errorf("a session in no repo was filed anyway: %+v", none)
	}
}

// Checkouts nest, and folder names share prefixes. Both have to be handled by
// path segment: "/src/app" must not swallow "/src/app-2", and a worktree inside
// another repo's tree belongs to the nearer one.
func TestPathAttributionIsBySegmentAndNearest(t *testing.T) {
	found := []repos.Repo{
		{Name: "app", Product: "p", Path: "/src/app", Worktrees: []repos.Worktree{{Path: "/src/app"}}},
		{Name: "app-2", Product: "p", Path: "/src/app-2", Worktrees: []repos.Worktree{{Path: "/src/app-2"}}},
		{Name: "inner", Product: "p", Path: "/src/app/vendor/inner", Worktrees: []repos.Worktree{{Path: "/src/app/vendor/inner"}}},
	}
	for _, tc := range []struct{ path, want string }{
		{"/src/app-2/cmd", "app-2"},
		{"/src/app/cmd", "app"},
		{"/src/app/vendor/inner/pkg", "inner"},
	} {
		r, ok := repoForPath(found, tc.path)
		if !ok || r.Name != tc.want {
			t.Errorf("repoForPath(%q) = %q (ok=%v), want %q", tc.path, r.Name, ok, tc.want)
		}
	}
	if _, ok := repoForPath(found, ""); ok {
		t.Error("an empty path matched a repo — it would prefix-match every one of them")
	}
}

// The default server has no name, and a blank column reads as a fact nobody can
// find rather than as an answer.
func TestTheDefaultServerIsNamed(t *testing.T) {
	if got := serverLabel(""); got != "default server" {
		t.Errorf("serverLabel(\"\") = %q", got)
	}
	if got := serverLabel("player-app"); got != "player-app" {
		t.Errorf("serverLabel(named) = %q", got)
	}
}
