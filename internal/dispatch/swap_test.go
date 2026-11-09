//go:build !windows

package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/state"
)

// onDefault lays out a finished dispatcher the way Claude Code would have left
// it under the default login: the transcript in projects/<folder>/, its
// subagents beside it, its checkpoints under file-history/.
func onDefault(t *testing.T) (d *state.Dispatch, home string) {
	t.Helper()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	home = t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	d = finished(t, initRepo(t))
	proj := filepath.Join(home, "projects", "-wt-acme-payment-retry")
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(proj, "sess-abc.jsonl"), "{\"turn\":1}\n")
	write(filepath.Join(proj, "sess-abc", "subagents", "agent-1.jsonl"), "{}\n")
	write(filepath.Join(home, "file-history", "sess-abc", "f@v1"), "x")
	d.TranscriptPath = filepath.Join(proj, "sess-abc.jsonl")
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}
	return d, home
}

// The swap itself: the conversation lands in the new login's directory under
// the same project folder, the old one is left where it was, and the session
// resumes the same id under the new login — which is what --resume needs to
// find it (measured: "No conversation found" until the copy is made).
func TestSwapCarriesTheConversationAcross(t *testing.T) {
	d, home := onDefault(t)
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)
	stubProbe(t, account.Auth{Known: true, LoggedIn: true})
	to := readyAccount(t)

	mode, _, err := SwapAccount(d, to, "")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ResumeStarted {
		t.Errorf("mode = %q", mode)
	}
	moved := filepath.Join(to.Dir, "projects", "-wt-acme-payment-retry", "sess-abc.jsonl")
	if b, err := os.ReadFile(moved); err != nil || string(b) != "{\"turn\":1}\n" {
		t.Fatalf("transcript not carried: %q %v", b, err)
	}
	for _, p := range []string{
		filepath.Join(to.Dir, "projects", "-wt-acme-payment-retry", "sess-abc", "subagents", "agent-1.jsonl"),
		filepath.Join(to.Dir, "file-history", "sess-abc", "f@v1"),
		filepath.Join(home, "projects", "-wt-acme-payment-retry", "sess-abc.jsonl"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if !strings.HasPrefix(sup.started.cmd, "CLAUDE_CONFIG_DIR="+shellQuote(to.Dir)+" ") ||
		!strings.Contains(sup.started.cmd, "--resume 'sess-abc'") {
		t.Errorf("resume command %q", sup.started.cmd)
	}
	got := recordByIDForTest(t, d.ID)
	if got.Account != "work" || got.ConfigDir != to.Dir || got.TranscriptPath != moved {
		t.Errorf("record: account %q dir %q transcript %q", got.Account, got.ConfigDir, got.TranscriptPath)
	}
}

// Running out is an API error ending the turn, and nothing inside the session
// will start another; the swap says "continue" for the human. A turn that
// ended on a question gets nothing typed at it.
func TestSwapContinuesOnlyAFailedTurn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure *state.Failure
		want    bool
	}{
		{"out of credits", &state.Failure{Error: "rate_limit", At: time.Now()}, true},
		{"asked a question", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := onDefault(t)
			d.Status, d.Failure = state.StatusNeedsInput, tc.failure
			sup := &stubSupervisor{alive: map[string]bool{}}
			stubSessions(t, sup)
			stubProbe(t, account.Auth{Known: true, LoggedIn: true})
			if _, _, err := SwapAccount(d, readyAccount(t), ""); err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(sup.started.cmd, `"$(cat `); got != tc.want {
				t.Errorf("opening message sent = %v, want %v (%q)", got, tc.want, sup.started.cmd)
			}
		})
	}
}

// A claude still at its prompt is ended before the new one starts; two
// sessions writing one conversation under two logins would fork it.
func TestSwapEndsTheOldClaudeAtItsPrompt(t *testing.T) {
	d, _ := onDefault(t)
	d.Status = state.StatusNeedsInput
	sup := &stubSupervisor{alive: map[string]bool{"disp-payment-retry": true}, known: true, idle: false}
	stubSessions(t, sup)
	stubProbe(t, account.Auth{Known: true, LoggedIn: true})
	if _, _, err := SwapAccount(d, readyAccount(t), ""); err != nil {
		t.Fatal(err)
	}
	if len(sup.killed) != 1 || sup.killed[0] != "disp-payment-retry" {
		t.Errorf("killed %v", sup.killed)
	}
}

// Each refusal changes nothing: no copy, no kill, no session, the record as
// it was.
func TestSwapRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(d *state.Dispatch, sup *stubSupervisor, to *account.Account)
		auth  account.Auth
		want  string
	}{
		{"mid-turn", func(d *state.Dispatch, sup *stubSupervisor, _ *account.Account) {
			d.Status = state.StatusWorking
			sup.alive["disp-payment-retry"], sup.known = true, true
		}, account.Auth{Known: true, LoggedIn: true}, "mid-turn"},
		{"on a menu", func(d *state.Dispatch, sup *stubSupervisor, _ *account.Account) {
			d.Status = state.StatusBlocked
			sup.alive["disp-payment-retry"], sup.known = true, true
		}, account.Auth{Known: true, LoggedIn: true}, "on a menu"},
		{"done while still working", func(d *state.Dispatch, sup *stubSupervisor, _ *account.Account) {
			d.Status, d.Said = state.StatusDone, ""
			sup.alive["disp-payment-retry"], sup.known = true, true
		}, account.Auth{Known: true, LoggedIn: true}, "mid-turn"},
		{"adopted", func(d *state.Dispatch, _ *stubSupervisor, _ *account.Account) {
			now := time.Now()
			d.AdoptedAt = &now
		}, account.Auth{Known: true, LoggedIn: true}, "adopted"},
		{"same account", func(d *state.Dispatch, _ *stubSupervisor, to *account.Account) {
			d.Account, d.ConfigDir = to.Name, to.Dir
		}, account.Auth{Known: true, LoggedIn: true}, "already on work"},
		{"logged out", func(*state.Dispatch, *stubSupervisor, *account.Account) {},
			account.Auth{Known: true, LoggedIn: false}, "not logged in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := onDefault(t)
			sup := &stubSupervisor{alive: map[string]bool{}}
			stubSessions(t, sup)
			stubProbe(t, tc.auth)
			to := readyAccount(t)
			tc.setup(d, sup, &to)
			before := *d
			_, _, err := SwapAccount(d, to, "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if len(sup.killed) > 0 || sup.started.cmd != "" {
				t.Errorf("a refused swap acted: killed %v started %q", sup.killed, sup.started.cmd)
			}
			if _, err := os.Stat(filepath.Join(to.Dir, "projects")); err == nil {
				t.Error("a refused swap copied the conversation")
			}
			if d.Account != before.Account || d.ConfigDir != before.ConfigDir || d.TranscriptPath != before.TranscriptPath {
				t.Error("a refused swap changed the record")
			}
		})
	}
}

// Back to the default login sets no CLAUDE_CONFIG_DIR and copies into the
// directory Claude Code reads with none set.
func TestSwapBackToTheDefault(t *testing.T) {
	d, home := onDefault(t)
	from := readyAccount(t)
	if _, _, err := func() (ResumeMode, string, error) {
		sup := &stubSupervisor{alive: map[string]bool{}}
		stubSessions(t, sup)
		stubProbe(t, account.Auth{Known: true, LoggedIn: true})
		return SwapAccount(d, from, "")
	}(); err != nil {
		t.Fatal(err)
	}
	// The session ran on and wrote more under the account.
	if err := os.WriteFile(d.TranscriptPath, []byte("{\"turn\":1}\n{\"turn\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.Status = state.StatusNeedsInput
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)
	if _, _, err := SwapAccount(d, account.Account{Name: account.Default}, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sup.started.cmd, "CLAUDE_CONFIG_DIR") || d.Account != "" || d.ConfigDir != "" {
		t.Errorf("back on the default: cmd %q account %q dir %q", sup.started.cmd, d.Account, d.ConfigDir)
	}
	b, _ := os.ReadFile(filepath.Join(home, "projects", "-wt-acme-payment-retry", "sess-abc.jsonl"))
	if string(b) != "{\"turn\":1}\n{\"turn\":2}\n" {
		t.Errorf("the default's copy is not the latest: %q", b)
	}
}

func TestSwapOffered(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		d    state.Dispatch
		want bool
	}{
		{state.Dispatch{SessionID: "s", Status: state.StatusNeedsInput}, true},
		{state.Dispatch{SessionID: "s", Status: state.StatusExited}, true},
		{state.Dispatch{SessionID: "s", Status: state.StatusDone}, true},
		{state.Dispatch{Prompt: "go", Status: state.StatusExited}, true},
		{state.Dispatch{SessionID: "s", Status: state.StatusWorking}, false},
		{state.Dispatch{SessionID: "s", Status: state.StatusBlocked}, false},
		{state.Dispatch{SessionID: "s", Status: state.StatusNeedsInput, AdoptedAt: &now}, false},
		{state.Dispatch{Status: state.StatusExited}, false},
	} {
		if got := SwapOffered(&tc.d); got != tc.want {
			t.Errorf("%+v: %v, want %v", tc.d.Status, got, tc.want)
		}
	}
}

func recordByIDForTest(t *testing.T, id string) *state.Dispatch {
	t.Helper()
	for _, d := range state.LoadAll() {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("no record %s", id)
	return nil
}
