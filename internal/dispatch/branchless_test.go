//go:build !windows

package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// startedAt records where and how a stubbed launch started its session.
type startedAt struct{ dir, cmd, env string }

// stubStart is stubLaunch that also keeps the directory the session was
// started in, which is the half of a branchless launch that matters most.
func stubStart(t *testing.T) *startedAt {
	t.Helper()
	// Nothing a launch writes outside the state dir may reach the human's own
	// home: trust lives in ~/.claude.json.
	t.Setenv("HOME", t.TempDir())
	got := &startedAt{}
	prevNew, prevUniq, prevAlive := newSession, uniqueName, sessionAlive
	newSession = func(_ supervisor.Session, dir, c, env string) error {
		got.dir, got.cmd, got.env = dir, c, env
		return nil
	}
	prevTrust := trustOwnDir
	trustOwnDir = func(string, string) bool { return true }
	uniqueName = func(s supervisor.Session) string { return s.Name }
	sessionAlive = func(supervisor.Session) bool { return false }
	t.Cleanup(func() { newSession, uniqueName, sessionAlive, trustOwnDir = prevNew, prevUniq, prevAlive, prevTrust })
	return got
}

// portfolio is three repos in one product and one in another, as discovery
// would hand them over.
func portfolio(t *testing.T) []repos.Repo {
	t.Helper()
	return []repos.Repo{
		{Name: "playerpulse", Path: initRepo(t), Product: "pp", RootBranch: "dev", Socket: "playerpulse"},
		{Name: "player-app", Path: initRepo(t), Product: "pp", Socket: "player-app", Shell: "/bin/zsh"},
		{Name: "design-system", Path: initRepo(t), Product: "pp"},
		{Name: "cz-bot", Path: initRepo(t), Product: "cz"},
	}
}

// refs is every ref and registered worktree in repo — what a dispatch that
// cuts nothing must leave exactly as it found.
func refs(t *testing.T, repo string) string {
	t.Helper()
	return gitIn(t, repo, "for-each-ref", "--format=%(refname)") + "\n" + gitIn(t, repo, "worktree", "list", "--porcelain")
}

// A scope is a product: the repo it starts on first, then every other repo the
// portfolio files under that product, and nothing from any other.
func TestAScopeIsTheProductItsFocusIsIn(t *testing.T) {
	all := portfolio(t)
	s, err := ScopeOf(all, "", "player-app")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range s.Repos {
		names = append(names, r.Name)
	}
	if got, want := strings.Join(names, " "), "player-app design-system playerpulse"; got != want || s.Product != "pp" || s.Focus != "player-app" {
		t.Errorf("scope = %s (%q, focus %q), want %s in pp, focus first", got, s.Product, s.Focus, want)
	}

	whole, err := ScopeOf(all, "pp", "")
	if err != nil || whole.Focus != "" || len(whole.Repos) != 3 {
		t.Errorf("whole product = %+v, %v; want all three of pp with no focus", whole, err)
	}

	// A repo in no product is a scope of one: there is nothing else it is
	// filed with, and nothing to borrow.
	solo := append(all, repos.Repo{Name: "scratch", Path: t.TempDir()})
	one, err := ScopeOf(solo, "", "scratch")
	if err != nil || len(one.Repos) != 1 || one.Product != "" {
		t.Errorf("unassigned repo = %+v, %v; want itself alone", one, err)
	}

	for _, bad := range [][2]string{{"", ""}, {"", "nope"}, {"empty", ""}} {
		if _, err := ScopeOf(all, bad[0], bad[1]); err == nil {
			t.Errorf("ScopeOf(product %q, focus %q) was not refused", bad[0], bad[1])
		}
	}
}

// The whole of the feature: a dispatch that cuts no branch cuts nothing at
// all — no branch, no worktree, in any repo — and the session it starts can
// reach every repo in the product, in a folder of its own whose CLAUDE.md says
// what each one is and where it lives.
func TestABranchlessDispatchSpansItsProductAndCutsNothing(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	all := portfolio(t)
	before := map[string]string{}
	for _, r := range all {
		before[r.Name] = refs(t, r.Path)
	}
	got := stubStart(t)

	s, _ := ScopeOf(all, "", "player-app")
	d, err := LaunchBranchless(s, "review 163", "review PR 163 and report", ModeAuto, DefaultModel, false, ownAccount)
	if err != nil {
		t.Fatalf("LaunchBranchless: %v", err)
	}
	for _, r := range all {
		if after := refs(t, r.Path); after != before[r.Name] {
			t.Errorf("%s changed:\n%s\n→\n%s", r.Name, before[r.Name], after)
		}
	}

	home := filepath.Join(state.BranchlessDir(), "review-163-"+d.ID)
	if d.WorktreePath != home || got.dir != home {
		t.Errorf("session runs in %q (record %q), want its own folder %q", got.dir, d.WorktreePath, home)
	}
	if !d.Branchless || d.Branch != "" || d.BaseSHA != "" {
		t.Errorf("record: branchless %v, branch %q, base %q — want no branch and nothing to measure from", d.Branchless, d.Branch, d.BaseSHA)
	}
	if d.OwnsWorktree() {
		t.Error("the record claims its folder as a worktree, so kill and tidy would try to remove it")
	}
	if d.RepoName != "player-app" || d.Product != "pp" || d.WholeProduct() {
		t.Errorf("record names %q in %q, want the repo it starts on, in its product", d.RepoName, d.Product)
	}
	want := []string{all[1].Path, all[2].Path, all[0].Path}
	if strings.Join(d.AddDirs, "\n") != strings.Join(want, "\n") {
		t.Errorf("add dirs = %v, want %v", d.AddDirs, want)
	}
	// The flag is variadic, so it must come after the prompt or it would take
	// the prompt for one more directory.
	prompt := strings.Index(got.cmd, `"$(cat `)
	flag := strings.Index(got.cmd, " --add-dir ")
	if prompt < 0 || flag < prompt {
		t.Fatalf("--add-dir is not after the prompt:\n%s", got.cmd)
	}
	for _, p := range want {
		if !strings.Contains(got.cmd[flag:], shellQuote(p)) {
			t.Errorf("the session was not given %s:\n%s", p, got.cmd)
		}
	}
	if !strings.Contains(d.Prompt, BranchlessInstruction) || strings.Contains(d.Prompt, "open the PR") {
		t.Errorf("the prompt was not given the branchless contract:\n%s", d.Prompt)
	}
	// It runs where the repo it starts on runs: that repo's server and shell.
	if d.TmuxSocket != "player-app" || d.PaneShell != "/bin/zsh" {
		t.Errorf("server %q, shell %q; want player-app's", d.TmuxSocket, d.PaneShell)
	}

	brief, err := os.ReadFile(filepath.Join(home, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("no brief: %v", err)
	}
	for _, line := range []string{
		"starts on **player-app**",
		"| playerpulse | `" + all[0].Path + "` | dev |",
		"| player-app | `" + all[1].Path + "` | origin's default branch |",
		"| design-system | `" + all[2].Path + "` |",
		"do not edit, commit, or switch branches",
	} {
		if !strings.Contains(string(brief), line) {
			t.Errorf("brief lacks %q:\n%s", line, brief)
		}
	}
	if strings.Contains(string(brief), "cz-bot") {
		t.Errorf("the brief names a repo from another product:\n%s", brief)
	}
}

// About the product as a whole there is no repo to start on, and so none to
// borrow an environment or a shell from: the session runs in the plain
// environment, and the record names no repo. It still gets a server of its own,
// named for the product the way a repo's sessions are named for the repo. No
// name at all means no -L, and tmux then answers on whichever server $TMUX
// points at — measured running the cockpit inside a tmux of its own, the
// conversation landed on the cockpit's server — so where the session lived
// would depend on where the cockpit was started.
func TestAWholeProductConversationBorrowsNoRepo(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	all := portfolio(t)
	got := stubStart(t)

	s, _ := ScopeOf(all, "pp", "")
	d, err := LaunchBranchless(s, "byga sitrep", "what is the status of byga?", ModeAuto, DefaultModel, false, ownAccount)
	if err != nil {
		t.Fatalf("LaunchBranchless: %v", err)
	}
	if !d.WholeProduct() || d.RepoName != "" || d.RepoPath != "" || d.Product != "pp" {
		t.Errorf("record = repo %q at %q in %q; want the product and no repo", d.RepoName, d.RepoPath, d.Product)
	}
	if d.EnvCommand != "" || got.env != "" || d.PaneShell != "" {
		t.Errorf("borrowed a repo's env %q, shell %q", d.EnvCommand, d.PaneShell)
	}
	if d.TmuxSocket != "pp" {
		t.Errorf("server = %q, want one named for the product", d.TmuxSocket)
	}
	if len(d.AddDirs) != 3 {
		t.Errorf("add dirs = %v, want all three of pp", d.AddDirs)
	}
	brief, _ := os.ReadFile(filepath.Join(d.WorktreePath, "CLAUDE.md"))
	if !strings.Contains(string(brief), "about the product as a whole") || !strings.Contains(string(brief), "the product **pp**") {
		t.Errorf("brief does not say it is about the whole product:\n%s", brief)
	}
}

// One live dispatch per name holds here too: the folder is keyed by the slug,
// exactly as a worktree is, and a second session in it would share a brief and
// a transcript key with the first.
func TestABranchlessDispatchRefusesALiveName(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	all := portfolio(t)
	stubStart(t)
	if err := state.Save(&state.Dispatch{
		ID: state.NewID(), Feature: "sitrep", Slug: "sitrep", Product: "pp", Branchless: true,
		TmuxSession: "disp-sitrep", Status: state.StatusWorking,
	}); err != nil {
		t.Fatal(err)
	}
	sessionAlive = func(supervisor.Session) bool { return true }
	prevIdle := sessionIdle
	sessionIdle = func(supervisor.Session) (bool, bool) { return false, true }
	t.Cleanup(func() { sessionIdle = prevIdle })

	s, _ := ScopeOf(all, "pp", "")
	_, err := LaunchBranchless(s, "sitrep", "again", ModeAuto, DefaultModel, false, ownAccount)
	if err == nil || !strings.Contains(err.Error(), "already live in pp") {
		t.Errorf("a live name was not refused, or the refusal named no place: %v", err)
	}
}

// Reopened, a branchless dispatcher comes back where it ran — its own folder,
// put back if somebody removed it — and with the repos it was given, because
// those are a property of the session and not of the transcript.
func TestResumingABranchlessDispatcherGivesItsReposAgain(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	got := stubStart(t)
	home := filepath.Join(state.BranchlessDir(), "sitrep")
	d := &state.Dispatch{
		ID: state.NewID(), Feature: "sitrep", Slug: "sitrep", Product: "pp", Branchless: true,
		WorktreePath: home, AddDirs: []string{"/src/a", "/src/b c"}, SessionID: "sess-1",
		Mode: string(ModeAuto), TmuxSession: "disp-sitrep", Status: state.StatusExited,
	}
	if err := state.Save(d); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resume(d, "and the tests?"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got.dir != home {
		t.Errorf("resumed in %q, want its own folder %q", got.dir, home)
	}
	if _, err := os.Stat(home); err != nil {
		t.Errorf("the folder was not put back: %v", err)
	}
	if !strings.Contains(got.cmd, "--resume 'sess-1'") || !strings.Contains(got.cmd, " --add-dir '/src/a' '/src/b c'") {
		t.Errorf("resume command lost its repos:\n%s", got.cmd)
	}
	if strings.Index(got.cmd, "--add-dir") < strings.Index(got.cmd, `"$(cat `) {
		t.Errorf("--add-dir comes before the message, which it would swallow:\n%s", got.cmd)
	}
}

// A folder that cannot be put back is said, not resumed into: claude would
// open nowhere, and the human would be handed a session that is not theirs.
func TestResumingABranchlessDispatcherWhoseFolderCannotBeMadeSaysSo(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	stubStart(t)
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	d := &state.Dispatch{ID: state.NewID(), Feature: "sitrep", Slug: "sitrep", Branchless: true,
		WorktreePath: filepath.Join(file, "sitrep"), SessionID: "sess-1", Status: state.StatusExited}
	if _, _, err := Resume(d, ""); err == nil || !strings.Contains(err.Error(), "recreate") {
		t.Errorf("Resume = %v, want it to say the folder could not be recreated", err)
	}
}

// Slugs keep five words, and a conversation is often named as a question, so
// two finished ones collide easily: "what is the status of byga" and "what is
// the status of auth" are both what-is-the-status-of. Keyed by the slug, the
// second rewrote the first's brief, and resuming the first reopened it with
// one product's checkouts and another product's CLAUDE.md. A folder is the
// record's own.
func TestTwoConversationsOfOneSlugKeepTheirOwnBriefs(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	all := portfolio(t)
	stubStart(t)

	pp, _ := ScopeOf(all, "pp", "")
	first, err := LaunchBranchless(pp, "what is the status of byga", "?", ModeAuto, DefaultModel, false, ownAccount)
	if err != nil {
		t.Fatal(err)
	}
	first.Stop(state.StatusExited, "session ended", first.CreatedAt)
	_ = state.Save(first)

	cz, _ := ScopeOf(all, "", "cz-bot")
	second, err := LaunchBranchless(cz, "what is the status of auth", "?", ModeAuto, DefaultModel, false, ownAccount)
	if err != nil {
		t.Fatal(err)
	}
	if first.WorktreePath == second.WorktreePath {
		t.Fatalf("two conversations share %s", first.WorktreePath)
	}
	brief, _ := os.ReadFile(filepath.Join(first.WorktreePath, "CLAUDE.md"))
	if !strings.Contains(string(brief), "the product **pp**") || strings.Contains(string(brief), "cz-bot") {
		t.Errorf("the first conversation's brief was rewritten:\n%s", brief)
	}
}

// A folder somebody removed comes back with its brief, not empty: the contract
// tells the session "the repositories CLAUDE.md lists", and an empty folder
// lists none. The launch keeps a copy beside the prompt for exactly this.
func TestResumingABranchlessDispatcherPutsItsBriefBack(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	all := portfolio(t)
	got := stubStart(t)
	s, _ := ScopeOf(all, "", "player-app")
	d, err := LaunchBranchless(s, "sitrep", "?", ModeAuto, DefaultModel, false, ownAccount)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join(d.WorktreePath, "CLAUDE.md"))
	if err := os.RemoveAll(d.WorktreePath); err != nil {
		t.Fatal(err)
	}
	d.SessionID, d.Status = "sess-1", state.StatusExited
	if _, _, err := Resume(d, ""); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	brief, err := os.ReadFile(filepath.Join(d.WorktreePath, "CLAUDE.md"))
	if err != nil || string(brief) != string(want) || got.dir != d.WorktreePath {
		t.Errorf("resumed in %q with brief %q (%v), want the brief it was launched with", got.dir, brief, err)
	}
}
