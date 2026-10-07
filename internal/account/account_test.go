package account

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
)

// The payload the installed claude actually hands its status line, as
// captured from claude 2.1.281 on a Max login (trimmed to what we read plus a
// neighbour, so an unknown key is exercised).
const capturedPayload = `{"session_id":"s","model":{"id":"claude-haiku"},
"rate_limits":{"five_hour":{"used_percentage":5,"resets_at":1791367800},
"seven_day":{"used_percentage":3,"resets_at":1791892800}}}`

func TestStatusLineReadingRoundTrips(t *testing.T) {
	st, cd := t.TempDir(), "/home/me/.claude-work"
	now := time.Unix(1791360000, 0)
	if err := RecordStatusLine(st, cd, []byte(capturedPayload), now); err != nil {
		t.Fatal(err)
	}
	l, ok := ReadLimits(st, cd)
	if !ok {
		t.Fatal("no reading after recording one")
	}
	if l.FiveHour.UsedPct != 5 || l.SevenDay.ResetsAt != 1791892800 || !l.At.Equal(now) {
		t.Fatalf("reading = %+v", l)
	}
	if got := l.Brief(now); got != "5h 95% · wk 97% left" {
		t.Errorf("Brief = %q", got)
	}
	if got := l.Least(now); got != 95 {
		t.Errorf("Least = %d, want 95", got)
	}
	// Another directory has its own reading, and none yet.
	if _, ok := ReadLimits(st, "/home/me/.claude"); ok {
		t.Error("a reading leaked to another config dir")
	}
}

// A payload without rate limits — before the first response, or an API-key
// login — is not a reading, and must not wipe the last one.
func TestAPayloadWithoutLimitsKeepsTheLastReading(t *testing.T) {
	st, cd := t.TempDir(), "/x"
	now := time.Unix(1791360000, 0)
	_ = RecordStatusLine(st, cd, []byte(capturedPayload), now)
	if err := RecordStatusLine(st, cd, []byte(`{"session_id":"s"}`), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if l, ok := ReadLimits(st, cd); !ok || !l.At.Equal(now) {
		t.Fatalf("reading = %+v %v, want the first one kept", l, ok)
	}
}

// Past resets_at the window has emptied: that is the window's own rule.
func TestAWindowPastItsResetIsFull(t *testing.T) {
	w := Window{UsedPct: 90, ResetsAt: 1000}
	if left, reset := w.Left(time.Unix(999, 0)); left != 10 || reset {
		t.Errorf("before reset: %d %v", left, reset)
	}
	if left, reset := w.Left(time.Unix(1000, 0)); left != 100 || !reset {
		t.Errorf("at reset: %d %v", left, reset)
	}
	if left, _ := (Window{UsedPct: 140}).Left(time.Unix(0, 0)); left != 0 {
		t.Errorf("over 100%% used reads %d left", left)
	}
}

func TestListPutsDefaultFirstAndSkipsAnImpostor(t *testing.T) {
	cfg := &config.Config{Accounts: map[string]string{
		"work": "~/.claude-work", "default": "~/.claude-x", "client": "/c", "blank": " ",
	}}
	var names []string
	for _, a := range List(cfg) {
		names = append(names, a.Name)
	}
	if want := []string{"default", "client", "work"}; !slices.Equal(names, want) {
		t.Fatalf("List = %v, want %v", names, want)
	}
	if a, ok := Find(cfg, ""); !ok || !a.IsDefault() {
		t.Error("\"\" is not the default account")
	}
	if _, ok := Find(cfg, "nope"); ok {
		t.Error("found an account that is not configured")
	}
}

// The default account must never be spelled out: the keychain entry is keyed
// to CLAUDE_CONFIG_DIR, so setting it to ~/.claude is a different login.
func TestTheDefaultAccountSetsNothing(t *testing.T) {
	if e := (Account{Name: Default}).Env(); e != "" {
		t.Errorf("default Env = %q", e)
	}
	home, _ := os.UserHomeDir()
	if e := (Account{Name: "w", Dir: "~/.claude-w"}).Env(); e != filepath.Join(home, ".claude-w") {
		t.Errorf("Env = %q", e)
	}
	env := withConfigDir([]string{"A=1", "CLAUDE_CONFIG_DIR=/old"}, "")
	if !slices.Equal(env, []string{"A=1", "CLAUDE_CONFIG_DIR=/old"}) {
		t.Errorf("default env changed: %v", env)
	}
	env = withConfigDir([]string{"A=1", "CLAUDE_CONFIG_DIR=/old"}, "/new")
	if !slices.Equal(env, []string{"A=1", "CLAUDE_CONFIG_DIR=/new"}) {
		t.Errorf("env = %v", env)
	}
}

func TestProbeReadsAuthStatus(t *testing.T) {
	prev := authStatus
	t.Cleanup(func() { authStatus = prev })
	var asked string
	authStatus = func(env string) ([]byte, error) {
		asked = env
		return []byte(`{"loggedIn":true,"authMethod":"claude.ai","email":"a@b.c","orgName":"Org","subscriptionType":"max"}`), nil
	}
	got := Probe(Account{Name: "w", Dir: "/w"})
	if !got.Known || !got.LoggedIn || got.Email != "a@b.c" || got.Plan != "max" || asked != "/w" {
		t.Fatalf("Probe = %+v (asked %q)", got, asked)
	}
	authStatus = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	if got := Probe(Account{Name: "w", Dir: "/w"}); got.Known {
		t.Error("a probe that could not run claims to know")
	}
}

// Each problem is a session that would start and sit there; each is named
// with what to do about it.
func TestProblemsNameWhatWouldStallASession(t *testing.T) {
	dir := t.TempDir()
	a := Account{Name: "work", Dir: dir}
	got := Problems(a, Auth{Known: true, LoggedIn: false})
	if len(got) != 3 {
		t.Fatalf("Problems = %q, want login, hooks and onboarding", got)
	}
	for _, want := range []string{"not logged in", "hooks", "first-run"} {
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Errorf("no problem mentions %q: %q", want, got)
		}
	}

	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"hooks":{"Stop":[{"hooks":[{"command":"/bin/claude-dispatcher hook Stop"}]}]}}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"hasCompletedOnboarding":true}`), 0o644)
	if got := Problems(a, Auth{Known: true, LoggedIn: true}); len(got) != 0 {
		t.Errorf("a ready account has problems: %q", got)
	}
	// An unknown login is not a refusal: nothing may be refused on a question
	// that could not be asked.
	if got := Problems(a, Auth{}); len(got) != 0 {
		t.Errorf("an unasked login is a problem: %q", got)
	}
	if got := Problems(Account{Name: "gone", Dir: filepath.Join(dir, "nope")}, Auth{}); len(got) != 1 || !strings.Contains(got[0], "does not exist") {
		t.Errorf("missing dir: %q", got)
	}
	if got := Problems(Account{Name: Default}, Auth{Known: true}); got != nil {
		t.Errorf("the default account was checked: %q", got)
	}
}
