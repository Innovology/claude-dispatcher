//go:build !windows

package fleetcmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// `dispatch` from inside a conversation sends a dispatcher of its own: on the
// conversation's subscription, recorded as sent by it, and launched with
// nothing of the conversation's session left in its environment — a pane takes
// its environment from whoever asks for it.
func TestDispatchSendsADispatcherOfTheConversationsOwn(t *testing.T) {
	portfolio(t)
	withStore(t, &state.Dispatch{ID: "c1", Feature: "talk", Product: "pp", Branchless: true, Account: "work",
		Status: state.StatusWorking})
	t.Setenv("CLAUDE_DISPATCHER_ID", "c1")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "parent-session")

	var asked struct {
		repo, feature, prompt string
		root                  dispatch.Root
		acct                  account.Account
		env                   []string
	}
	prev := launch
	launch = func(r repos.Repo, feature, prompt string, _ dispatch.Mode, _ dispatch.Model, root dispatch.Root, _ bool, acct account.Account) (*state.Dispatch, error) {
		asked.repo, asked.feature, asked.prompt, asked.root, asked.acct = r.Name, feature, prompt, root, acct
		asked.env = os.Environ()
		d := &state.Dispatch{ID: "w1", Feature: feature, RepoName: r.Name, Branch: "feature/sync", TmuxSession: "disp-sync"}
		return d, state.Save(d)
	}
	t.Cleanup(func() { launch = prev })

	var out bytes.Buffer
	if err := runDispatch([]string{"--repo", "api", "--title", "sync", "--root", "dev", "rework", "the", "sync"}, &out); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if asked.repo != "api" || asked.feature != "sync" || asked.prompt != "sync\n\nrework the sync" || asked.root != "dev" {
		t.Errorf("launched %+v", asked)
	}
	if asked.acct.Name != "work" {
		t.Errorf("the sent dispatcher runs on %q, want the conversation's account", asked.acct.Name)
	}
	for _, kv := range asked.env {
		if strings.HasPrefix(kv, "CLAUDECODE=") || strings.HasPrefix(kv, "CLAUDE_CODE_SESSION_ID=") || strings.HasPrefix(kv, "CLAUDE_DISPATCHER_ID=") {
			t.Errorf("the sent dispatcher was launched carrying the conversation's %s", kv)
		}
	}
	if w, _ := find("w1"); w == nil || w.DispatchedBy != "c1" {
		t.Errorf("sent = %+v, want it recorded as sent by c1", w)
	}
	if !strings.Contains(out.String(), `dispatched "sync" → api on feature/sync · id w1`) {
		t.Errorf("output: %s", out.String())
	}

	if err := runDispatch([]string{"--repo", "api", "--title", "sync"}, &out); err == nil {
		t.Error("a title with no brief was dispatched")
	}
	if err := runDispatch([]string{"--repo", "nope", "--title", "x", "y"}, &out); err == nil {
		t.Error("a repo that is not there was dispatched into")
	}
}

// stubLaunch records each launch `dispatch` asks for and saves a record for it.
func stubLaunch(t *testing.T) *[]string {
	t.Helper()
	var launched []string
	prev := launch
	launch = func(r repos.Repo, feature, _ string, _ dispatch.Mode, _ dispatch.Model, _ dispatch.Root, _ bool, _ account.Account) (*state.Dispatch, error) {
		launched = append(launched, r.Name+":"+feature)
		d := &state.Dispatch{ID: "w-" + dispatch.Slugify(feature), Feature: feature, RepoName: r.Name, Status: state.StatusLaunching}
		return d, state.Save(d)
	}
	t.Cleanup(func() { launch = prev })
	return &launched
}

// Sent from a dispatcher's session, work goes to a repo of the product it was
// dispatched over — the repos `worktree` would let it take a branch in — and
// a session whose record is gone sends nothing. From a plain shell, any repo.
func TestDispatchStaysInTheSendersProduct(t *testing.T) {
	portfolio(t)
	withStore(t, &state.Dispatch{ID: "c1", Feature: "talk", Product: "pp", Branchless: true, Status: state.StatusWorking})
	launched := stubLaunch(t)
	var out bytes.Buffer

	t.Setenv("CLAUDE_DISPATCHER_ID", "c1")
	if err := runDispatch([]string{"--repo", "other", "--title", "x", "y"}, &out); err == nil || !strings.Contains(err.Error(), "api, app") {
		t.Errorf("sent work outside its product: %v", err)
	}
	t.Setenv("CLAUDE_DISPATCHER_ID", "gone")
	if err := runDispatch([]string{"--repo", "api", "--title", "x", "y"}, &out); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("a session with no record sent work: %v", err)
	}
	t.Setenv("CLAUDE_DISPATCHER_ID", "")
	if err := runDispatch([]string{"--repo", "other", "--title", "x", "y"}, &out); err != nil {
		t.Errorf("from a plain shell: %v", err)
	}
	if strings.Join(*launched, ",") != "other:x" {
		t.Errorf("launched %v; want only the plain shell's", *launched)
	}
}

// A mode that is not one is refused rather than read as auto: a typo for
// manual would otherwise send the work out unattended.
func TestDispatchRefusesAModeThatIsNotOne(t *testing.T) {
	portfolio(t)
	withStore(t)
	launched := stubLaunch(t)
	t.Setenv("CLAUDE_DISPATCHER_ID", "")
	var out bytes.Buffer
	if err := runDispatch([]string{"--repo", "api", "--mode", "manaul", "--title", "x", "y"}, &out); err == nil || !strings.Contains(err.Error(), "manaul") {
		t.Errorf("--mode manaul: %v", err)
	}
	if err := runDispatch([]string{"--repo", "api", "--mode", "plan", "--title", "x", "y"}, &out); err != nil {
		t.Errorf("--mode plan: %v", err)
	}
	if len(*launched) != 1 {
		t.Errorf("launched %v; want only the plan one", *launched)
	}
}

// A sender whose account config no longer names sends on the directory its
// record says it runs under, and says so, rather than quietly moving the work
// onto the human's own login; with no directory either, it sends nothing.
func TestDispatchKeepsTheSendersAccountAndSaysWhenConfigLostIt(t *testing.T) {
	portfolio(t)
	withStore(t,
		&state.Dispatch{ID: "c1", Feature: "talk", Product: "pp", Account: "gone", ConfigDir: "/cfg/gone", Status: state.StatusWorking},
		&state.Dispatch{ID: "c2", Feature: "lost", Product: "pp", Account: "gone", Status: state.StatusWorking})
	var acct account.Account
	prev := launch
	launch = func(r repos.Repo, feature, _ string, _ dispatch.Mode, _ dispatch.Model, _ dispatch.Root, _ bool, a account.Account) (*state.Dispatch, error) {
		acct = a
		d := &state.Dispatch{ID: "w1", Feature: feature, RepoName: r.Name}
		return d, state.Save(d)
	}
	t.Cleanup(func() { launch = prev })

	t.Setenv("CLAUDE_DISPATCHER_ID", "c1")
	var out bytes.Buffer
	if err := runDispatch([]string{"--repo", "api", "--title", "x", "y"}, &out); err != nil {
		t.Fatal(err)
	}
	if acct.Name != "gone" || acct.Dir != "/cfg/gone" || !strings.Contains(out.String(), "not in config.toml") {
		t.Errorf("sent on %+v, said:\n%s", acct, out.String())
	}
	t.Setenv("CLAUDE_DISPATCHER_ID", "c2")
	if err := runDispatch([]string{"--repo", "api", "--title", "z", "y"}, &out); err == nil || !strings.Contains(err.Error(), "no longer has") {
		t.Errorf("a sender with no account to send on: %v", err)
	}
}

// A dispatcher has at most MaxSentOut dispatchers out at once; one that
// finished frees its place, and a plain shell is not counted against anyone.
func TestDispatchBoundsWhatOneDispatcherHasOut(t *testing.T) {
	portfolio(t)
	recs := []*state.Dispatch{{ID: "c1", Feature: "talk", Product: "pp", Status: state.StatusWorking}}
	for i := 0; i < MaxSentOut; i++ {
		recs = append(recs, &state.Dispatch{ID: fmt.Sprintf("s%d", i), Feature: fmt.Sprintf("sent %d", i),
			DispatchedBy: "c1", Status: state.StatusWorking})
	}
	withStore(t, recs...)
	launched := stubLaunch(t)
	t.Setenv("CLAUDE_DISPATCHER_ID", "c1")
	var out bytes.Buffer
	if err := runDispatch([]string{"--repo", "api", "--title", "one more", "y"}, &out); err == nil || !strings.Contains(err.Error(), "already has 8") {
		t.Errorf("past the bound: %v", err)
	}
	recs[1].Stop(state.StatusExited, "done", time.Now())
	if err := state.Save(recs[1]); err != nil {
		t.Fatal(err)
	}
	if err := runDispatch([]string{"--repo", "api", "--title", "one more", "y"}, &out); err != nil {
		t.Errorf("with a place freed: %v", err)
	}
	if len(*launched) != 1 {
		t.Errorf("launched %v", *launched)
	}
}
