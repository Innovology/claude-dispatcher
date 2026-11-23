//go:build !windows

package fleetcmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// `dispatch` from inside a conversation sends a worker of its own: on the
// conversation's subscription, recorded as sent by it, and launched with
// nothing of the conversation's session left in its environment — a pane takes
// its environment from whoever asks for it.
func TestDispatchSendsAWorkerOfTheConversationsOwn(t *testing.T) {
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
		t.Errorf("worker runs on %q, want the conversation's account", asked.acct.Name)
	}
	for _, kv := range asked.env {
		if strings.HasPrefix(kv, "CLAUDECODE=") || strings.HasPrefix(kv, "CLAUDE_CODE_SESSION_ID=") || strings.HasPrefix(kv, "CLAUDE_DISPATCHER_ID=") {
			t.Errorf("the worker was launched carrying the conversation's %s", kv)
		}
	}
	if w, _ := find("w1"); w == nil || w.DispatchedBy != "c1" {
		t.Errorf("worker = %+v, want it recorded as sent by c1", w)
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
