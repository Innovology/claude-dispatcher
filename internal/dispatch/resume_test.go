//go:build !windows

package dispatch

import (
	"claude-dispatcher/internal/supervisor"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// stubSupervisor swaps every supervisor call Resume makes and records the
// session it was asked to start.
type stubSupervisor struct {
	alive map[string]bool
	// born is when each live session was created, where a test says; a
	// session with none listed is one whose creation the backend cannot say.
	born    map[string]time.Time
	idle    bool
	known   bool
	killed  []string
	started struct{ name, socket, dir, cmd, env string }
}

func stubSessions(t *testing.T, s *stubSupervisor) {
	t.Helper()
	pAlive, pIdle, pKill, pUniq, pNew, pList := sessionAlive, sessionIdle, killSession, uniqueName, newSession, sessionList
	t.Cleanup(func() {
		sessionAlive, sessionIdle, killSession, uniqueName, newSession, sessionList = pAlive, pIdle, pKill, pUniq, pNew, pList
	})
	sessionAlive = func(sess supervisor.Session) bool { return s.alive[sess.Name] }
	sessionList = func(socket string) []supervisor.SessionInfo {
		var out []supervisor.SessionInfo
		for name, ok := range s.alive {
			if ok {
				out = append(out, supervisor.SessionInfo{
					Session: supervisor.Session{Name: name, Socket: socket}, Created: s.born[name],
				})
			}
		}
		return out
	}
	sessionIdle = func(supervisor.Session) (bool, bool) { return s.idle, s.known }
	killSession = func(sess supervisor.Session) error {
		s.killed = append(s.killed, sess.Name)
		s.alive[sess.Name] = false
		return nil
	}
	uniqueName = func(sess supervisor.Session) string { return sess.Name }
	newSession = func(sess supervisor.Session, dir, cmd, env string) error {
		s.started.name, s.started.socket = sess.Name, sess.Socket
		s.started.dir, s.started.cmd, s.started.env = dir, cmd, env
		return nil
	}
}

// finished builds a record for a dispatcher whose session is over, with a
// transcript on disk for --resume to find.
func finished(t *testing.T, repo string) *state.Dispatch {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "worktrees", "acme", "payment-retry")
	if _, err := ensureWorktree(repo, wt, "feature/payment-retry", DefaultRoot); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(t.TempDir(), "sess-abc.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &state.Dispatch{
		ID: state.NewID(), Feature: "Payment Retry", Slug: "payment-retry",
		RepoPath: repo, RepoName: "acme", Branch: "feature/payment-retry",
		WorktreePath: wt, TranscriptPath: transcript, SessionID: "sess-abc",
		TmuxSession: "disp-payment-retry", Prompt: "retry the payments",
		Status: state.StatusExited, StatusReason: "session ended",
	}
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}
	return d
}

// The whole point of the feature: a dispatcher whose session ended can be put
// back to work on its own transcript, in its own worktree, with a follow-up
// prompt — and the record leaves "exited" so the lifecycle hook will listen to
// the resumed session again.
func TestResumeStartsTheRecordedSession(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)

	d := finished(t, repo)
	mode, session, err := Resume(d, "now add the retry cap")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResumeStarted {
		t.Errorf("mode = %q, want %q", mode, ResumeStarted)
	}
	if session != sup.started.name || session == "" {
		t.Errorf("returned session %q, started %q", session, sup.started.name)
	}
	if sup.started.dir != d.WorktreePath {
		t.Errorf("resumed in %q, want the dispatch's own worktree %q", sup.started.dir, d.WorktreePath)
	}
	// The opening message travels as a file the session reads, not inside the
	// command: the command is one argument to the supervisor and every
	// supervisor caps that. So the command names the file, and the file says it.
	msgPath := state.PromptPath(d.ID + "-resume")
	for _, want := range []string{"--resume", "'sess-abc'", msgPath, "CLAUDE_DISPATCHER_ID=" + d.ID} {
		if !strings.Contains(sup.started.cmd, want) {
			t.Errorf("resume command %q is missing %q", sup.started.cmd, want)
		}
	}
	if b, err := os.ReadFile(msgPath); err != nil || string(b) != "now add the retry cap" {
		t.Errorf("message file = %q, %v; want the message the human typed", b, err)
	}
	// And it is its own file: resuming must not overwrite what the dispatch was
	// originally sent, which is the only copy of the ask on disk.
	if msgPath == state.PromptPath(d.ID) {
		t.Error("the resume message overwrote the launch prompt")
	}
	if d.Status != state.StatusLaunching {
		t.Errorf("record left at %q — the hook ignores a done record, so a resumed one must move off it", d.Status)
	}
	if d.TmuxSession != session {
		t.Errorf("record still points at the old session %q", d.TmuxSession)
	}
	// And it is on disk, not just in memory: the cockpit re-reads records.
	saved := state.LoadAll()
	if len(saved) != 1 || saved[0].TmuxSession != session {
		t.Errorf("saved record = %#v", saved)
	}
}

// Resuming with nothing to say is a real ask — "give it back to me" — and must
// not pass an empty argument claude would read as a first message.
func TestResumeWithoutAPromptPassesNone(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)

	if _, _, err := Resume(finished(t, initRepo(t)), ""); err != nil {
		t.Fatal(err)
	}
	// The claude invocation only: the tail that picks the pane's shell carries
	// an empty pair of quotes of its own when no shell is configured, and it is
	// not an argument to anything (see exitShell).
	claudePart, _, _ := strings.Cut(sup.started.cmd, "; cdsh=")
	if strings.Contains(claudePart, "''") {
		t.Errorf("empty prompt passed as an argument: %q", sup.started.cmd)
	}
}

// The worktree is reclaimed when a clean dispatcher is killed, and the
// transcript is findable only from the directory the session ran in — so a
// resume has to put it back at the same path.
func TestResumeRebuildsAReclaimedWorktree(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)

	d := finished(t, repo)
	if !CleanupWorktree(d.RepoPath, d.WorktreePath) {
		t.Fatal("the fixture worktree should have been clean enough to remove")
	}
	if _, _, err := Resume(d, ""); err != nil {
		t.Fatal(err)
	}
	if got := worktreeBranch(t, d.WorktreePath); got != "feature/payment-retry" {
		t.Errorf("rebuilt worktree is on %q", got)
	}
	if sup.started.dir != d.WorktreePath {
		t.Errorf("resumed in %q, want %q", sup.started.dir, d.WorktreePath)
	}
}

// A session left running is never resumed over the top of itself: two claude
// processes appending to one transcript is worse than the thing it fixes. The
// caller is told to attach instead.
func TestResumeLeavesABusySessionAlone(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	sup := &stubSupervisor{alive: map[string]bool{"disp-payment-retry": true}, idle: false, known: true}
	stubSessions(t, sup)

	d := finished(t, initRepo(t))
	mode, session, err := Resume(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResumeLive || session != "disp-payment-retry" {
		t.Errorf("mode = %q session = %q, want the live session handed back", mode, session)
	}
	if sup.started.name != "" {
		t.Error("a second session was started over a running one")
	}
	if len(sup.killed) != 0 {
		t.Errorf("a running session was killed: %v", sup.killed)
	}
}

// The shell a dispatch drops to when claude exits is not a session doing
// anything: it is reclaimed rather than left behind as an orphan.
func TestResumeReclaimsTheIdleShell(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	sup := &stubSupervisor{alive: map[string]bool{"disp-payment-retry": true}, idle: true, known: true}
	stubSessions(t, sup)

	if _, _, err := Resume(finished(t, initRepo(t)), ""); err != nil {
		t.Fatal(err)
	}
	if len(sup.killed) != 1 || sup.killed[0] != "disp-payment-retry" {
		t.Errorf("killed = %v, want the idle shell reclaimed", sup.killed)
	}
	if sup.started.name == "" {
		t.Error("nothing was started")
	}
}

// A backend that cannot see into a session (the Windows console one) must not
// have it killed on a guess.
func TestResumeLeavesAnUnknownSessionAlone(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	sup := &stubSupervisor{alive: map[string]bool{"disp-payment-retry": true}, idle: false, known: false}
	stubSessions(t, sup)

	mode, _, err := Resume(finished(t, initRepo(t)), "")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResumeStarted {
		t.Errorf("mode = %q, want a new session started alongside", mode)
	}
	if len(sup.killed) != 0 {
		t.Errorf("killed %v on a guess", sup.killed)
	}
}

// A session that took the record's name after the record's own ended is
// somebody else's: an adopted session's name comes back every time the human's
// wrapper opens one. It is not handed back as the dispatcher's — that would
// attach ⏎ to another conversation — and it is never killed for sitting at its
// shell, which would end the human's session. A new one is started alongside.
func TestResumeLeavesAYoungerSessionUnderItsNameAlone(t *testing.T) {
	for _, idle := range []bool{false, true} {
		t.Run(map[bool]string{false: "busy", true: "idle"}[idle], func(t *testing.T) {
			t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
			held := time.Date(2026, 10, 13, 11, 54, 36, 0, time.Local)
			sup := &stubSupervisor{
				alive: map[string]bool{"disp-payment-retry": true},
				born:  map[string]time.Time{"disp-payment-retry": held.Add(50 * time.Hour)},
				idle:  idle, known: true,
			}
			stubSessions(t, sup)

			d := finished(t, initRepo(t))
			d.SessionStartedAt = &held
			mode, _, err := Resume(d, "")
			if err != nil {
				t.Fatal(err)
			}
			if mode != ResumeStarted {
				t.Errorf("mode = %q, want a new session started alongside", mode)
			}
			if len(sup.killed) != 0 {
				t.Errorf("killed %v, a session that only shares the name", sup.killed)
			}
		})
	}
}

// An adopted session is the human's even when it is the record's own: claude
// exiting in it leaves their shell, and perhaps other windows, which ⏎ must
// not end. Idle, it is left running and a new session is started alongside.
func TestResumeNeverKillsAnAdoptedSession(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	held := time.Date(2026, 10, 13, 11, 54, 36, 0, time.Local)
	sup := &stubSupervisor{
		alive: map[string]bool{"disp-payment-retry": true},
		born:  map[string]time.Time{"disp-payment-retry": held.Add(-time.Hour)},
		idle:  true, known: true,
	}
	stubSessions(t, sup)

	d := finished(t, initRepo(t))
	d.SessionStartedAt = &held
	d.AdoptedAt = &held
	mode, _, err := Resume(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResumeStarted {
		t.Errorf("mode = %q, want a new session started alongside", mode)
	}
	if len(sup.killed) != 0 {
		t.Errorf("killed %v, the human's own session", sup.killed)
	}
}

// Nothing is invented: with no session id, or with the transcript gone, there
// is nothing to resume and the human is told so rather than handed a fresh
// session wearing the old one's name.
func TestResumeRefusesWhatItCannotResume(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)

	// A repo each: one branch cannot be checked out in two worktrees at once.
	// No conversation is started again (below) — unless there is not even a
	// prompt to start it with.
	noSession := finished(t, initRepo(t))
	noSession.SessionID, noSession.TranscriptPath, noSession.Prompt = "", "", ""
	if _, _, err := Resume(noSession, ""); err == nil || !strings.Contains(err.Error(), "never started") {
		t.Errorf("no session id and no prompt: err = %v", err)
	}

	gone := finished(t, initRepo(t))
	if err := os.Remove(gone.TranscriptPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resume(gone, ""); err == nil || !strings.Contains(err.Error(), "transcript") {
		t.Errorf("missing transcript: err = %v", err)
	}
	if sup.started.name != "" {
		t.Error("a session was started for something that cannot be resumed")
	}
}

// turnless is the reported record (ADR 0039): a dispatch whose session id came
// from a claude that started and ended without a turn, so the transcript it
// names was never written. The log holds what the reporting log held — the
// launch, the session's start and end, and a refused resume of the cockpit's
// own carrying the session's id — plus the idle prompt another turnless
// session in that log drew, and nothing else.
func turnless(t *testing.T) *state.Dispatch {
	t.Helper()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	d := finished(t, initRepo(t))
	if err := os.Remove(d.TranscriptPath); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []state.Event{
		{Event: state.EventDispatchLaunched, DispatcherID: d.ID, Feature: d.Feature},
		{Event: "SessionStart", SessionID: "sess-abc", Cwd: d.WorktreePath},
		{Event: "Notification:idle_prompt", SessionID: "sess-abc", Cwd: d.WorktreePath},
		{Event: "SessionEnd", SessionID: "sess-abc", Cwd: d.WorktreePath},
		{Event: "Resumed", DispatcherID: d.ID, SessionID: "sess-abc", Reason: "failed: the transcript is gone"},
	} {
		state.AppendEvent(ev)
	}
	return d
}

// A session id is not a conversation. One that never had a turn — the claude
// a human opened in a dead dispatch's pane and closed again — left the record
// refusing every ⏎ with "the transcript is gone", and nothing on the row could
// start the dispatch. It is started again from its brief, as a launch that
// never ran is, and the stranger's session leaves the record.
func TestResumeStartsAConversationThatNeverBeganAgain(t *testing.T) {
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)
	d := turnless(t)

	mode, session, err := Resume(d, "")
	if err != nil {
		t.Fatalf("Resume = %v, want the dispatch started again", err)
	}
	if mode != ResumeRelaunched || session == "" {
		t.Fatalf("mode = %q session = %q, want a relaunch", mode, session)
	}
	if strings.Contains(sup.started.cmd, "--resume") || !strings.Contains(sup.started.cmd, state.PromptPath(d.ID)) {
		t.Errorf("command %q is not the original launch", sup.started.cmd)
	}
	if b, err := os.ReadFile(state.PromptPath(d.ID)); err != nil || string(b) != "retry the payments" {
		t.Errorf("prompt file = %q, %v; want what it was dispatched with", b, err)
	}
	got := recordByIDForTest(t, d.ID)
	if got.SessionID != "" || got.TranscriptPath != "" {
		t.Errorf("record kept session %q / %q, which was never this dispatch's conversation", got.SessionID, got.TranscriptPath)
	}
	if got.Status != state.StatusLaunching || got.StatusReason != "starting again" {
		t.Errorf("record left at %q / %q", got.Status, got.StatusReason)
	}
}

// A conversation that existed and whose transcript has gone (Claude Code's
// cleanup, a human's rm) looks the same on the record as one that never began,
// and relaunching the brief there would do the work over again. So the refusal
// stands wherever the evidence is anything short of a session the log saw
// start and saw do nothing — and a branch nobody has worked on.
func TestResumeStillRefusesAConversationThatHasGone(t *testing.T) {
	git := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	cases := []struct {
		name string
		make func(t *testing.T, d *state.Dispatch)
	}{
		{"a prompt reached it", func(t *testing.T, d *state.Dispatch) {
			state.AppendEvent(state.Event{Event: "UserPromptSubmit", SessionID: "sess-abc"})
		}},
		// Measured: a session with two Stops and no prompt on record.
		{"it answered with no prompt on record", func(t *testing.T, d *state.Dispatch) {
			state.AppendEvent(state.Event{Event: "Stop", SessionID: "sess-abc"})
		}},
		{"an API error ended a turn", func(t *testing.T, d *state.Dispatch) {
			state.AppendEvent(state.Event{Event: "StopFailure", SessionID: "sess-abc", Reason: "rate_limit"})
		}},
		{"it asked a question", func(t *testing.T, d *state.Dispatch) {
			state.AppendEvent(state.Event{Event: "PreToolUse:AskUserQuestion", SessionID: "sess-abc"})
		}},
		{"its own claude worked under another session", func(t *testing.T, d *state.Dispatch) {
			state.AppendEvent(state.Event{Event: "UserPromptSubmit", DispatcherID: d.ID, SessionID: "sess-first"})
		}},
		{"the log never saw it start", func(t *testing.T, d *state.Dispatch) {
			t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
			if err := state.Save(d); err != nil {
				t.Fatal(err)
			}
			state.AppendEvent(state.Event{Event: "SessionEnd", SessionID: "sess-abc"})
		}},
		{"its transcript is under its config dir", func(t *testing.T, d *state.Dispatch) {
			p := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-elsewhere", "sess-abc.jsonl")
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"the record has commits", func(t *testing.T, d *state.Dispatch) {
			d.Commits = []string{"0123abc"}
		}},
		{"the branch has moved since the dispatch", func(t *testing.T, d *state.Dispatch) {
			d.BaseSHA = git(t, d.WorktreePath, "rev-parse", "HEAD")
			git(t, d.WorktreePath, "commit", "--allow-empty", "-m", "by hand")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sup := &stubSupervisor{alive: map[string]bool{}}
			stubSessions(t, sup)
			d := turnless(t)
			c.make(t, d)

			if _, _, err := Resume(d, ""); err == nil || !strings.Contains(err.Error(), "is gone") {
				t.Errorf("Resume = %v, want the transcript refused as gone", err)
			}
			if sup.started.name != "" {
				t.Errorf("started %q over a conversation that existed", sup.started.name)
			}
			if d.SessionID != "sess-abc" {
				t.Errorf("session id dropped to %q", d.SessionID)
			}
		})
	}
	// The branch check, at its edges: a branch that has not moved, or is not
	// there at all, carries no work.
	d := turnless(t)
	d.BaseSHA = git(t, d.WorktreePath, "rev-parse", "HEAD")
	if branchMoved(d) {
		t.Error("a branch still at its base reads as moved")
	}
	d.Branch = "feature/not-there"
	if branchMoved(d) {
		t.Error("a branch that does not exist reads as moved")
	}
}

// A record from before the hook bound session ids still has its transcript, and
// Claude Code names that file after the session — so the id is recoverable.
func TestSessionIDOfFallsBackToTheTranscript(t *testing.T) {
	cases := []struct {
		d    *state.Dispatch
		want string
	}{
		{&state.Dispatch{SessionID: "abc"}, "abc"},
		{&state.Dispatch{TranscriptPath: "/p/-Users-me-repo/9f8e.jsonl"}, "9f8e"},
		{&state.Dispatch{SessionID: "abc", TranscriptPath: "/p/x/def.jsonl"}, "abc"},
		{&state.Dispatch{}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := SessionIDOf(c.d); got != c.want {
			t.Errorf("SessionIDOf(%#v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// A launch that died before claude ran keeps its row (ADR 0009), and ⏎ on it
// used to refuse — "nothing to resume" — leaving the human to type the whole
// dispatch again. It launches it again instead: same worktree, the prompt it
// was sent with, and an environment that works where it now stands. The
// reported case was a flake on the checkout's branch and not on main, so the
// record carried a bare `nix develop` that found nothing in the worktree.
func TestResumeStartsANeverStartedLaunchAgain(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)

	repo := initRepo(t)
	d := finished(t, repo)
	d.SessionID, d.TranscriptPath = "", ""
	d.EnvCommand = repos.NixDevelop
	d.Status, d.StatusReason = state.StatusExited, "did not start: tmux new-session: path … does not contain a 'flake.nix'"
	// The checkout's branch has a flake; the worktree, cut from elsewhere, not.
	if err := os.WriteFile(filepath.Join(repo, "flake.nix"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !NeverStarted(d) {
		t.Fatal("a record with no session id and no transcript never started")
	}

	mode, session, err := Resume(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResumeRelaunched || session == "" {
		t.Fatalf("mode = %q session = %q, want a relaunch", mode, session)
	}
	if sup.started.dir != d.WorktreePath {
		t.Errorf("started in %q, want its own worktree %q", sup.started.dir, d.WorktreePath)
	}
	if strings.Contains(sup.started.cmd, "--resume") || !strings.Contains(sup.started.cmd, state.PromptPath(d.ID)) {
		t.Errorf("command %q is not the original launch", sup.started.cmd)
	}
	if b, err := os.ReadFile(state.PromptPath(d.ID)); err != nil || string(b) != "retry the payments" {
		t.Errorf("prompt file = %q, %v; want what it was dispatched with", b, err)
	}
	if want := "nix develop " + repo + " --no-write-lock-file --command"; sup.started.env != want || d.EnvCommand != want {
		t.Errorf("env = %q (record %q), want the checkout's flake named: %q", sup.started.env, d.EnvCommand, want)
	}
	if d.Status != state.StatusLaunching || d.StatusReason != "starting again" {
		t.Errorf("record left at %q / %q", d.Status, d.StatusReason)
	}
}
