package cockpit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/repos"
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

// The forge warm-up asks for PR detail only where the open search found some —
// once per discovered repo that has open PRs, from that repo's own directory —
// and asks nothing per repo while gh is refusing us: a parked client spends no
// calls, and a repo the search names that this machine has not discovered is
// nothing to warm.
func TestTheForgeWarmUpAsksOnlyReposWithOpenPRs(t *testing.T) {
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	script := "#!/bin/sh\necho \"$PWD|$*\" >> " + calls + "\n" +
		"case \"$*\" in\n" +
		"  \"search prs --state open\"*) echo '[{\"repository\":{\"name\":\"shop\"},\"number\":7,\"title\":\"t\",\"author\":{\"login\":\"me\"},\"createdAt\":\"2026-10-01T00:00:00Z\"}," +
		"{\"repository\":{\"name\":\"elsewhere\"},\"number\":2,\"title\":\"t\",\"author\":{\"login\":\"me\"},\"createdAt\":\"2026-10-01T00:00:00Z\"}]' ;;\n" +
		"  *) echo '[]' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	gh.InvalidateCache()
	gh.ResetThrottle()
	t.Cleanup(func() { gh.InvalidateCache(); gh.ResetThrottle() })

	shop, quiet := t.TempDir(), t.TempDir()
	disc := map[string]repos.Repo{"shop": {Name: "shop", Path: shop}, "quiet": {Name: "quiet", Path: quiet}}
	read := func() []string {
		b, _ := os.ReadFile(calls)
		_ = os.Remove(calls)
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}

	open, _ := warmProductForge(&collectCtx{cfg: &config.Config{}}, disc, true)
	if len(open["shop"]) != 1 || open["shop"][0].Number != 7 {
		t.Fatalf("open PRs = %+v, want shop's #7 handed back", open)
	}
	var detail []string
	for _, c := range read() {
		if strings.Contains(c, "|pr list") {
			detail = append(detail, c)
		}
	}
	if len(detail) != 1 || !strings.HasPrefix(detail[0], shop+"|") {
		t.Errorf("PR detail was asked %v, want once, from shop's checkout", detail)
	}

	gh.InvalidateCache()
	warmProductForge(&collectCtx{cfg: &config.Config{}}, disc, false)
	down := read()
	if len(down) == 0 || down[0] == "" {
		t.Fatal("gh down asked nothing at all — the searches still go out")
	}
	for _, c := range down {
		if !strings.Contains(c, "|search prs") {
			t.Errorf("gh down, yet a per-repo read went out: %s", c)
		}
	}
}
