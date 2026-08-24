//go:build !windows

package dispatch

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// stubLaunch swaps the supervisor calls Launch makes and hands back the
// command it was asked to run.
func stubLaunch(t *testing.T) *string {
	t.Helper()
	var cmd string
	prevNew, prevUniq, prevAlive := newSession, uniqueName, sessionAlive
	newSession = func(_, _, c string) error { cmd = c; return nil }
	uniqueName = func(base string) string { return base }
	sessionAlive = func(string) bool { return false }
	t.Cleanup(func() { newSession, uniqueName, sessionAlive = prevNew, prevUniq, prevAlive })
	return &cmd
}

// A prompt of any size reaches the session, because it does not travel inside
// the launch command.
//
// It used to. The command is ONE argument to the supervisor, and tmux carries a
// client command to its server in a single imsg capped at 16KB: measured
// against tmux 3.7b, 16313 bytes went through and 16314 came back "command too
// long". A prompt long enough to matter took the launch down with it.
func TestLaunchKeepsALongPromptOutOfTheCommand(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	cmd := stubLaunch(t)

	// Comfortably past tmux's ceiling, with the characters a real prompt has:
	// newlines, quotes, and things a shell would otherwise expand.
	prompt := strings.Repeat("do it 'properly' and \"carefully\" $HOME `now`\n", 1000)
	d, err := Launch(repos.Repo{Name: "acme", Path: repo}, "long prompt", prompt, ModeAuto, DefaultModel, false)
	if err != nil {
		t.Fatalf("a %d-byte prompt failed to launch: %v", len(prompt), err)
	}
	if len(*cmd) > 1024 {
		t.Errorf("the prompt is still inside the launch command (%d bytes) — the supervisor caps that", len(*cmd))
	}
	if !strings.Contains(*cmd, state.PromptPath(d.ID)) {
		t.Errorf("the launch command does not name the prompt file:\n%s", *cmd)
	}

	// And the command a real shell runs produces the prompt back, byte for
	// byte — the quoting is the whole risk of moving it out of line.
	out, err := exec.Command("sh", "-c", "printf %s "+readFileArg(state.PromptPath(d.ID))).Output()
	if err != nil {
		t.Fatalf("reading the prompt back through the shell: %v", err)
	}
	// $(…) drops trailing newlines, which is why the prompt is trimmed before
	// it is written; compare against what was actually stored.
	stored, err := os.ReadFile(state.PromptPath(d.ID))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != strings.TrimRight(string(stored), "\n") {
		t.Errorf("the shell did not reproduce the prompt:\ngot  %q\nwant %q",
			trunc(string(out)), trunc(string(stored)))
	}
	// The record still carries the prompt it was sent: the file is the
	// transport, not a replacement for the record. Trailing newlines are gone
	// from both, so the record says exactly what the session was given.
	if d.Prompt != strings.TrimRight(prompt, "\n") || d.Prompt != string(stored) {
		t.Error("the record and the prompt file disagree about what was dispatched")
	}
}

func trunc(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

// Past the kernel's own ceiling on one argument the launch is refused by name,
// at the moment of asking. The alternative is a session that starts, fails the
// exec, fires no hook and sits at "launching" until a sweep retires it.
func TestLaunchRefusesAPromptPastTheLimit(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	stubLaunch(t)

	_, err := Launch(repos.Repo{Name: "acme", Path: repo}, "huge",
		strings.Repeat("x", MaxPromptBytes+1), ModeAuto, DefaultModel, false)
	if err == nil {
		t.Fatal("a prompt past the limit was accepted")
	}
	if !strings.Contains(err.Error(), "the limit is") {
		t.Errorf("the refusal does not say what the limit is: %v", err)
	}
	// Refused before anything was made: no record, no session, no worktree.
	if got := state.LoadAll(); len(got) != 0 {
		t.Errorf("a refused launch left %d records behind", len(got))
	}
}

// Every attempt is in the audit, whether or not it produced a record. A launch
// that fails before state.Save leaves no record, no worktree and no session, so
// without this the entire account of it is a notice in a footer that the next
// keypress replaces — and every different failure reads as the same one thing.
func TestEveryDispatchAttemptIsAudited(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	stubLaunch(t)

	if _, err := Launch(repos.Repo{Name: "acme", Path: repo}, "good one", "go", ModeAuto, DefaultModel, false); err != nil {
		t.Fatal(err)
	}
	// A refusal that happens before any record exists.
	if _, err := Launch(repos.Repo{Name: "acme", Path: repo}, "!!!", "go", ModeAuto, DefaultModel, false); err == nil {
		t.Fatal("expected the empty slug to be refused")
	}

	var asked, launched, failed []state.Event
	for _, ev := range state.LoadEvents() {
		switch ev.Event {
		case state.EventDispatchAsked:
			asked = append(asked, ev)
		case state.EventDispatchLaunched:
			launched = append(launched, ev)
		case state.EventDispatchFailed:
			failed = append(failed, ev)
		}
	}
	if len(asked) != 2 {
		t.Errorf("%d asks audited, want both", len(asked))
	}
	if len(launched) != 1 || launched[0].Feature != "good one" {
		t.Errorf("launched events = %#v", launched)
	}
	if len(failed) != 1 || failed[0].Feature != "!!!" {
		t.Fatalf("failed events = %#v", failed)
	}
	if !strings.Contains(failed[0].Reason, "empty slug") {
		t.Errorf("the audit does not say why it failed: %q", failed[0].Reason)
	}
	if failed[0].Repo != "acme" {
		t.Errorf("the audit does not say which repo it was for: %q", failed[0].Repo)
	}
}

// A session that would not start keeps its record, and the record carries the
// supervisor's own words. "tmux launch failed" was what this said for every one
// of them, which turned the diagnosis tmux had handed us into four words that
// diagnose nothing.
func TestAFailedSessionKeepsItsRecordAndTheReason(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	stubLaunch(t)
	prev := newSession
	newSession = func(string, string, string) error {
		return errFake("tmux new-session: command too long")
	}
	t.Cleanup(func() { newSession = prev })

	if _, err := Launch(repos.Repo{Name: "acme", Path: repo}, "doomed", "go", ModeAuto, DefaultModel, false); err == nil {
		t.Fatal("expected the launch to fail")
	}
	recs := state.LoadAll()
	if len(recs) != 1 {
		t.Fatalf("%d records, want the failed one kept as evidence", len(recs))
	}
	if recs[0].Status != state.StatusExited {
		t.Errorf("status = %q, want exited", recs[0].Status)
	}
	if !strings.Contains(recs[0].StatusReason, "command too long") {
		t.Errorf("status reason = %q — it must carry the supervisor's own words", recs[0].StatusReason)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
