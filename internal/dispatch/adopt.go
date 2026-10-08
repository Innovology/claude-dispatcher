package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// adopt.go wraps a dispatch record around a session this cockpit did not
// start.
//
// The case it exists for: a human moving onto this tool who already has
// long-running orchestrator sessions holding the context that makes them
// fast. Listing those sessions (ADR 0015) tells them where the sessions are
// and nothing about what the sessions want, so adopting the cockpit meant
// leaving behind the thing that knew the work. Adoption is the migration
// path: the session keeps running, keeps its transcript and its context, and
// starts reporting like every other dispatcher.
//
// Nothing is created. No branch is cut, no worktree is added, no claude is
// launched. The record is a join key and a place to keep what the hooks say —
// which is why this is cheap and why it is safe: the only thing adoption
// changes about the session is that somebody is now listening to it.

// Adoption is a session being taken over, as the sessions collector found it.
type Adoption struct {
	// Session is the whole supervisor address — socket and name.
	Session supervisor.Session
	// Dir is where the session runs: the human's own checkout.
	Dir string
	// Repo is the repository that directory belongs to, already attributed.
	Repo repos.Repo
	// Feature names it on the table. Empty takes the branch checked out in
	// Dir, which on a layout of one folder per branch is the name the human
	// already calls this work.
	Feature string
	// SessionID is Claude Code's own id for the conversation. Empty asks the
	// machine (see BindSession): without one the hooks have nothing to match,
	// and the row would never move.
	SessionID string
}

// Adopt writes the record. The session must be live, the name must be free,
// and the conversation must be identifiable — a record that cannot be matched
// to a session's hooks is a row that would sit there saying nothing for ever,
// which is worse than not adopting it.
func Adopt(a Adoption, now time.Time) (*state.Dispatch, error) {
	if a.Session.Name == "" || a.Dir == "" {
		return nil, fmt.Errorf("no session to adopt")
	}
	if !sessionAlive(a.Session) {
		return nil, fmt.Errorf("%q is not running any more", a.Session.Name)
	}

	feature := strings.TrimSpace(a.Feature)
	if feature == "" {
		feature = AdoptName(a.Dir)
	}
	slug := Slugify(feature)
	if slug == "" {
		return nil, fmt.Errorf("%q leaves nothing to name a dispatcher by", feature)
	}
	if live := liveDispatch(slug); live != nil {
		return nil, fmt.Errorf("%q is already live in %s (session %s) — adopt it under a different name",
			live.Feature, live.RepoName, live.TmuxSession)
	}

	sid := a.SessionID
	if sid == "" {
		found, err := BindSession(a.Session, a.Dir)
		if err != nil {
			return nil, err
		}
		sid = found
	}

	d := &state.Dispatch{
		ID:       state.NewID(),
		Feature:  feature,
		Slug:     slug,
		RepoPath: a.Repo.Path,
		RepoName: a.Repo.Name,
		Product:  a.Repo.Product,
		// The branch it is already on, read rather than cut. Empty when the
		// directory is not a checkout at all, which is a session worth
		// adopting too: an orchestrator driving a repo from its parent.
		Branch: currentBranch(a.Dir),
		// Where it already runs. OwnsWorktree() is false for the life of this
		// record, so nothing removes it.
		WorktreePath: a.Dir,
		BaseSHA:      headSHA(a.Dir),
		// No prompt: the context is in the conversation, which is the whole
		// reason for adopting rather than dispatching afresh. Mode and model
		// stay empty for the same reason the record of a pre-flag dispatch
		// does — this session was launched by something else, and we do not
		// know what it was given.
		TmuxSession:      a.Session.Name,
		TmuxSocket:       a.Session.Socket,
		EnvCommand:       a.Repo.Env,
		PaneShell:        a.Repo.Shell,
		SessionID:        sid,
		Status:           state.StatusWorking,
		StatusReason:     "adopted — reporting from its next hook",
		CreatedAt:        now,
		AdoptedAt:        &now,
		SessionStartedAt: &now,
	}
	if err := state.Save(d); err != nil {
		return nil, err
	}
	audit(state.Event{
		Event: state.EventDispatchLaunched, DispatcherID: d.ID,
		Feature: feature, Repo: a.Repo.Name, Cwd: a.Dir,
	})
	return d, nil
}

// AdoptName is what an unnamed adoption is called: the branch in that
// directory, else the directory itself. On a layout that names a folder after
// its branch the two agree, and either way it is a name the human already
// uses for this work rather than one we invented.
func AdoptName(dir string) string {
	if b := currentBranch(dir); b != "" {
		return b
	}
	return filepath.Base(dir)
}

// currentBranch is the branch checked out in dir, or "" for a detached head or
// a directory that is not a checkout.
func currentBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	if b := strings.TrimSpace(string(out)); b != "HEAD" {
		return b
	}
	return ""
}

// headSHA is the commit dir is on at adoption. It is the record's BaseSHA, so
// every provenance figure counts what the dispatcher does from here — an
// adopted session is credited with the work it does under management, never
// with whatever was already on the branch.
func headSHA(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// BindSession works out which Claude Code conversation is running in a tmux
// session, which is the one fact adoption cannot do without: the hooks
// identify themselves by session id, and a record carrying the wrong one
// would quietly take another session's status.
//
// It asks the process, not the log. claude keeps its transcript open, named
// for the session id (`~/.claude/projects/<slug>/<id>.jsonl`), so the open
// file descriptors of the claude running under this pane name the session
// exactly. The event log can only say which conversation last reported from
// this DIRECTORY, and a human with two orchestrators in one checkout — the
// case this feature is for — has two answers there and no way to choose.
//
// The log is still the fallback, for a platform with no /proc and for a
// claude whose descriptor we cannot read, and it refuses rather than guesses
// when more than one conversation has been reporting from the directory.
func BindSession(sess supervisor.Session, dir string) (string, error) {
	if id := sessionIDFromProc(paneChildren(sess)); id != "" {
		return id, nil
	}
	ids := recentSessionIDs(dir)
	switch len(ids) {
	case 1:
		return ids[0], nil
	case 0:
		return "", fmt.Errorf("no Claude Code session has reported from %s — is the hook installed (claude-dispatcher init)?", dir)
	default:
		return "", fmt.Errorf("%d conversations are reporting from %s and this machine cannot say which one is in %q — adopt it from a cockpit on the same machine as the session",
			len(ids), dir, sess.Name)
	}
}

// paneChildren is every process running under the session's panes, nearest
// first. It is the same question SessionIdle asks, for the same reason: the
// pane's own command is a shell, and what matters is what it is running.
var paneChildren = func(sess supervisor.Session) []int {
	return supervisor.SessionPIDs(sess)
}

// sessionIDFromProc reads each process's open files and returns the session id
// of the first Claude Code transcript it finds. A pid we cannot read is
// skipped: another user's process, a kernel thread, a race with exit.
func sessionIDFromProc(pids []int) string {
	for _, pid := range pids {
		fds, err := os.ReadDir(filepath.Join(procRoot, itoa(pid), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(procRoot, itoa(pid), "fd", fd.Name()))
			if err != nil {
				continue
			}
			if id := transcriptSessionID(target); id != "" {
				return id
			}
		}
	}
	return ""
}

// procRoot is /proc, as a var so a test can hand it a directory of its own.
var procRoot = "/proc"

func itoa(i int) string { return strconv.Itoa(i) }

// transcriptSessionID is the session id a Claude Code transcript path names,
// or "" for any other file. The shape is `…/.claude/projects/<slug>/<id>.jsonl`
// — the directory is what distinguishes it from any other jsonl a session
// might have open.
func transcriptSessionID(path string) string {
	if !strings.HasSuffix(path, ".jsonl") {
		return ""
	}
	if !strings.Contains(filepath.ToSlash(filepath.Dir(filepath.Dir(path))), "/projects") {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

// recentSessionIDs is every conversation the event log has heard from in dir,
// newest first. Paths are compared resolved, because a session's directory and
// a hook's cwd can differ by a symlink.
func recentSessionIDs(dir string) []string {
	want := resolvePath(dir)
	seen := map[string]bool{}
	var out []string
	events := state.LoadEvents()
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.SessionID == "" || seen[e.SessionID] || resolvePath(e.Cwd) != want {
			continue
		}
		seen[e.SessionID] = true
		out = append(out, e.SessionID)
	}
	return out
}

func resolvePath(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
