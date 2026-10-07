//go:build !windows

package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// readyAccount is a config directory that would run a dispatch on its own:
// our hooks installed, Claude Code's first-run setup done.
func readyAccount(t *testing.T) account.Account {
	t.Helper()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"hooks":{"Stop":[{"hooks":[{"command":"/x/claude-dispatcher hook Stop"}]}]}}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"hasCompletedOnboarding":true}`), 0o644)
	return account.Account{Name: "work", Dir: dir}
}

func stubProbe(t *testing.T, auth account.Auth) {
	t.Helper()
	prev := probeAccount
	probeAccount = func(account.Account) account.Auth { return auth }
	t.Cleanup(func() { probeAccount = prev })
}

// An account is CLAUDE_CONFIG_DIR on the command line — carried inline, since
// tmux starts sessions with its server's environment — and on the record,
// where Resume finds it.
func TestLaunchRunsUnderTheAccount(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	cmd := stubLaunch(t)
	stubProbe(t, account.Auth{Known: true, LoggedIn: true})
	acct := readyAccount(t)

	d, err := Launch(repos.Repo{Name: "acme", Path: repo}, "work thing", "go", ModeAuto, DefaultModel, DefaultRoot, false, acct)
	if err != nil {
		t.Fatal(err)
	}
	if want := "CLAUDE_CONFIG_DIR=" + shellQuote(acct.Dir) + " "; !strings.HasPrefix(*cmd, want) {
		t.Errorf("launch command %q does not start with %q", *cmd, want)
	}
	if d.Account != "work" || d.ConfigDir != acct.Dir {
		t.Errorf("record account %q dir %q", d.Account, d.ConfigDir)
	}
}

// The default account sets nothing — not even CLAUDE_CONFIG_DIR=~/.claude,
// which Claude Code keys a different keychain entry to.
func TestLaunchOnTheDefaultSetsNoConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	cmd := stubLaunch(t)
	d, err := Launch(repos.Repo{Name: "acme", Path: repo}, "own thing", "go", ModeAuto, DefaultModel, DefaultRoot, false, ownAccount)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(*cmd, "CLAUDE_CONFIG_DIR") || d.Account != "" || d.ConfigDir != "" {
		t.Errorf("default launch carries an account: %q / %q %q", *cmd, d.Account, d.ConfigDir)
	}
}

// An account that would leave a session sitting unattended is refused before
// anything is made, with what to do about it in the refusal.
func TestLaunchRefusesAnAccountThatWouldStall(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	cmd := stubLaunch(t)
	stubProbe(t, account.Auth{Known: true, LoggedIn: false})

	_, err := Launch(repos.Repo{Name: "acme", Path: repo}, "work thing", "go", ModeAuto, DefaultModel, DefaultRoot, false, readyAccount(t))
	if err == nil || !strings.Contains(err.Error(), "not logged in") || !strings.Contains(err.Error(), "account login work") {
		t.Fatalf("err = %v", err)
	}
	if *cmd != "" || len(state.LoadAll()) != 0 {
		t.Error("a refused launch still started something")
	}
}

// Resume reopens the session under the login whose directory holds its
// transcript; under any other claude cannot find the conversation.
func TestResumeRunsUnderTheRecordedAccount(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	repo := initRepo(t)
	sup := &stubSupervisor{alive: map[string]bool{}}
	stubSessions(t, sup)
	d := finished(t, repo)
	d.Account, d.ConfigDir = "work", "/home/me/.claude-work"
	if _, _, err := Resume(d, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sup.started.cmd, "CLAUDE_CONFIG_DIR='/home/me/.claude-work' ") {
		t.Errorf("resume command %q is not under the account", sup.started.cmd)
	}
}
