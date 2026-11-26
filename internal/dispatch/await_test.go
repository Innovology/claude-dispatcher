//go:build !windows

package dispatch

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

func quickPoll(t *testing.T) {
	t.Helper()
	prev := awaitPoll
	awaitPoll = 5 * time.Millisecond
	t.Cleanup(func() { awaitPoll = prev })
}

// A claude that reports in while the caller waits is a start it may report:
// the hook moving the record off launching is the one thing only a running
// session can do.
func TestAwaitStartConfirmsOnceClaudeReportsIn(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	quickPoll(t)
	now := time.Now()
	saveAll(t, &state.Dispatch{ID: "up", Feature: "up", Status: state.StatusLaunching,
		TmuxSession: "disp-up", SessionStartedAt: &now})
	withFakeSessions(t, map[string]bool{"disp-up": true}, true)
	withPanes(t, false, true, "", nil)
	go func() { // the hook, landing while the caller waits
		time.Sleep(30 * time.Millisecond)
		if d := recordByID("up"); d != nil {
			d.Status, d.SessionID = state.StatusWorking, "sid"
			_ = state.Save(d)
		}
	}()

	o, d := AwaitStart("up", 5*time.Second)
	if o != StartConfirmed || d == nil || d.SessionID != "sid" {
		t.Fatalf("AwaitStart = %v (%+v), want confirmed with the session the hook bound", o, d)
	}
	if got := StartReport(o, d, 0); got != "claude reported in" {
		t.Errorf("report = %q", got)
	}
}

// The handoff that started this: the supervisor accepted the session, the
// command said "handed off", and claude had already died in the pane. The
// waiter makes the sweep's own look, so it hears the death in claude's words
// rather than waiting out the clock — and the record is retired whether or not
// a cockpit is open to do it.
func TestAwaitStartHearsALaunchThatDied(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	quickPoll(t)
	started := time.Now().Add(-5 * time.Second)
	saveAll(t, &state.Dispatch{ID: "dead", Feature: "dead", Status: state.StatusLaunching,
		TmuxSession: "disp-dead", SessionStartedAt: &started})
	withFakeSessions(t, map[string]bool{"disp-dead": true}, true)
	withPanes(t, true, true, incidentScreen, nil)

	begun := time.Now()
	o, d := AwaitStart("dead", time.Minute)
	if o != StartFailed {
		t.Fatalf("AwaitStart = %v, want failed", o)
	}
	if waited := time.Since(begun); waited > 5*time.Second {
		t.Errorf("took %s to hear a verdict that was there at the first look", waited)
	}
	got := StartReport(o, d, time.Minute)
	if !strings.Contains(got, "did not stay up: claude exited before it reported in (status 1): The current directory no longer exists") {
		t.Errorf("report = %q", got)
	}
	if recordOnDisk(t, "dead").Status != state.StatusExited {
		t.Error("the waiter heard the death but the record still says launching")
	}
}

// A session that has gone is the older half of the same verdict.
func TestAwaitStartHearsASessionThatWentAway(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	quickPoll(t)
	now := time.Now()
	saveAll(t, &state.Dispatch{ID: "gone", Feature: "gone", Status: state.StatusLaunching,
		TmuxSession: "disp-gone", SessionStartedAt: &now})
	withFakeSessions(t, nil, true)

	if o, d := AwaitStart("gone", time.Minute); o != StartFailed || !strings.Contains(d.StatusReason, "ended before claude reported in") {
		t.Errorf("AwaitStart = %v, reason %q", o, d.StatusReason)
	}
}

// A busy pane that never reports in is neither, and the caller is told so when
// the wait runs out — not that it worked, and not that it failed. The record is
// left launching, for the cockpit to call a startup prompt or the sweep to
// retire later.
func TestAwaitStartSaysWhenItCannotTell(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	quickPoll(t)
	started := time.Now().Add(-time.Minute)
	saveAll(t, &state.Dispatch{ID: "slow", Feature: "slow", Status: state.StatusLaunching,
		TmuxSession: "disp-slow", SessionStartedAt: &started})
	withFakeSessions(t, map[string]bool{"disp-slow": true}, true)
	withPanes(t, false, true, "", nil)

	o, d := AwaitStart("slow", 40*time.Millisecond)
	if o != StartUnconfirmed {
		t.Fatalf("AwaitStart = %v, want unconfirmed", o)
	}
	if got := StartReport(o, d, StartWait); !strings.HasPrefix(got, "not confirmed: claude has not reported in after 30s") ||
		!strings.Contains(got, "disp-slow") {
		t.Errorf("report = %q", got)
	}
	if recordOnDisk(t, "slow").Status != state.StatusLaunching {
		t.Error("a launch nobody could judge was changed")
	}
}

// A record that is not there started nothing.
func TestAwaitStartOfNoRecordIsNoStart(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if o, d := AwaitStart("nope", time.Millisecond); o != StartFailed || d != nil {
		t.Errorf("AwaitStart = %v, %v", o, d)
	}
	if got := StartReport(StartFailed, nil, 0); !strings.Contains(got, "record is gone") {
		t.Errorf("report = %q", got)
	}
}
