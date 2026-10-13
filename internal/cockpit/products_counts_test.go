package cockpit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// WANTS YOU counts what triage counts as wanting the human, once each. It
// counted every open PR: a product with two waiting dispatchers read 5, the
// other three being two still working on their PRs and an exited test whose PR
// was left open.
func TestWantsYouIsWhatTriageSaysWantsYou(t *testing.T) {
	now := time.Now()
	waited := now.Add(-time.Hour)
	rec := func(id string, st state.Status, pr string) *state.Dispatch {
		d := &state.Dispatch{ID: id, Feature: id, RepoName: "acme-hq", Product: "acme", Status: st,
			CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now}
		if pr != "" {
			d.PRNumber, d.PRState = 1, pr
		}
		return d
	}
	asking := rec("asking", state.StatusNeedsInput, "")
	asking.WaitingSince = &waited
	parked := rec("parked", state.StatusNeedsInput, "")
	parked.ParkedAt = &now
	recs := []*state.Dispatch{
		asking,
		rec("menu", state.StatusBlocked, ""),
		rec("merge-me", state.StatusNeedsInput, "OPEN"), // stopped on an open PR: review
		rec("busy-on-pr", state.StatusWorking, "OPEN"),
		rec("left-open", state.StatusExited, "OPEN"),
		parked,
	}

	var s snapshot
	collectProducts(&collectCtx{cfg: &config.Config{Products: map[string][]string{"acme": {"acme-hq"}}}, records: recs}, &s)
	for _, p := range s.products {
		if p.name != "acme" {
			continue
		}
		if p.needs != 2 || p.review != 1 {
			t.Errorf("needs %d review %d, want 2 and 1 — the working and exited PRs are not the human's", p.needs, p.review)
		}
		if p.inflight != 5 {
			t.Errorf("in flight %d, want 5", p.inflight)
		}
		return
	}
	t.Fatal("no acme product collected")
}

// The panel's in-flight list is the floor's rows for the product, each once,
// and none of them live.
func TestThePanelListsEachDispatcherInFlightOnce(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	dispatches = []dispatch{
		{feature: "explore", repo: "acme-hq", product: "acme", state: "working", age: "now"},
		{feature: "asking", repo: "acme-hq", product: "acme", state: "needs", age: "4m"},
		{feature: "shipped", repo: "acme-hq", product: "acme", state: "live", age: "2d"},
		{feature: "elsewhere", repo: "other", product: "bluefin", state: "working", age: "1h"},
	}
	working = []workingItem{{feature: "explore", repo: "acme-hq", product: "acme", age: "5h"}}

	got := productsFocusFeatures("acme")
	if len(got) != 2 || got[0].feature != "explore" || got[0].age != "now" || got[1].feature != "asking" {
		t.Errorf("in flight = %+v, want explore (now) and asking, once each", got)
	}
}

// A repo is untouched only if none of its branches has moved: the checkout a
// row reads can be one nobody works in.
func TestUntouchedReadsEveryBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t"), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	old := []string{"GIT_AUTHOR_DATE=2020-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2020-01-01T00:00:00Z"}
	git(nil, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(nil, "add", "f")
	git(old, "commit", "-q", "-m", "long ago")
	if days, ok := stqDaysSinceCommit(dir); !ok || days < 365 {
		t.Fatalf("an old repo reads %d days (%v)", days, ok)
	}
	git(nil, "checkout", "-q", "-b", "feature")
	git(nil, "commit", "-q", "--allow-empty", "-m", "today, elsewhere", "--no-verify")
	git(nil, "checkout", "-q", "main") // the checkout the row reads stays on the old commit
	if days, ok := stqDaysSinceCommit(dir); !ok || days != 0 {
		t.Errorf("a branch committed today leaves the repo reading %d days untouched (%v)", days, ok)
	}
}
