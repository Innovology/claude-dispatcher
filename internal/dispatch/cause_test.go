//go:build !windows

package dispatch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/cause"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// nixDevelopFailure is the shape of what nix develop printed on the reporting
// machine when it could not build a repo's shell: progress first, the failure
// last.
const nixDevelopFailure = `copying path '/nix/store/m65dckzfgr1lr90g5v6jx0iynnz38nsk-nss-cacert-3.115' from 'https://cache.nixos.org'...
building '/nix/store/83cg2b1bhanmg31ixndnvxj8cgp4hmvl-google-chrome-stable_142.0.7444.175-1_amd64.deb.drv'...
error: Cannot build '/nix/store/83cg2b1bhanmg31ixndnvxj8cgp4hmvl-google-chrome-stable_142.0.7444.175-1_amd64.deb.drv'.
       Reason: builder failed with exit code 1.
       Last 2 log lines:
       > curl: (22) The requested URL returned error: 404
       > error: cannot download google-chrome-stable_142.0.7444.175-1_amd64.deb from any mirror
error: Cannot build '/nix/store/j50vxjmba8z0kz8msyfv4zkvr9nv9vi9-nix-shell-env.drv'.
       Reason: 1 dependency failed.`

// A session that would not start because its environment would not build
// says so on its record — the build that failed and why — where it used to
// say "did not start: tmux new-session: copying path …", the first line nix
// printed and the only one with nothing wrong in it. The audit keeps all of it.
func TestAFailedLaunchRecordsItsCauseAndAuditsEverything(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	stubLaunch(t)
	prev := newSession
	newSession = func(supervisor.Session, string, string, string) error {
		return cause.Failed("tmux new-session", []byte(nixDevelopFailure), errors.New("exit status 1"))
	}
	t.Cleanup(func() { newSession = prev })

	if _, err := Launch(repos.Repo{Name: "acme", Path: repo}, "doomed", "go", ModeAuto, DefaultModel, DefaultRoot, false, ownAccount); err == nil {
		t.Fatal("expected the launch to fail")
	}
	recs := state.LoadAll()
	if len(recs) != 1 {
		t.Fatalf("%d records, want the failed one", len(recs))
	}
	want := "did not start: tmux new-session: error: Cannot build 'google-chrome-stable_142.0.7444.175-1_amd64.deb.drv' — " +
		"cannot download google-chrome-stable_142.0.7444.175-1_amd64.deb from any mirror"
	if recs[0].StatusReason != want {
		t.Errorf("status reason =\n%q\nwant\n%q", recs[0].StatusReason, want)
	}

	var failed []state.Event
	for _, ev := range state.LoadEvents() {
		if ev.Event == state.EventDispatchFailed {
			failed = append(failed, ev)
		}
	}
	if len(failed) != 1 {
		t.Fatalf("%d failures audited, want 1", len(failed))
	}
	if !strings.Contains(failed[0].Reason, "copying path") || !strings.Contains(failed[0].Reason, "1 dependency failed") {
		t.Errorf("the audit lost some of what nix printed:\n%s", failed[0].Reason)
	}
}

// git prints "Preparing worktree (…)" before the line that says why it could
// not, and the refusal a human reads was that first line. A folder already
// standing where the worktree goes is the plainest way to make git refuse.
func TestAWorktreeGitRefusedSaysWhyGitRefused(t *testing.T) {
	repo := initRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "somebody's file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ensureWorktree(repo, wt, "feature/thing", DefaultRoot)
	if err == nil {
		t.Fatal("git cut a worktree over a folder that was there")
	}
	if msg := err.Error(); strings.Contains(msg, "Preparing worktree") || !strings.Contains(msg, "fatal:") ||
		!strings.Contains(msg, "already exists") {
		t.Errorf("error = %q, want git's fatal line", msg)
	}
}
