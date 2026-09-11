package cockpit

// collect_sessions.go finds the sessions this cockpit did not start.
//
// A repo dispatches onto its own tmux server, and a human who works that way
// has servers of their own on the same machine — one per project, started by
// hand long before any dispatcher existed. Those sessions are real work in a
// product's repos, and the cockpit could not see them at all: it only ever
// asked about sessions it had a record for.
//
// They are NOT dispatchers and are never shown as ones. A session started by
// hand has no feature, no status, no base SHA and no effort figure; promoting
// it to a row that implies any of those is inventing a record, which this
// product refuses to do anywhere. It is listed as what it is — a session, with
// where it runs and the key that hands you the terminal.

import (
	"path/filepath"
	"sort"
	"strings"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// listServers and listSessions are the seams the enumeration is driven
// through: tests swap them rather than stand up tmux servers of their own on
// the machine running the suite.
var (
	listServers  = supervisor.Servers
	listSessions = supervisor.SessionList
)

// ownSession is a session on one of this machine's servers that no dispatch
// record claims.
type ownSession struct {
	name, socket string
	repo         string
	path         string
	// env is the repo's own environment, carried so that attaching hands the
	// human a client that can spawn panes seeing the repo's binaries — the
	// same reason a dispatch's attach carries it.
	env string
}

// unclaimedSessions is every live session on every server found by looking,
// minus the ones a dispatch record accounts for.
//
// A record claims a session by its whole address: the same name on two servers
// is two sessions, and treating a name as claimed everywhere would hide a
// human's own `disp-`-shaped session because a dispatcher elsewhere shares its
// name. Records of every status count, not just live ones — a finished
// dispatcher's session outlives its claude by design, and it is still the
// dispatcher's.
func unclaimedSessions(records []*state.Dispatch) []supervisor.SessionInfo {
	claimed := map[supervisor.Session]bool{}
	for _, d := range records {
		if d.TmuxSession != "" {
			claimed[supervisor.Session{Name: d.TmuxSession, Socket: d.TmuxSocket}] = true
		}
	}
	var out []supervisor.SessionInfo
	for _, sock := range listServers() {
		for _, s := range listSessions(sock) {
			if !claimed[s.Session] {
				out = append(out, s)
			}
		}
	}
	return out
}

// sessionsByProduct files each unclaimed session under the product whose repo
// it is working in.
//
// Two ways to attribute one, in order:
//
//	the SOCKET — a server named for a repo is that repo's, which is the rule
//	the launcher itself follows, so it is exact rather than inferred.
//	the PATH — where the session is actually working, matched against the
//	repo's checkouts. This is what catches a server whose name matches no
//	repo at all, which is the common case for servers a human named
//	themselves: `pp_calendar_isolated` is a checkout of `pp-calendar-sync`
//	and nothing but the directory says so.
//
// The longest matching checkout wins, because checkouts nest: a worktree
// inside a repo that is itself inside another scan root must be attributed to
// the nearer one.
//
// A session that matches neither is DROPPED. It is a session in something this
// cockpit does not know about, and there is no product to file it under — the
// product panel groups by product, and a bucket invented to hold it would be a
// claim about work nobody made.
func sessionsByProduct(found []repos.Repo, sessions []supervisor.SessionInfo) map[string][]ownSession {
	bySocket := map[string]repos.Repo{}
	for _, r := range found {
		if r.Socket != "" {
			bySocket[r.Socket] = r
		}
	}

	out := map[string][]ownSession{}
	for _, s := range sessions {
		r, ok := bySocket[s.Socket]
		if !ok {
			r, ok = repoForPath(found, s.Path)
		}
		if !ok {
			continue
		}
		// A repo in no product goes where its repo already goes: collectProducts
		// gives the unassigned ones a bucket of their own and lists them in the
		// portfolio, so this is the bucket the cockpit already has rather than a
		// product invented to hold them. Dropping them instead made the whole
		// tab invisible on a machine that has not assigned products yet — which
		// is every machine on its first run.
		product := r.Product
		if product == "" {
			product = clUnassigned
		}
		out[product] = append(out[product], ownSession{
			name: s.Name, socket: s.Socket, repo: r.Name, path: s.Path, env: r.Env,
		})
	}
	for p := range out {
		sort.SliceStable(out[p], func(i, j int) bool {
			if out[p][i].repo != out[p][j].repo {
				return out[p][i].repo < out[p][j].repo
			}
			return out[p][i].name < out[p][j].name
		})
	}
	return out
}

// repoForPath is the repo whose checkout dir contains path, longest match
// first. An empty path matches nothing: the Windows backend records no
// directory for a session, and "" would otherwise prefix-match every repo.
func repoForPath(found []repos.Repo, path string) (repos.Repo, bool) {
	if strings.TrimSpace(path) == "" {
		return repos.Repo{}, false
	}
	path = filepath.Clean(path)
	best, bestLen, ok := repos.Repo{}, -1, false
	for _, r := range found {
		for _, dir := range checkoutDirs(r) {
			if dir == "" || !underDir(path, dir) {
				continue
			}
			if len(dir) > bestLen {
				best, bestLen, ok = r, len(dir), true
			}
		}
	}
	return best, ok
}

func checkoutDirs(r repos.Repo) []string {
	dirs := []string{r.Path}
	for _, w := range r.Worktrees {
		dirs = append(dirs, w.Path)
	}
	return dirs
}

// underDir reports whether path is dir or sits inside it. Compared by path
// segment, never by string prefix: "/src/app" must not swallow "/src/app-2".
func underDir(path, dir string) bool {
	dir = filepath.Clean(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
