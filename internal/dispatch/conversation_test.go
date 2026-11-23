//go:build !windows

package dispatch

import (
	"path/filepath"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

// claudeIn says that pid, a claude running conversation sid, is in session on
// the stubbed server — the way the machine says it: Claude Code's registry
// file for the pid, under a home and a /proc of the test's own.
func claudeIn(t *testing.T, sup *stubSupervisor, session string, pid int, sid string) {
	t.Helper()
	if sup.home == "" {
		sup.home = t.TempDir()
		t.Setenv("HOME", sup.home)
		fakeProc(t, nil)
	}
	writeRegistry(t, filepath.Join(sup.home, ".claude"), pid, sid, "")
	if sup.pids == nil {
		sup.pids = map[string][]int{}
	}
	// The pane's own shell first, as the real read lists it.
	sup.pids[session] = append(sup.pids[session], pid-1, pid)
	sup.alive[session] = true
}

// The report. The machine rebooted; ⏎ on the finished row resumed it, and a
// load that had read the records before that resume saved put the row back to
// "finished". The second ⏎ resumed the copy from before the first: to that copy
// the session the first resume started was a younger one that had taken the
// name, so a second claude was started on the same conversation beside it.
//
// Asked of the machine instead, the conversation is running, so nothing is
// started: the human is sent to it, and the record on disk — which the first
// resume and the hooks have moved on — is not saved back over.
func TestResumeOfARunningConversationStartsNoSecondClaude(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	rebootedAt := time.Now().Add(-72 * time.Hour)
	resumedAt := time.Now().Add(-20 * time.Second).Truncate(time.Second)
	sup := &stubSupervisor{
		alive: map[string]bool{},
		born:  map[string]time.Time{"disp-payment-retry": resumedAt},
		known: true, idle: false,
	}
	stubSessions(t, sup)

	// The copy on screen: from before the reboot.
	stale := finished(t, repo)
	stale.SessionStartedAt = &rebootedAt
	// The record on disk: the first resume's session, and the hooks since.
	onDisk := *stale
	held := resumedAt.Add(500 * time.Millisecond)
	onDisk.SessionStartedAt = &held
	onDisk.Status, onDisk.StatusReason = state.StatusNeedsInput, "waiting for your next prompt"
	if err := state.Save(&onDisk); err != nil {
		t.Fatal(err)
	}
	claudeIn(t, sup, "disp-payment-retry", 4242, "sess-abc")

	mode, session, err := Resume(stale, "")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResumeLive || session != "disp-payment-retry" {
		t.Errorf("Resume = %q in %q, want live in the session already running it", mode, session)
	}
	if sup.started.name != "" {
		t.Fatalf("started a second claude on the conversation in %q", sup.started.name)
	}
	if len(sup.killed) != 0 {
		t.Errorf("killed %v — the running conversation is the one the human asked for", sup.killed)
	}
	if got := state.Load(stale.ID); got.Status != state.StatusNeedsInput || got.TmuxSession != "disp-payment-retry" {
		t.Errorf("record on disk = %q in %q; the stale copy was saved back over it", got.Status, got.TmuxSession)
	}
}

// The other half of the aftermath: the record names a session that has gone
// (the twin was ended) while its conversation still runs in the one the first
// resume started. That session is the dispatcher's, so the record is pointed at
// it — only its address, and holding it, so the sweep does not retire a
// running dispatcher and the next ⏎ does not resume it yet again.
func TestResumeClaimsTheSessionItsConversationRunsIn(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	born := time.Now().Add(-time.Hour).Truncate(time.Second)
	sup := &stubSupervisor{alive: map[string]bool{}, born: map[string]time.Time{"disp-payment-retry": born}}
	stubSessions(t, sup)

	d := finished(t, repo)
	d.TmuxSession = "disp-payment-retry-2"
	later := born.Add(time.Minute)
	d.SessionStartedAt = &later
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}
	claudeIn(t, sup, "disp-payment-retry", 4242, "sess-abc")

	mode, session, err := Resume(d, "")
	if err != nil || mode != ResumeLive || session != "disp-payment-retry" {
		t.Fatalf("Resume = %q %q %v, want live in disp-payment-retry", mode, session, err)
	}
	got := state.Load(d.ID)
	if got.TmuxSession != "disp-payment-retry" || !got.HoldsSessionBorn(born) {
		t.Errorf("record names %q, holds a session born %v: %v; want it pointed at the running one", got.TmuxSession, born, got.HoldsSessionBorn(born))
	}
	if got.Status != state.StatusExited {
		t.Errorf("status = %q — the claim changes the address, and the hooks say the rest", got.Status)
	}
	if d.TmuxSession != "disp-payment-retry" {
		t.Errorf("the caller's copy still names %q", d.TmuxSession)
	}
}

// A conversation running in a session a human started on the repo's server is
// attached to and left theirs: a record that claimed it would end it on x.
func TestResumeDoesNotClaimAHumansSession(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)
	d := finished(t, initRepo(t))
	claudeIn(t, sup, "acme__payment-retry", 4242, "sess-abc")

	mode, session, err := Resume(d, "")
	if err != nil || mode != ResumeLive || session != "acme__payment-retry" {
		t.Fatalf("Resume = %q %q %v, want live in the human's session", mode, session, err)
	}
	if got := state.Load(d.ID); got.TmuxSession != "disp-payment-retry" {
		t.Errorf("the record claimed the human's session: %q", got.TmuxSession)
	}
	if sup.started.name != "" {
		t.Errorf("started %q beside it", sup.started.name)
	}
}

// Where the conversation already runs twice, the session the record names is
// the one the human is sent to, and both are listed for the cockpit to name.
func TestTwoSessionsOnOneConversationAreBothFound(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)
	d := finished(t, initRepo(t))
	d.TmuxSession = "disp-payment-retry-2"
	claudeIn(t, sup, "disp-payment-retry", 4242, "sess-abc")
	claudeIn(t, sup, "disp-payment-retry-2", 5252, "sess-abc")
	claudeIn(t, sup, "disp-other", 6262, "sess-other")

	got := ConversationSessions(d)
	if len(got) != 2 || got[0].Name != "disp-payment-retry" || got[1].Name != "disp-payment-retry-2" {
		t.Errorf("ConversationSessions = %+v, want both twins in name order and nothing else", got)
	}
	if at, ok := runningConversation(d); !ok || at.Name != "disp-payment-retry-2" {
		t.Errorf("runningConversation = %+v %v, want the one the record names", at, ok)
	}
}

// A claude on another account keeps its registry under that account's dir,
// and where the process's environment cannot be read the record's dir is the
// only place to find it. A transcript held open says the same thing.
func TestTheConversationIsFoundUnderItsAccountOrByItsTranscript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeProc(t, map[int][]string{
		7: {"/home/u/.claude/projects/-wt-acme/sess-open.jsonl"},
	})
	acct := t.TempDir()
	writeRegistry(t, acct, 9, "sess-acct", "")

	if !runsConversation([]int{8, 9}, "sess-acct", acct) {
		t.Error("missed a claude whose registry is under the record's account dir")
	}
	if runsConversation([]int{8, 9}, "sess-acct", "") {
		t.Error("found an account's claude without being told the account")
	}
	if !runsConversation([]int{6, 7}, "sess-open", "") {
		t.Error("missed a claude holding the conversation's transcript open")
	}
	if runsConversation([]int{6, 7}, "sess-other", "") {
		t.Error("matched a conversation nothing is running")
	}
	if ConversationSessions(nil) != nil || ConversationSessions(&state.Dispatch{}) != nil {
		t.Error("a record with no conversation was found running")
	}
}

// What a launch or resume would have named the session: the base, or the base
// with UniqueName's -N; nothing else.
func TestOwnSessionNames(t *testing.T) {
	d := &state.Dispatch{ID: "abc123", Slug: "payment-retry"}
	for name, want := range map[string]bool{
		"disp-payment-retry":      true,
		"disp-payment-retry-2":    true,
		"disp-payment-retry-17":   true,
		"disp-payment-retry-1":    false,
		"disp-payment-retry-02":   false,
		"disp-payment-retry-x":    false,
		"disp-payment-retry-cap":  false,
		"acme__payment-retry":     false,
		"disp-payment":            false,
		"disp-payment-retry-2-3":  false,
		"disp-payment-retry-2 ":   false,
		"disp-payment-retry-9999": true,
	} {
		if got := ownSessionName(d, name); got != want {
			t.Errorf("ownSessionName(%q) = %v, want %v", name, got, want)
		}
	}
	if !ownSessionName(&state.Dispatch{ID: "abc123"}, "disp-abc123-2") {
		t.Error("a record with no slug is named by its id")
	}
}
