package cockpit

// actions_test.go covers actions.go's guard/degrade branches: every action
// resolves the selected feature to a live record via recordFor, and
// degrades to an honest notice when there is no record or no live tmux
// session — the paths exercised here never touch a real tmux session or
// reach the network, since liveRecords always names a session that does not
// exist and the fake repos below carry no git remote.

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

func TestFirstLine(t *testing.T) {
	if got := firstLine("one\ntwo\nthree"); got != "one" {
		t.Errorf("firstLine multiline = %q, want %q", got, "one")
	}
	if got := firstLine("solo"); got != "solo" {
		t.Errorf("firstLine single = %q, want %q", got, "solo")
	}
	if got := firstLine(""); got != "" {
		t.Errorf("firstLine empty = %q", got)
	}
}

// withFakeRecord installs a single fake record under feature in liveRecords,
// restoring the previous global state afterwards.
func withFakeRecord(t *testing.T, feature string, rec *state.Dispatch) {
	t.Helper()
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	liveRecords = map[string]*state.Dispatch{feature: rec}
}

func TestAttachNoRecord(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	liveRecords = map[string]*state.Dispatch{}

	m := newModel()
	mm, cmd := m.attach("ghost feature")
	if cmd != nil {
		t.Error("attach with no record should not return a tea.Cmd")
	}
	if !strings.Contains(mm.notice, "no live session") {
		t.Errorf("notice = %q", mm.notice)
	}
}

func TestAttachNoTmuxSession(t *testing.T) {
	withFakeRecord(t, "phantom", &state.Dispatch{
		Feature: "phantom", TmuxSession: "cockpit-test-does-not-exist-8231",
	})
	m := newModel()
	mm, cmd := m.attach("phantom")
	if cmd != nil {
		t.Error("attach with a dead session should not return a tea.Cmd")
	}
	if !strings.Contains(mm.notice, "no live tmux session") {
		t.Errorf("notice = %q", mm.notice)
	}
}

// withFakeSweep swaps the stray-session sweep for one that answers from the
// test, so these cases do not need a real tmux server (CI has none) and can
// still say what the sweep found.
func withFakeSweep(t *testing.T, retired, live int) *int {
	t.Helper()
	prev := reconcileSessions
	t.Cleanup(func() { reconcileSessions = prev })
	calls := 0
	reconcileSessions = func([]*state.Dispatch) (int, int) {
		calls++
		return retired, live
	}
	return &calls
}

// A row offering "attach" whose session is gone is a ghost: the record claims a
// status only a hook could have written, and the session that wrote it has been
// taken away. Pressing ⏎ is how the human meets one, so it must retire the
// record and name the way on — not refuse and leave the same dead key on the
// same row.
func TestAttachRetiresAGhostAndSaysWhereItWent(t *testing.T) {
	withFakeRecord(t, "phantom", &state.Dispatch{
		Feature: "phantom", TmuxSession: "cockpit-test-does-not-exist-8231",
		Status: state.StatusWorking,
	})
	calls := withFakeSweep(t, 1, 0)

	m := newModel()
	m.cfg = &config.Config{}
	mm, cmd := m.attach("phantom")
	if *calls != 1 {
		t.Fatalf("the sweep ran %d times, want 1 — attach must not decide this itself", *calls)
	}
	if cmd == nil {
		t.Error("a retired record must be reloaded, or the table keeps showing it as working")
	}
	if !strings.Contains(mm.notice, "retired") || !strings.Contains(mm.notice, "history") {
		t.Errorf("notice = %q, want the retirement and where the dispatcher went", mm.notice)
	}
}

// The one absence that proves nothing: a launching record has had no hook, so
// its session may not exist YET. The sweep declines to retire it, and attach
// must say that rather than reporting a dispatcher as lost mid-launch.
func TestAttachDoesNotRetireADispatcherStillStarting(t *testing.T) {
	withFakeRecord(t, "starting", &state.Dispatch{
		Feature: "starting", TmuxSession: "cockpit-test-does-not-exist-8232",
		Status: state.StatusLaunching,
	})
	withFakeSweep(t, 0, 0)

	m := newModel()
	m.cfg = &config.Config{}
	mm, cmd := m.attach("starting")
	if cmd != nil {
		t.Error("nothing changed, so nothing to reload")
	}
	if !strings.Contains(mm.notice, "still starting") {
		t.Errorf("notice = %q, want the launch window named", mm.notice)
	}
}

func TestKillCmdNothingToKill(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	liveRecords = map[string]*state.Dispatch{}

	msg := killCmd(nil)()
	am, ok := msg.(actionMsg)
	if !ok || am.notice != "nothing to kill" {
		t.Errorf("killCmd(nil) = %#v", msg)
	}

	msg = killCmd([]string{"unknown"})()
	am, ok = msg.(actionMsg)
	if !ok || am.notice != "nothing to kill" {
		t.Errorf("killCmd(unknown) = %#v", msg)
	}
}

func TestKillCmdReal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	saved := captureVars()
	defer restoreVars(saved)

	rec := &state.Dispatch{
		ID: "kill1", Feature: "to kill", TmuxSession: "cockpit-test-nonexistent-4471",
		Status: state.StatusWorking,
	}
	if err := state.Save(rec); err != nil {
		t.Fatal(err)
	}
	liveRecords = map[string]*state.Dispatch{"to kill": rec}

	msg := killCmd([]string{"to kill"})()
	am, ok := msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "killed 1 dispatcher") {
		t.Errorf("killCmd = %#v", msg)
	}
	if rec.Status != state.StatusExited {
		t.Errorf("record status = %q, want exited", rec.Status)
	}

	// Plural wording + a record already done is left alone (no re-save path,
	// but still counted as killed).
	rec2 := &state.Dispatch{ID: "kill2", Feature: "already done", TmuxSession: "cockpit-test-nonexistent-4472", Status: state.StatusDone}
	rec3 := &state.Dispatch{ID: "kill3", Feature: "also working", TmuxSession: "cockpit-test-nonexistent-4473", Status: state.StatusWorking}
	if err := state.Save(rec2); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(rec3); err != nil {
		t.Fatal(err)
	}
	liveRecords = map[string]*state.Dispatch{"already done": rec2, "also working": rec3}
	msg = killCmd([]string{"already done", "also working"})()
	am, ok = msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "killed 2 dispatchers") {
		t.Errorf("plural killCmd = %#v", msg)
	}
	if rec2.Status != state.StatusDone {
		t.Errorf("done record should be left alone, got %q", rec2.Status)
	}
}

func TestShipCmdNoRecord(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	liveRecords = map[string]*state.Dispatch{}

	msg := shipCmd("ghost")()
	am, ok := msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "nothing to ship") {
		t.Errorf("shipCmd(ghost) = %#v", msg)
	}
}

func TestShipCmdNoPR(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	rec := &state.Dispatch{ID: "ship1", Feature: "no pr feature", RepoPath: t.TempDir()}
	withFakeRecord(t, "no pr feature", rec)

	msg := shipCmd("no pr feature")()
	am, ok := msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "marked live") || strings.Contains(am.notice, "squash-merged") {
		t.Errorf("shipCmd(no pr) = %#v", msg)
	}
	if rec.Status != state.StatusDone {
		t.Errorf("status = %q, want done", rec.Status)
	}
}

func TestShipCmdWithOpenPRMergeFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	repoDir := t.TempDir() // not a git repo, and definitely no gh remote
	rec := &state.Dispatch{ID: "ship2", Feature: "has pr", RepoPath: repoDir, PRNumber: 42, PRState: "OPEN"}
	withFakeRecord(t, "has pr", rec)

	msg := shipCmd("has pr")()
	am, ok := msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "merge failed") {
		t.Errorf("shipCmd(open pr) = %#v", msg)
	}
	// A failed merge must not have marked the record done.
	if rec.Status == state.StatusDone {
		t.Error("record should not be marked done on merge failure")
	}
}

func TestMarkDoneCmd(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	liveRecords = map[string]*state.Dispatch{}

	msg := markDoneCmd("ghost")()
	am, ok := msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "has no record") {
		t.Errorf("markDoneCmd(ghost) = %#v", msg)
	}

	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	rec := &state.Dispatch{ID: "md1", Feature: "mark me"}
	liveRecords = map[string]*state.Dispatch{"mark me": rec}
	msg = markDoneCmd("mark me")()
	am, ok = msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "marked shipped") {
		t.Errorf("markDoneCmd(real) = %#v", msg)
	}
	if rec.Status != state.StatusDone {
		t.Errorf("status = %q, want done", rec.Status)
	}
}

func TestReplyCmdNoSession(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	liveRecords = map[string]*state.Dispatch{}

	msg := replyCmd("ghost", "hello")()
	am, ok := msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "no live session to reply") {
		t.Errorf("replyCmd(ghost) = %#v", msg)
	}

	// A record exists but its session is not live either.
	withFakeRecord(t, "quiet", &state.Dispatch{Feature: "quiet", TmuxSession: "cockpit-test-nonexistent-9981"})
	msg = replyCmd("quiet", "hello")()
	am, ok = msg.(actionMsg)
	if !ok || !strings.Contains(am.notice, "no live session to reply") {
		t.Errorf("replyCmd(dead session) = %#v", msg)
	}
}

func TestLaunchCmdNoConfig(t *testing.T) {
	msg := launchCmd(nil, "repo", "feature", "prompt", dispatchpkg.ModeAuto, dispatchpkg.DefaultModel, false)()
	lm, ok := msg.(launchedMsg)
	if !ok || !lm.failed || lm.feature != "feature" || !strings.Contains(lm.notice, "no config") {
		t.Errorf("launchCmd(nil cfg) = %#v", msg)
	}
}

func TestLaunchCmdRepoNotFound(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Roots: []string{dir}}
	msg := launchCmd(cfg, "nonexistent-repo", "feature", "prompt", dispatchpkg.ModeAuto, dispatchpkg.DefaultModel, false)()
	lm, ok := msg.(launchedMsg)
	if !ok || !lm.failed || lm.feature != "feature" || !strings.Contains(lm.notice, "repo not found") {
		t.Errorf("launchCmd(repo not found) = %#v", msg)
	}
}

func TestLaunchCmdEnsureBranchFails(t *testing.T) {
	// repos.Discover only requires a ".git" entry to exist — it does not
	// validate the repo. An empty ".git" directory passes discovery but
	// fails every real git operation, so dispatch.Launch's ensureBranch step
	// errors out before ever touching tmux — the safe way to exercise
	// launchCmd's failure branch without spawning a real session.
	root := t.TempDir()
	repoPath := root + "/fake-repo"
	if err := os.MkdirAll(repoPath+"/.git", 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Roots: []string{root}}

	msg := launchCmd(cfg, "fake-repo", "new feature", "do something", dispatchpkg.ModeAuto, dispatchpkg.DefaultModel, false)()
	lm, ok := msg.(launchedMsg)
	if !ok || !lm.failed || lm.feature != "new feature" || !strings.Contains(lm.notice, "launch failed") {
		t.Errorf("launchCmd(broken repo) = %#v", msg)
	}
}

// ---- Update() message-branch coverage (model.go) -----------------------------

func TestUpdateMessageBranches(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)

	m := newModel()
	var tm tea.Model = m

	tm, cmd := tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if tm.(model).width != 100 {
		t.Error("WindowSizeMsg not applied")
	}
	_ = cmd

	tm, _ = tm.Update(snapshotMsg(snapshot{dispatches: []dispatch{{feature: "x"}}}))
	if tm.(model).loading {
		t.Error("snapshotMsg should clear loading")
	}

	// stateChangedMsg / refreshTickMsg / trackedMsg with nil cfg still return
	// batched cmds without panicking.
	tm, cmd = tm.Update(stateChangedMsg{})
	if cmd == nil {
		t.Error("stateChangedMsg should return a cmd")
	}
	tm, cmd = tm.Update(refreshTickMsg{})
	if cmd == nil {
		t.Error("refreshTickMsg should return a cmd")
	}
	tm, cmd = tm.Update(trackedMsg{})
	if cmd == nil {
		t.Error("trackedMsg should return a cmd")
	}

	// actionMsg with nil cfg: sets notice, no reload cmd.
	tm, cmd = tm.Update(actionMsg{notice: "hello"})
	if tm.(model).notice != "hello" {
		t.Error("actionMsg notice not applied")
	}
	if cmd != nil {
		t.Error("actionMsg with nil cfg should not return a cmd")
	}

	// actionMsg with a cfg set: returns the reload cmd.
	mWithCfg := tm.(model)
	mWithCfg.cfg = &config.Config{}
	tm = mWithCfg
	tm, cmd = tm.Update(actionMsg{notice: "reload me"})
	if cmd == nil {
		t.Error("actionMsg with cfg should return a reload cmd")
	}

	// attachReturnedMsg both with and without an error, with and without cfg.
	tm, cmd = tm.Update(attachReturnedMsg{err: nil})
	if tm.(model).notice != "" {
		t.Error("attachReturnedMsg without error should clear notice")
	}
	if cmd == nil {
		t.Error("attachReturnedMsg with cfg should reload")
	}
	mNoCfg := tm.(model)
	mNoCfg.cfg = nil
	tm = mNoCfg
	tm, cmd = tm.Update(attachReturnedMsg{err: errFake{}})
	if !strings.Contains(tm.(model).notice, "attach failed") {
		t.Errorf("notice = %q", tm.(model).notice)
	}
	if cmd != nil {
		t.Error("attachReturnedMsg without cfg should not reload")
	}

	// cqFlashMsg: a stale seq is ignored, the current one ends the flash and
	// clears the item it was fired on.
	mStale := tm.(model)
	mStale.cqFlash, mStale.cqFlashSeq, mStale.cqFlashID = "killed", 4, "id-1"
	tm, _ = mStale.Update(cqFlashMsg{seq: 1})
	if tm.(model).cqFlash != "killed" {
		t.Error("a superseded cqFlashMsg must not clear the flash")
	}
	tm, _ = tm.(model).Update(cqFlashMsg{seq: 4})
	mDone := tm.(model)
	if mDone.cqFlash != "" {
		t.Error("the matching cqFlashMsg should end the flash")
	}
	if !mDone.cqSuppressed["id-1"] || mDone.cqCleared != 1 || mDone.cqUndo == nil {
		t.Errorf("ending a flash should clear the item and offer an undo: %+v", mDone.cqUndo)
	}

	// shipTickMsg with no shipFx is a no-op via advanceShip.
	tm, cmd = tm.Update(shipTickMsg{})
	if cmd != nil {
		t.Error("shipTickMsg with no shipFx should be a no-op")
	}

	// landClearMsg clears justLanded.
	mLanded := tm.(model)
	mLanded.justLanded = "something"
	tm, _ = mLanded.Update(landClearMsg{})
	if tm.(model).justLanded != "" {
		t.Error("landClearMsg should clear justLanded")
	}

	// undoClearMsg: matching seq clears, stale seq is ignored.
	mUndo := tm.(model)
	mUndo.undo, mUndo.undoSeq = "undo me", 5
	tm, _ = mUndo.Update(undoClearMsg{seq: 5})
	if tm.(model).undo != "" {
		t.Error("matching undoClearMsg should clear undo")
	}
	mUndo2 := tm.(model)
	mUndo2.undo, mUndo2.undoSeq = "keep me", 7
	tm, _ = mUndo2.Update(undoClearMsg{seq: 1})
	if tm.(model).undo != "keep me" {
		t.Error("stale undoClearMsg should not clear undo")
	}

	// tea.KeyMsg routes through handleKey.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if tm.(model).lens != "products" {
		t.Errorf("KeyMsg '2' should switch lens, got %q", tm.(model).lens)
	}
}

type errFake struct{}

func (errFake) Error() string { return "boom" }

func TestInitBothPaths(t *testing.T) {
	m := newModel()
	if cmd := m.Init(); cmd != nil {
		t.Error("Init with nil cfg should return nil")
	}
	m.cfg = &config.Config{}
	if cmd := m.Init(); cmd == nil {
		t.Error("Init with a cfg should return a batched cmd")
	}
}

// ---- refresh.go --------------------------------------------------------------

func TestRefreshCmds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	cfg := &config.Config{}

	msg := loadSnapshotCmd(cfg)()
	if _, ok := msg.(snapshotMsg); !ok {
		t.Errorf("loadSnapshotCmd = %#v", msg)
	}

	msg = trackRefreshCmd(cfg)()
	if _, ok := msg.(trackedMsg); !ok {
		t.Errorf("trackRefreshCmd = %#v", msg)
	}

	if cmd := waitState(nil); cmd == nil {
		t.Fatal("waitState(nil) should return a cmd")
	} else if msg := cmd(); msg != nil {
		t.Errorf("waitState(nil)() = %#v, want nil", msg)
	}

	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	msg = waitState(ch)()
	if _, ok := msg.(stateChangedMsg); !ok {
		t.Errorf("waitState(ch) = %#v", msg)
	}

	if cmd := refreshTick(); cmd == nil {
		t.Error("refreshTick should return a non-nil cmd")
	}
}

// ---- run.go --------------------------------------------------------------

func TestApplyConfigEnv(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "")
	t.Setenv("AZURE_DEVOPS_ORG", "")
	t.Setenv("AZURE_DEVOPS_PROJECT", "")
	cfg := &config.Config{LinearAPIKey: "secret", AzureOrg: "org", AzureProject: "proj"}
	applyConfigEnv(cfg)
	if got := os.Getenv("LINEAR_API_KEY"); got != "secret" {
		t.Errorf("LINEAR_API_KEY = %q", got)
	}
	if got := os.Getenv("AZURE_DEVOPS_ORG"); got != "org" {
		t.Errorf("AZURE_DEVOPS_ORG = %q", got)
	}
	if got := os.Getenv("AZURE_DEVOPS_PROJECT"); got != "proj" {
		t.Errorf("AZURE_DEVOPS_PROJECT = %q", got)
	}

	// A real env var already set wins over config.
	t.Setenv("LINEAR_API_KEY", "from-env")
	cfg2 := &config.Config{LinearAPIKey: "from-config"}
	applyConfigEnv(cfg2)
	if got := os.Getenv("LINEAR_API_KEY"); got != "from-env" {
		t.Errorf("env should win: got %q", got)
	}
}

func TestAnimateHelpersDirect(t *testing.T) {
	if cmd := shipTick(); cmd == nil {
		t.Error("shipTick should return a cmd")
	}
}
