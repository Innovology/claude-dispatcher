//go:build !windows

package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

// tidyFixture is a repo that ignores node_modules, and a way to add a
// dispatcher's worktree of it with a record naming it.
type tidyFixture struct {
	t    *testing.T
	repo string
	root string // where the worktrees live
}

func newTidyFixture(t *testing.T) *tidyFixture {
	t.Helper()
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	withFakeSessions(t, map[string]bool{}, true)
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".gitignore")
	gitIn(t, repo, "commit", "-m", "ignore deps")
	return &tidyFixture{t: t, repo: repo, root: t.TempDir()}
}

// dispatcher adds a worktree with an installed node_modules and saves a record
// for it in the given status.
func (f *tidyFixture) dispatcher(slug string, status state.Status) (*state.Dispatch, string) {
	f.t.Helper()
	wt := filepath.Join(f.root, slug)
	if _, err := ensureWorktree(f.repo, wt, "feature/"+slug, DefaultRoot); err != nil {
		f.t.Fatal(err)
	}
	writeFile(f.t, filepath.Join(wt, "node_modules", "left-pad", "index.js"), "x")
	d := &state.Dispatch{
		ID: "id-" + slug, Feature: slug, Slug: slug, RepoPath: f.repo, RepoName: "shop",
		Branch: "feature/" + slug, WorktreePath: wt, TmuxSession: "disp-" + slug,
		Status: status, CreatedAt: time.Now(),
	}
	saveAll(f.t, d)
	return d, wt
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func planFor(items []TidyItem, path string) *TidyItem {
	for i := range items {
		if items[i].Path == path {
			return &items[i]
		}
	}
	return nil
}

// The case the whole thing is for: a dispatcher that ended on its own left a
// clean checkout and its dependencies behind. The tidy takes both, and keeps the
// branch — which is what lets a resume put the checkout back.
func TestTidyRemovesAFinishedCleanWorktreeWithItsNodeModules(t *testing.T) {
	f := newTidyFixture(t)
	_, wt := f.dispatcher("done-one", state.StatusDone)

	plan := PlanTidy(state.LoadAll())
	it := planFor(plan, wt)
	if it == nil || it.Action != TidyRemove {
		t.Fatalf("plan = %+v, want remove", it)
	}
	if !exists(wt) {
		t.Fatal("planning must not touch the disk")
	}

	got := Tidy(plan)
	if it := planFor(got, wt); it == nil || it.Action != TidyRemove || it.Err != "" {
		t.Fatalf("tidy = %+v, want removed", it)
	}
	if exists(wt) {
		t.Fatal("worktree still on disk")
	}
	if out := gitIn(t, f.repo, "branch", "--list", "feature/done-one"); out == "" {
		t.Fatal("the branch must outlive its worktree")
	}
	if out := gitIn(t, f.repo, "worktree", "list"); strings.Contains(out, wt) {
		t.Fatalf("git still lists the worktree:\n%s", out)
	}
	var audited bool
	for _, ev := range state.LoadEvents() {
		if ev.Event == state.EventTidied && ev.Cwd == wt && ev.DispatcherID == "id-done-one" {
			audited = true
		}
	}
	if !audited {
		t.Error("a removal must be written to the event log")
	}
}

// Uncommitted work is never taken. What is taken is what an install rebuilds:
// every ignored node_modules, a workspace's as well as the root's — and not a
// node_modules folder the repo actually tracks files in.
func TestTidyStripsNodeModulesFromADirtyWorktreeAndKeepsTheWork(t *testing.T) {
	f := newTidyFixture(t)
	_, wt := f.dispatcher("dirty", state.StatusExited)
	writeFile(t, filepath.Join(wt, "packages", "web", "node_modules", "react", "index.js"), "x")
	// A vendored folder the repo tracks despite the name — force-added past the
	// ignore rule, as some repos do.
	writeFile(t, filepath.Join(wt, "vendor", "node_modules", "patched.js"), "x")
	gitIn(t, wt, "add", "-f", "vendor/node_modules/patched.js")
	gitIn(t, wt, "commit", "-m", "vendor a patched dep")
	writeFile(t, filepath.Join(wt, "wip.txt"), "unfinished")

	plan := PlanTidy(state.LoadAll())
	it := planFor(plan, wt)
	if it == nil || it.Action != TidyStrip {
		t.Fatalf("plan = %+v, want strip", it)
	}
	want := map[string]bool{"node_modules": true, filepath.Join("packages", "web", "node_modules"): true}
	if len(it.NodeModules) != len(want) {
		t.Fatalf("node_modules = %v, want %v", it.NodeModules, want)
	}
	for _, rel := range it.NodeModules {
		if !want[rel] {
			t.Fatalf("would strip %s, which it must not", rel)
		}
	}

	Tidy(plan)
	if !exists(filepath.Join(wt, "wip.txt")) {
		t.Fatal("uncommitted work was removed")
	}
	if exists(filepath.Join(wt, "node_modules")) || exists(filepath.Join(wt, "packages", "web", "node_modules")) {
		t.Fatal("ignored node_modules survived the strip")
	}
	if !exists(filepath.Join(wt, "vendor", "node_modules", "patched.js")) {
		t.Fatal("a tracked node_modules was removed")
	}
}

// Every reason a folder is still somebody's keeps it, untouched.
func TestTidyKeepsWhatIsStillInUse(t *testing.T) {
	f := newTidyFixture(t)
	_, working := f.dispatcher("working", state.StatusWorking)
	_, open := f.dispatcher("open", state.StatusDone)
	parkedRec, parked := f.dispatcher("parked", state.StatusExited)
	parkedRec.ParkedReason = "waiting on legal"
	saveAll(t, parkedRec)
	// done is the tracker's word and says nothing about the session, whose
	// claude may still be in there.
	withFakeSessions(t, map[string]bool{"disp-open": true}, true)

	plan := PlanTidy(state.LoadAll())
	for path, reason := range map[string]string{
		working: "still working",
		open:    "its session is still open",
		parked:  "parked",
	} {
		it := planFor(plan, path)
		if it == nil || it.Action != TidyKeep || it.Reason != reason {
			t.Errorf("%s: plan = %+v, want keep (%s)", filepath.Base(path), it, reason)
		}
	}
	Tidy(plan)
	for _, p := range []string{working, open, parked} {
		if !exists(filepath.Join(p, "node_modules")) {
			t.Errorf("%s was touched", filepath.Base(p))
		}
	}
}

// Without a supervisor to ask, an open session cannot be ruled out, and an open
// session is somebody working in the folder.
func TestTidyKeepsWhatItCannotAskAbout(t *testing.T) {
	f := newTidyFixture(t)
	_, wt := f.dispatcher("unknown", state.StatusDone)
	withFakeSessions(t, map[string]bool{}, false)
	if it := planFor(PlanTidy(state.LoadAll()), wt); it == nil || it.Action != TidyKeep {
		t.Fatalf("plan = %+v, want keep", it)
	}
}

// A finished feature dispatched again shares its folder with the new run, so
// the older record's ending does not free it.
func TestTidyKeepsAFolderAnyRecordStillClaims(t *testing.T) {
	f := newTidyFixture(t)
	_, wt := f.dispatcher("again", state.StatusDone)
	saveAll(t, &state.Dispatch{
		ID: "id-again-2", Feature: "again", Slug: "again", RepoPath: f.repo, RepoName: "shop",
		Branch: "feature/again", WorktreePath: wt, TmuxSession: "disp-again-2",
		Status: state.StatusWorking, CreatedAt: time.Now().Add(time.Minute),
	})
	withFakeSessions(t, map[string]bool{"disp-again-2": true}, true)
	plan := PlanTidy(state.LoadAll())
	if len(plan) != 1 {
		t.Fatalf("one folder named twice is one item, got %d", len(plan))
	}
	if it := plan[0]; it.Action != TidyKeep || it.ID != "id-again-2" || len(it.Records) != 2 {
		t.Fatalf("plan = %+v, want keep, credited to the newest record", it)
	}
}

// The worktrees folder holds checkouts no record names — a full clone, a
// worktree a session made for itself. They are not ours and are never listed.
func TestTidyLeavesFoldersNoRecordNames(t *testing.T) {
	f := newTidyFixture(t)
	stray := filepath.Join(f.root, "made-by-hand")
	if _, err := ensureWorktree(f.repo, stray, "feature/by-hand", DefaultRoot); err != nil {
		t.Fatal(err)
	}
	if plan := PlanTidy(state.LoadAll()); len(plan) != 0 {
		t.Fatalf("plan = %+v, want nothing", plan)
	}
}

// A detached HEAD whose commits no ref holds is held only by the worktree.
func TestTidyKeepsCommitsOnNoBranch(t *testing.T) {
	f := newTidyFixture(t)
	_, wt := f.dispatcher("detached", state.StatusExited)
	gitIn(t, wt, "checkout", "--detach")
	gitIn(t, wt, "commit", "--allow-empty", "-m", "orphaned work")
	it := planFor(PlanTidy(state.LoadAll()), wt)
	if it == nil || it.Action != TidyKeep || !strings.Contains(it.Reason, "detached") {
		t.Fatalf("plan = %+v, want keep for the detached commits", it)
	}
	// Once a branch holds them, the worktree is just a checkout again.
	gitIn(t, wt, "branch", "rescued")
	if it := planFor(PlanTidy(state.LoadAll()), wt); it == nil || it.Action != TidyRemove {
		t.Fatalf("plan = %+v, want remove once a branch holds the commits", it)
	}
}

// A plan can sit on a confirm bar. Anything that has changed by the time it is
// carried out is re-read, not acted on from the old answer.
func TestTidyRechecksBeforeActing(t *testing.T) {
	f := newTidyFixture(t)
	rec, wt := f.dispatcher("relaunched", state.StatusDone)
	plan := PlanTidy(state.LoadAll())

	rec.Status = state.StatusWorking // dispatched again while the plan waited
	saveAll(t, rec)
	got := Tidy(plan)
	if it := planFor(got, wt); it == nil || it.Action != TidyKeep {
		t.Fatalf("tidy = %+v, want keep", it)
	}
	if !exists(wt) {
		t.Fatal("a worktree back in use was removed")
	}
}
