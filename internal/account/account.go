// Package account is the Claude subscriptions a dispatch can run under.
//
// An account is a Claude Code config directory. Claude Code keys everything a
// login is to CLAUDE_CONFIG_DIR — the settings, the transcripts under
// projects/, .claude.json, and the credentials themselves (on macOS the
// keychain entry is keyed to the directory too) — so two directories can be
// logged in to two subscriptions at once, and a session started with one
// spends that one's limits. Measured: an empty directory answers `claude auth
// status` with loggedIn false while the default one answers for the human's
// own Max login.
//
// The human's own login is always present and is called "default". Like
// dispatch.ModelDefault it passes nothing: no CLAUDE_CONFIG_DIR is set, and
// the session opens on whatever their Claude Code would open on. Setting the
// variable to ~/.claude explicitly is not the same thing — the keychain entry
// is keyed to the variable — so the default is never spelled out.
//
// Nothing here reads a credential. Who a directory is logged in as comes from
// `claude auth status --json`; how much of its limits are left comes from the
// status line (see limits.go), which is the one place Claude Code hands those
// figures to anything outside itself.
package account

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"claude-dispatcher/internal/config"
)

// Default is the name of the human's own login.
const Default = "default"

// Account is one subscription: a name and the config directory logged in to
// it. Dir is "" for the default account, as configured otherwise (~ allowed).
type Account struct {
	Name string
	Dir  string
}

// IsDefault reports whether a is the human's own login.
func (a Account) IsDefault() bool { return a.Dir == "" }

// Env is the CLAUDE_CONFIG_DIR a session of a is started with, "" for none.
func (a Account) Env() string {
	if a.Dir == "" {
		return ""
	}
	return filepath.Clean(config.ExpandHome(a.Dir))
}

// ConfigDir is the directory a's sessions actually read: Env, or for the
// default account what Claude Code itself resolves with nothing set.
func (a Account) ConfigDir() string {
	if e := a.Env(); e != "" {
		return e
	}
	return DefaultConfigDir()
}

// DefaultConfigDir is the directory a session with no override reads.
func DefaultConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Clean(config.ExpandHome(d))
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// List is every account, default first and the configured ones by name. A
// configured "default" is skipped: the name belongs to the human's own login,
// and a second account wearing it could never be told apart on a form.
func List(cfg *config.Config) []Account {
	out := []Account{{Name: Default}}
	if cfg == nil {
		return out
	}
	names := make([]string, 0, len(cfg.Accounts))
	for n, dir := range cfg.Accounts {
		if n == Default || strings.TrimSpace(n) == "" || strings.TrimSpace(dir) == "" {
			continue
		}
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		out = append(out, Account{Name: n, Dir: cfg.Accounts[n]})
	}
	return out
}

// Find is the account called name; "" and "default" are the default.
func Find(cfg *config.Config, name string) (Account, bool) {
	if name == "" || name == Default {
		return Account{Name: Default}, true
	}
	for _, a := range List(cfg) {
		if a.Name == name {
			return a, true
		}
	}
	return Account{}, false
}

// Auth is who a config directory is logged in as, in Claude Code's own words.
type Auth struct {
	// Known is false when the question could not be asked — no claude on
	// PATH, or one too old to have `auth status` — which is not the same as
	// an answer of "logged out", and nothing may be refused on it.
	Known    bool
	LoggedIn bool
	Email    string
	Plan     string // subscriptionType: "max", "pro", …
	Org      string
	Method   string // authMethod: "claude.ai", "none", …
}

// authStatus is the seam Probe asks through, so tests need no claude.
var authStatus = func(env string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "auth", "status", "--json")
	cmd.Env = withConfigDir(os.Environ(), env)
	cmd.Stdin = nil
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	// A logged-out directory exits 1 and still prints its JSON, so the
	// output is read whatever the exit status said.
	if out.Len() > 0 {
		return out.Bytes(), nil
	}
	return nil, err
}

// Probe asks claude who a is logged in as. It takes about a tenth of a second
// and touches no credential: claude reads its own store and says.
func Probe(a Account) Auth {
	raw, err := authStatus(a.Env())
	if err != nil || len(raw) == 0 {
		return Auth{}
	}
	var st struct {
		LoggedIn         bool   `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		Email            string `json:"email"`
		OrgName          string `json:"orgName"`
		SubscriptionType string `json:"subscriptionType"`
	}
	if json.Unmarshal(raw, &st) != nil {
		return Auth{}
	}
	return Auth{Known: true, LoggedIn: st.LoggedIn, Email: st.Email, Plan: st.SubscriptionType,
		Org: st.OrgName, Method: st.AuthMethod}
}

// withConfigDir is env with CLAUDE_CONFIG_DIR set to dir — or, for "", left
// exactly as it was: the default account inherits whatever the human runs
// with rather than being pinned to a spelling of it.
func withConfigDir(env []string, dir string) []string {
	if dir == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			out = append(out, kv)
		}
	}
	return append(out, "CLAUDE_CONFIG_DIR="+dir)
}

// CommandEnv is withConfigDir for a caller running claude itself under a —
// `account add` logging a directory in.
func CommandEnv(a Account) []string { return withConfigDir(os.Environ(), a.Env()) }

// Problems is every reason a dispatch under a would not get going on its own,
// each said as what to do about it. Empty means ready as far as can be told.
//
// Each one is a session that starts and then does nothing with nobody
// watching: a directory with no login opens on a login prompt; one without
// our hooks never reports a status, so its record sits at "launching" until
// a sweep calls it a ghost; one Claude Code has never been opened in stops on
// its first-run setup. All three are cheap to know before the launch and
// impossible to see after it, so the launch refuses on them (ADR 0009's rule:
// a dispatch that did not happen says why, at the moment of asking).
//
// The default account is the human's own and is not checked: nothing about
// it changed, and every dispatch before accounts existed ran on it.
func Problems(a Account, auth Auth) []string {
	if a.IsDefault() {
		return nil
	}
	dir := a.ConfigDir()
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return []string{"its config directory " + dir + " does not exist — claude-dispatcher account add " + a.Name}
	}
	var out []string
	if auth.Known && !auth.LoggedIn {
		out = append(out, "it is not logged in — claude-dispatcher account login "+a.Name)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "settings.json")); err != nil || !bytes.Contains(raw, []byte(hookMarker)) {
		out = append(out, "its hooks are not installed, so nothing would report its status — claude-dispatcher init")
	}
	if !Onboarded(a) {
		out = append(out, "Claude Code's first-run setup has not been done there — claude-dispatcher account login "+a.Name)
	}
	return out
}

// hookMarker is initcmd's: the text every one of our hook commands carries.
const hookMarker = "claude-dispatcher hook"

// Onboarded reports whether Claude Code's first-run setup (the theme picker
// and the rest) has been completed in a's directory. Until it has, every
// session there opens on it rather than on its prompt.
func Onboarded(a Account) bool {
	raw, err := os.ReadFile(filepath.Join(a.ConfigDir(), ".claude.json"))
	if err != nil {
		return false
	}
	var cfg struct {
		HasCompletedOnboarding bool `json:"hasCompletedOnboarding"`
	}
	return json.Unmarshal(raw, &cfg) == nil && cfg.HasCompletedOnboarding
}
