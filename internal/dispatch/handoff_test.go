//go:build !windows

package dispatch

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/binpath"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// A dispatch whose repo has others beside it in its product is told it can hand
// work on to them, by name and with the command: a session uses only what it
// has been told about. One alone in its product, or in none, is told nothing.
func TestALaunchIsToldWhereItCanHandOff(t *testing.T) {
	shop := repos.Repo{Name: "api", Product: "shop", Siblings: []string{"app", "web"}}
	got := withHandoff("build the endpoint", shop)
	for _, want := range []string{"build the endpoint\n\n", "one of shop's", "app and web",
		binpath.Self() + " handoff --repo <repo>", "marks this dispatcher finished", "push your own part first"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not carry %q:\n%s", want, got)
		}
	}
	if again := withHandoff(got, shop); again != got {
		t.Errorf("a prompt that already names the handoff gained it twice:\n%s", again)
	}
	for _, alone := range []repos.Repo{{Name: "api"}, {Name: "api", Product: "shop"}} {
		if got := withHandoff("go", alone); got != "go" {
			t.Errorf("a repo with no siblings was told about a handoff: %q", got)
		}
	}
}

func TestAndList(t *testing.T) {
	cases := map[string][]string{"": nil, "a": {"a"}, "a and b": {"a", "b"}, "a, b and c": {"a", "b", "c"}}
	for want, names := range cases {
		if got := andList(names); got != want {
			t.Errorf("andList(%v) = %q, want %q", names, got, want)
		}
	}
}

// The target must be another repo of the dispatcher's own product, and every
// refusal names the repos it could have been.
func TestAHandoffGoesOnlyToAnotherRepoOfTheProduct(t *testing.T) {
	all := []repos.Repo{
		{Name: "api", Product: "shop"}, {Name: "app", Product: "shop"}, {Name: "web", Product: "shop"},
		{Name: "blog", Product: "site"}, {Name: "loner"},
	}
	from := &state.Dispatch{RepoName: "api", Product: "shop"}

	if r, err := HandoffTarget(from, all, "app"); err != nil || r.Name != "app" {
		t.Fatalf("a sibling was refused: %v %v", r.Name, err)
	}
	refusals := map[string]string{
		"":     "name the repo with --repo: app and web",
		"api":  "api is this dispatcher's own repo — a handoff goes to another repo of shop: app and web",
		"blog": "blog is not a repo of shop — hand off to app and web",
		"nope": "nope is not a repo of shop",
	}
	for name, want := range refusals {
		if _, err := HandoffTarget(from, all, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("--repo %q: got %v, want %q", name, err, want)
		}
	}
	if _, err := HandoffTarget(&state.Dispatch{RepoName: "loner"}, all, "api"); err == nil || !strings.Contains(err.Error(), "in no product") {
		t.Errorf("a repo in no product was let hand off: %v", err)
	}
	if _, err := HandoffTarget(&state.Dispatch{RepoName: "blog", Product: "site"}, all, "api"); err == nil || !strings.Contains(err.Error(), "only repo of site") {
		t.Errorf("a repo alone in its product was let hand off: %v", err)
	}
}

// The product is the one config gives the repo now: a repo the human has moved
// since its dispatch launched hands off where they put it. A record whose repo
// is no longer found keeps the product it launched under.
func TestAHandoffReadsTheProductConfigGivesNow(t *testing.T) {
	all := []repos.Repo{{Name: "api", Product: "shop"}, {Name: "app", Product: "shop"}, {Name: "blog", Product: "site"}}
	moved := &state.Dispatch{RepoName: "api", Product: "site"}
	if _, err := HandoffTarget(moved, all, "app"); err != nil {
		t.Errorf("the repo's product now was not the one read: %v", err)
	}
	gone := &state.Dispatch{RepoName: "old-api", Product: "shop"}
	if _, err := HandoffTarget(gone, all, "app"); err != nil {
		t.Errorf("a record whose repo is gone lost its product: %v", err)
	}
}

// handoffFixture is two repos of one product and a dispatcher working in the
// first, saved, with the supervisor stubbed.
func handoffFixture(t *testing.T) (from *state.Dispatch, to repos.Repo) {
	t.Helper()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	stubLaunch(t)
	from = &state.Dispatch{
		ID: state.NewID(), Feature: "orders endpoint", Slug: "orders-endpoint",
		RepoName: "api", RepoPath: initRepo(t), Product: "shop", Branch: "feature/orders-endpoint",
		WorktreePath: "/wt/api/orders-endpoint", PRURL: "https://example.test/api/pull/7",
		Status: state.StatusWorking, TmuxSession: "disp-orders-endpoint", Mode: "auto",
	}
	if err := state.Save(from); err != nil {
		t.Fatal(err)
	}
	to = repos.Repo{Name: "app", Path: initRepo(t), Product: "shop", Siblings: []string{"api"}}
	return from, to
}

func reload(t *testing.T, id string) *state.Dispatch {
	t.Helper()
	for _, d := range state.LoadAll() {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("no record %s", id)
	return nil
}

// The whole of it: a dispatch in the other repo, told where the work it builds
// on lives, linked both ways; and the dispatcher it came from finished — held
// on the table, saying what it handed off to.
func TestAHandoffDispatchesTheWorkAndEndsTheRun(t *testing.T) {
	from, to := handoffFixture(t)
	d, err := Handoff(from, to, "show orders", "list the new orders on the account page", ModeManual, DefaultModel, DefaultRoot, ownAccount)
	if err != nil {
		t.Fatalf("Handoff: %v", err)
	}
	next := reload(t, d.ID)
	if next.RepoName != "app" || next.Feature != "show orders" || next.Mode != string(ModeManual) {
		t.Errorf("the dispatch went out wrong: %s in %s, mode %s", next.Feature, next.RepoName, next.Mode)
	}
	if next.HandedOffFrom != from.ID {
		t.Errorf("the new dispatcher does not say where it came from: %q", next.HandedOffFrom)
	}
	// It can hand on in turn, since api is beside it; manual mode's contract
	// closes the prompt after that sentence.
	for _, want := range []string{"show orders\n\nlist the new orders on the account page\n\n",
		`Handed off by "orders endpoint", a dispatcher in api, branch feature/orders-endpoint, https://example.test/api/pull/7`,
		"Its checkout is /wt/api/orders-endpoint.",
		"handoff --repo",
		"Do one pass, then stop and check in"} {
		if !strings.Contains(next.Prompt, want) {
			t.Errorf("the prompt does not carry %q:\n%s", want, next.Prompt)
		}
	}

	was := reload(t, from.ID)
	if was.Status != state.StatusExited || !was.HandedOff() || !was.Held() {
		t.Errorf("the dispatcher that handed off is %s (%q), held %v", was.Status, was.StatusReason, was.Held())
	}
	if want := ReasonHandedOffTo("show orders", "app"); was.StatusReason != want {
		t.Errorf("reason %q, want %q", was.StatusReason, want)
	}
	if len(was.HandedOffTo) != 1 || was.HandedOffTo[0] != d.ID {
		t.Errorf("the dispatcher that handed off does not name its successor: %v", was.HandedOffTo)
	}

	// A second handoff from the same run — the change has a second consumer —
	// adds to the list and leaves the ending as the first one wrote it.
	d2, err := Handoff(was, repos.Repo{Name: "web", Path: initRepo(t), Product: "shop"}, "show orders on web", "the same list on the site", ModeAuto, DefaultModel, DefaultRoot, ownAccount)
	if err != nil {
		t.Fatalf("second Handoff: %v", err)
	}
	again := reload(t, from.ID)
	if len(again.HandedOffTo) != 2 || again.HandedOffTo[1] != d2.ID || again.StatusReason != was.StatusReason {
		t.Errorf("a second handoff did not add to the first: %v %q", again.HandedOffTo, again.StatusReason)
	}
}

// A dispatcher that is already live keeps done: a handoff takes nothing back.
func TestAHandoffFromALiveDispatcherKeepsItLive(t *testing.T) {
	from, to := handoffFixture(t)
	now := time.Now()
	from.DeployedAt = &now
	if err := state.Save(from); err != nil {
		t.Fatal(err)
	}
	if _, err := Handoff(from, to, "show orders", "list them", ModeAuto, DefaultModel, DefaultRoot, ownAccount); err != nil {
		t.Fatal(err)
	}
	if was := reload(t, from.ID); was.Status != state.StatusDone || !was.HandedOff() {
		t.Errorf("a deployed dispatcher was handed off as %s (%q)", was.Status, was.StatusReason)
	}
}

// A handoff that did not happen leaves the work with the dispatcher that has
// it. The launch that failed is still kept (ADR 0009), saying where it came
// from.
func TestAFailedHandoffLeavesTheRunGoing(t *testing.T) {
	from, to := handoffFixture(t)
	newSession = func(supervisor.Session, string, string, string) error { return errors.New("no server") }
	d, err := Handoff(from, to, "show orders", "list them", ModeAuto, DefaultModel, DefaultRoot, ownAccount)
	if err == nil || d == nil {
		t.Fatalf("want the launch's failure and its record, got %v, %v", d, err)
	}
	if was := reload(t, from.ID); was.Status != state.StatusWorking || len(was.HandedOffTo) != 0 {
		t.Errorf("a failed handoff ended the run: %s %v", was.Status, was.HandedOffTo)
	}
	if failed := reload(t, d.ID); failed.HandedOffFrom != from.ID || failed.Status != state.StatusExited {
		t.Errorf("the failed launch is %s and says it came from %q", failed.Status, failed.HandedOffFrom)
	}
}

// What a handoff refuses before anything is created.
func TestAHandoffRefusesWhatItCannotSend(t *testing.T) {
	from, to := handoffFixture(t)
	cases := map[string][2]string{
		"no title":      {"  ", "a brief"},
		"no brief":      {"show orders", " "},
		"its own name":  {"Orders Endpoint", "a brief"},
		"no dispatcher": {"show orders", "a brief"},
	}
	for name, c := range cases {
		src := from
		if name == "no dispatcher" {
			src = nil
		}
		if d, err := Handoff(src, to, c[0], c[1], ModeAuto, DefaultModel, DefaultRoot, ownAccount); err == nil || d != nil {
			t.Errorf("%s: a handoff went out: %v", name, err)
		}
	}
	if len(state.LoadAll()) != 1 {
		t.Errorf("a refused handoff created a record: %d records", len(state.LoadAll()))
	}
	if was := reload(t, from.ID); was.Status != state.StatusWorking {
		t.Errorf("a refused handoff ended the run: %s", was.Status)
	}
}

// A link whose records are not both there says so rather than half-writing it.
func TestALinkWithAMissingEndSaysSo(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if err := linkHandoff("nope", "nada", true, "x"); err == nil {
		t.Error("linking two records that do not exist reported success")
	}
}

// The new session must not start as the old one's child: a pane takes its
// environment from the client that asked for it, and the client is this
// process, running inside the session that is handing off.
func TestScrubSessionEnvLeavesTheStore(t *testing.T) {
	for _, v := range []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_MESSAGING_SOCKET",
		"CLAUDE_DISPATCHER_ID", "CLAUDE_CONFIG_DIR", "CLAUDE_PID", "CLAUDE_EFFORT", "AI_AGENT"} {
		t.Setenv(v, "x")
	}
	t.Setenv("CLAUDE_DISPATCHER_STATE", "/store")
	ScrubSessionEnv()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if sessionVar(name) {
			t.Errorf("%s survived the scrub", name)
		}
	}
	if os.Getenv("CLAUDE_DISPATCHER_STATE") != "/store" {
		t.Error("the scrub took the store with it")
	}
}

// The account a handoff is given is the one the new session runs under.
func TestAHandoffRunsOnTheAccountItIsGiven(t *testing.T) {
	from, to := handoffFixture(t)
	prev := probeAccount
	probeAccount = func(account.Account) account.Auth { return account.Auth{} }
	t.Cleanup(func() { probeAccount = prev })
	var got account.Account
	prevLaunch := launchHandoff
	launchHandoff = func(r repos.Repo, f, p string, m Mode, mo Model, ro Root, fan bool, a account.Account) (*state.Dispatch, error) {
		got = a
		return Launch(r, f, p, m, mo, ro, fan, ownAccount)
	}
	t.Cleanup(func() { launchHandoff = prevLaunch })
	work := account.Account{Name: "work", Dir: "/accounts/work"}
	if _, err := Handoff(from, to, "show orders", "list them", ModeAuto, DefaultModel, DefaultRoot, work); err != nil {
		t.Fatal(err)
	}
	if got != work {
		t.Errorf("the launch ran on %+v, want %+v", got, work)
	}
}
