package fleetcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// handoffCall is what `handoff` asked dispatch.Handoff for.
type handoffCall struct {
	from         string
	to           string
	title, brief string
	mode         dispatch.Mode
	model        dispatch.Model
	root         dispatch.Root
	acct         account.Account
	env          string // CLAUDE_DISPATCHER_ID as the launch saw it
}

// withHandoff gives the verb a product of three repos and a stub launch that
// records what it was asked and hands back a record, or fails with err.
func withHandoff(t *testing.T, err error) *[]handoffCall {
	t.Helper()
	pd, ph := discover, handoff
	t.Cleanup(func() { discover, handoff = pd, ph })
	cfg := &config.Config{Accounts: map[string]string{"work": "/accounts/work"}}
	discover = func() ([]repos.Repo, *config.Config, error) {
		return []repos.Repo{
			{Name: "api", Product: "shop"}, {Name: "app", Product: "shop"}, {Name: "web", Product: "shop"},
			{Name: "blog", Product: "site"},
		}, cfg, nil
	}
	var calls []handoffCall
	handoff = func(from *state.Dispatch, to repos.Repo, title, brief string, m dispatch.Mode, mo dispatch.Model, ro dispatch.Root, a account.Account) (*state.Dispatch, error) {
		calls = append(calls, handoffCall{from.ID, to.Name, title, brief, m, mo, ro, a, os.Getenv("CLAUDE_DISPATCHER_ID")})
		d := &state.Dispatch{ID: "next", Feature: title, RepoName: to.Name, Branch: "feature/" + dispatch.Slugify(title), TmuxSession: "disp-" + dispatch.Slugify(title)}
		return d, err
	}
	return &calls
}

func source() *state.Dispatch {
	return &state.Dispatch{ID: "src", Feature: "orders endpoint", Slug: "orders-endpoint", RepoName: "api", Product: "shop",
		Status: state.StatusWorking, Mode: "manual", Model: "default", Account: "work", ConfigDir: "/accounts/work", SessionID: "sess-1"}
}

// Run from inside a dispatcher's session, `handoff` finds that dispatcher by
// the variable its launch line set, sends the work to the repo named on the
// mode, model and subscription it runs on, and tells the session it is done.
func TestHandoffFromInsideASession(t *testing.T) {
	withStore(t, source())
	calls := withHandoff(t, nil)
	t.Setenv("CLAUDE_DISPATCHER_ID", "src")
	t.Setenv("CLAUDECODE", "1")

	var out bytes.Buffer
	if code := Run("handoff", []string{"--repo", "app", "--title", "show orders", "list", "the", "new", "orders"}, &out, &out); code != 0 {
		t.Fatalf("handoff failed: %s", out.String())
	}
	if len(*calls) != 1 {
		t.Fatalf("want one launch, got %d", len(*calls))
	}
	c := (*calls)[0]
	want := handoffCall{from: "src", to: "app", title: "show orders", brief: "list the new orders",
		mode: "manual", model: "default", root: "", acct: account.Account{Name: "work", Dir: "/accounts/work"}}
	if c != want {
		t.Errorf("the handoff went out as\n%+v\nwant\n%+v", c, want)
	}
	if os.Getenv("CLAUDECODE") != "" {
		t.Error("the session's variables were left for the new pane to inherit")
	}
	for _, line := range []string{`handed off to "show orders" in app · branch feature/show-orders · id next · session disp-show-orders · claude reported in`,
		`"orders endpoint" is finished`, "End your turn here."} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("the output does not say %q:\n%s", line, out.String())
		}
	}
}

// The flags override what the dispatcher runs with, and --from names a
// dispatcher from a plain shell — which is told nothing about ending a turn.
func TestHandoffTakesItsChoicesFromFlags(t *testing.T) {
	withStore(t, source())
	calls := withHandoff(t, nil)
	t.Setenv("CLAUDE_DISPATCHER_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	var out bytes.Buffer
	args := []string{"--from", "src", "--repo", "web", "--title", "show orders", "--root", "develop",
		"--mode", "plan", "--model", "default", "--account", "default", "the same list on the site"}
	if code := Run("handoff", args, &out, &out); code != 0 {
		t.Fatalf("handoff failed: %s", out.String())
	}
	c := (*calls)[0]
	if c.to != "web" || c.mode != dispatch.ModePlan || c.root != "develop" || !c.acct.IsDefault() {
		t.Errorf("the flags were not taken: %+v", c)
	}
	if strings.Contains(out.String(), "End your turn") {
		t.Errorf("a handoff from a shell was told to end a turn:\n%s", out.String())
	}
}

// A session adopted from the human carries no dispatcher id, but its record
// knows its conversation, and Claude Code puts that in every tool's
// environment.
func TestHandoffFindsAnAdoptedSessionByItsConversation(t *testing.T) {
	src := source()
	src.ConfigDir, src.Account = "", ""
	withStore(t, src)
	calls := withHandoff(t, nil)
	t.Setenv("CLAUDE_DISPATCHER_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-1")

	var out bytes.Buffer
	if code := Run("handoff", []string{"--repo", "app", "--title", "show orders", "list them"}, &out, &out); code != 0 {
		t.Fatalf("handoff failed: %s", out.String())
	}
	if c := (*calls)[0]; c.from != "src" || c.acct.Name != account.Default || !c.acct.IsDefault() {
		t.Errorf("the adopted dispatcher was not the one handing off, on its own login: %+v", c)
	}
}

// A brief of "-" is read from stdin, for one too long or too quoted to pass as
// an argument.
func TestHandoffReadsABriefFromStdin(t *testing.T) {
	withStore(t, source())
	calls := withHandoff(t, nil)
	t.Setenv("CLAUDE_DISPATCHER_ID", "src")
	prev := stdin
	stdin = strings.NewReader("  list the orders\nwith 'quotes' and $vars\n")
	t.Cleanup(func() { stdin = prev })

	var out bytes.Buffer
	if code := Run("handoff", []string{"--repo", "app", "--title", "show orders", "-"}, &out, &out); code != 0 {
		t.Fatalf("handoff failed: %s", out.String())
	}
	if got := (*calls)[0].brief; got != "list the orders\nwith 'quotes' and $vars" {
		t.Errorf("brief %q", got)
	}
}

// Every refusal is said before anything is launched.
func TestHandoffRefusals(t *testing.T) {
	withStore(t, source())
	calls := withHandoff(t, nil)
	t.Setenv("CLAUDE_DISPATCHER_ID", "src")
	cases := map[string]struct {
		args []string
		want string
	}{
		"no title":      {[]string{"--repo", "app", "a brief"}, "usage: handoff"},
		"no brief":      {[]string{"--repo", "app", "--title", "x"}, "usage: handoff"},
		"bad flag":      {[]string{"--nope"}, "usage: handoff"},
		"bad mode":      {[]string{"--repo", "app", "--title", "x", "--mode", "yolo", "b"}, `no mode "yolo"`},
		"bad model":     {[]string{"--repo", "app", "--title", "x", "--model", "gpt-9", "b"}, `no model "gpt-9"`},
		"bad account":   {[]string{"--repo", "app", "--title", "x", "--account", "nope", "b"}, `no account "nope"`},
		"other product": {[]string{"--repo", "blog", "--title", "x", "b"}, "blog is not a repo of shop"},
		"own repo":      {[]string{"--repo", "api", "--title", "x", "b"}, "this dispatcher's own repo"},
		"no repo":       {[]string{"--title", "x", "b"}, "name the repo with --repo: app and web"},
		"unknown from":  {[]string{"--from", "nobody", "--repo", "app", "--title", "x", "b"}, "no live dispatcher"},
	}
	for name, c := range cases {
		var out, errOut bytes.Buffer
		if code := Run("handoff", c.args, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), c.want) {
			t.Errorf("%s: exit %d, said %q, want %q", name, code, errOut.String(), c.want)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("a refused handoff launched %d dispatches", len(*calls))
	}

	t.Setenv("CLAUDE_DISPATCHER_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	var errOut bytes.Buffer
	if Run("handoff", []string{"--repo", "app", "--title", "x", "b"}, &errOut, &errOut) == 0 || !strings.Contains(errOut.String(), "not run from a dispatcher's session") {
		t.Errorf("a handoff from nowhere was not refused: %s", errOut.String())
	}
}

// A launch that failed after its record was made says the row is kept and the
// work is still the dispatcher's own.
func TestHandoffSaysWhereAFailedLaunchWent(t *testing.T) {
	withStore(t, source())
	withHandoff(t, errors.New("no server"))
	t.Setenv("CLAUDE_DISPATCHER_ID", "src")
	var out, errOut bytes.Buffer
	if Run("handoff", []string{"--repo", "app", "--title", "show orders", "b"}, &out, &errOut) == 0 {
		t.Fatal("a failed launch reported success")
	}
	for _, want := range []string{"no server", "did not start", `"orders endpoint" is still yours`} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("the failure does not say %q: %s", want, errOut.String())
		}
	}
}

// A config that will not load is said, not read as an empty portfolio.
func TestHandoffSaysWhenTheConfigWillNotLoad(t *testing.T) {
	withStore(t, source())
	withHandoff(t, nil)
	discover = func() ([]repos.Repo, *config.Config, error) { return nil, nil, errors.New("bad toml") }
	t.Setenv("CLAUDE_DISPATCHER_ID", "src")
	var errOut bytes.Buffer
	if Run("handoff", []string{"--repo", "app", "--title", "x", "b"}, &errOut, &errOut) == 0 || !strings.Contains(errOut.String(), "bad toml") {
		t.Errorf("got %q", errOut.String())
	}
}

// `status --json` carries both ends of a handoff, so a session reading the
// fleet can follow a piece of work from repo to repo.
func TestStatusCarriesTheHandoff(t *testing.T) {
	withStore(t,
		&state.Dispatch{ID: "a", Feature: "orders endpoint", Status: state.StatusWorking, HandedOffTo: []string{"b"}},
		&state.Dispatch{ID: "b", Feature: "show orders", Status: state.StatusWorking, HandedOffFrom: "a"})
	var out bytes.Buffer
	if code := Run("status", []string{"--json"}, &out, &out); code != 0 {
		t.Fatal(out.String())
	}
	var es []Entry
	if err := json.Unmarshal(out.Bytes(), &es); err != nil {
		t.Fatal(err)
	}
	by := map[string]Entry{}
	for _, e := range es {
		by[e.ID] = e
	}
	if by["a"].HandedOffTo[0] != "b" || by["b"].HandedOffFrom != "a" {
		t.Errorf("status lost the handoff: %+v / %+v", by["a"], by["b"])
	}
}

// The real discovery reads the config and scans the roots it names, and a
// config that is not there is an error rather than an empty portfolio.
func TestDiscoverReadsTheConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, _, err := discover(); err == nil {
		t.Error("no config read as an empty portfolio")
	}
	root := filepath.Join(home, "src")
	for _, p := range []string{filepath.Join(root, "api", ".git"), config.Dir()} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	toml := "roots = [\"" + root + "\"]\n[products]\nshop = [\"api\"]\n"
	if err := os.WriteFile(config.Path(), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	all, cfg, err := discover()
	if err != nil || cfg == nil || len(all) != 1 || all[0].Product != "shop" {
		t.Errorf("discover: %+v, %v", all, err)
	}
}

// A session reading "handed off" ends its turn on it, so it is said only over a
// claude that reported in. One that died is the start's failure in claude's
// words; one nothing could confirm is named as not a handoff yet. Neither tells
// the session it is finished.
func TestHandoffSaysHandedOffOnlyOverAClaudeThatReportedIn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		wants []string
	}{
		{"died", &dispatch.StartError{Outcome: dispatch.StartFailed,
			Report: "it did not stay up: claude exited before it reported in (status 1): The current directory no longer exists (it was deleted or moved)."},
			[]string{"claude exited before it reported in (status 1): The current directory no longer exists", "did not start", `"orders endpoint" is still yours`}},
		{"unconfirmed", &dispatch.StartError{Outcome: dispatch.StartUnconfirmed,
			Report: "not confirmed: claude has not reported in after 30s — it may be stopped on a startup prompt in session disp-show-orders"},
			[]string{`"show orders" in app is not confirmed`, "disp-show-orders", "not a handoff yet", `"orders endpoint" is still yours`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withStore(t, source())
			withHandoff(t, tc.err)
			t.Setenv("CLAUDE_DISPATCHER_ID", "src")
			var out, errOut bytes.Buffer
			if Run("handoff", []string{"--repo", "app", "--title", "show orders", "b"}, &out, &errOut) == 0 {
				t.Fatal("a start claude never confirmed reported success")
			}
			for _, want := range tc.wants {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("the failure does not say %q: %s", want, errOut.String())
				}
			}
			if strings.Contains(out.String()+errOut.String(), "handed off to") || strings.Contains(out.String(), "End your turn here") {
				t.Errorf("said handed off over a claude that did not report in:\n%s%s", out.String(), errOut.String())
			}
		})
	}
}
