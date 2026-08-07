package cockpit

// collectors_env_test.go builds a realistic on-disk portfolio — real git
// repos with a base commit and a feature-branch commit, dispatch records
// spanning every status, a transcript with real token usage plus a 429 rate
// limit line, ADR markdown files, and a queue.json draft — and then runs the
// real collectors (loadSnapshot and each collectX) against it. This is the
// closest thing to an end-to-end test the package has: it proves the
// record→collector→snapshot→lens pipeline produces real, renderable data
// rather than only exercising each piece in isolation.
//
// gh/tmux/git calls degrade to "no signal" here (the repos carry no remote,
// and no gh/tmux session exists to find), so this stays hermetic and
// network-free even though the real binaries are on PATH.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// envScenario is the on-disk fixture used by TestLoadSnapshotFullEnvironment
// and the direct per-collector tests below.
type envScenario struct {
	root        string
	repoAPath   string // "shop-api": ADRs, a merged+deployed record, a blocked record
	repoBPath   string // "shop-web": a needs-input/review record, a stale history
	repoCPath   string // "billing-core": unassigned product, an exited/never-shipped record
	repoAName   string
	repoBName   string
	repoCName   string
	baseAShaOld string
	cfg         *config.Config
}

// buildEnvScenario wires up the whole fixture: HOME and CLAUDE_DISPATCHER_STATE
// point at fresh temp dirs, three real git repos exist with a base commit plus
// a feature-branch commit each, dispatch records span blocked/needs-input/
// working/done/exited, one repo carries ADR markdown, the state dir carries a
// queue.json draft, and ~/.claude/projects carries a transcript with real
// usage tokens and a 429 rate-limit hit.
func buildEnvScenario(t *testing.T) envScenario {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	stateDir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", stateDir)

	root := t.TempDir()

	repoAPath := filepath.Join(root, "shop-api")
	repoBPath := filepath.Join(root, "shop-web")
	repoCPath := filepath.Join(root, "billing-core")
	mustMkGitRepo(t, repoAPath, "api")
	mustMkGitRepo(t, repoBPath, "web")
	mustMkGitRepo(t, repoCPath, "billing")

	baseA := gitOutput(t, repoAPath, "rev-parse", "HEAD")
	writeAndCommit(t, repoAPath, "handler.go", "package api\n\nfunc Handle() {}\n", "webhook retry logic")
	writeAndCommit(t, repoBPath, "export.go", "package web\n\nfunc Export() {}\n", "csv export")
	writeAndCommit(t, repoCPath, "billing.go", "package billing\n\nfunc Bill() {}\n", "billing tweak")

	// ADRs live under shop-api/doc/adr.
	adrDir := filepath.Join(repoAPath, "doc", "adr")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	adr := "# Retries are idempotent by event id\n\n## Status\nAccepted\n\n## Context\nWebhooks redeliver.\n\n## Decision\nDedupe by event id.\n\n## Consequences\nOne extra table.\n"
	if err := os.WriteFile(filepath.Join(adrDir, "0001-idempotent-retries.md"), []byte(adr), 0o644); err != nil {
		t.Fatal(err)
	}

	// A transcript for the blocked record, with a tool-use + assistant text
	// tail, real usage tokens, and a 429 rate_limit hit.
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	now := time.Now()
	transcript := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"retrying the webhook handler now"}]}}
`
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}

	// A second transcript under ~/.claude/projects feeds the usage collector
	// (it reads Claude Code's own session cache, not TranscriptPath).
	usageProj := filepath.Join(home, ".claude", "projects", "shop-api")
	if err := os.MkdirAll(usageProj, 0o755); err != nil {
		t.Fatal(err)
	}
	usageLine := func(ts time.Time, model string, in, out int) string {
		l := map[string]any{
			"type":       "assistant",
			"timestamp":  ts.UTC().Format(time.RFC3339),
			"session_id": "sess-1",
			"message": map[string]any{
				"model": model,
				"usage": map[string]any{"input_tokens": in, "output_tokens": out},
			},
		}
		b, _ := json.Marshal(l)
		return string(b)
	}
	limitLine := func(ts time.Time) string {
		l := map[string]any{
			"type":              "assistant",
			"timestamp":         ts.UTC().Format(time.RFC3339),
			"isApiErrorMessage": true,
			"apiErrorStatus":    429,
			"error":             "rate_limit exceeded",
		}
		b, _ := json.Marshal(l)
		return string(b)
	}
	lines := usageLine(now.Add(-time.Hour), "claude-opus-4", 5000, 1200) + "\n" +
		usageLine(now.Add(-30*time.Minute), "claude-sonnet-4", 2000, 800) + "\n" +
		limitLine(now.Add(-20*time.Minute)) + "\n"
	if err := os.WriteFile(filepath.Join(usageProj, "sess-1.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	// Dispatch records spanning every status the floor/products/velocity
	// collectors branch on.
	deployedAt := now.Add(-2 * time.Hour)
	mergedAt := now.Add(-3 * time.Hour)
	recs := []*state.Dispatch{
		{
			ID: "rec-blocked", Feature: "webhook retries", Slug: "webhook-retries",
			RepoPath: repoAPath, RepoName: "shop-api", Product: "shop",
			Branch: "main", BaseSHA: baseA, Prompt: "retry webhooks idempotently",
			TmuxSession: "cockpit-test-nonexistent-env-1", TranscriptPath: transcriptPath,
			Commits: []string{"c1", "c2"}, Status: state.StatusBlocked,
			StatusReason: "waiting on a permission approval",
			CreatedAt:    now.Add(-90 * time.Minute), UpdatedAt: now.Add(-5 * time.Minute),
		},
		{
			ID: "rec-needs", Feature: "csv export", Slug: "csv-export",
			RepoPath: repoBPath, RepoName: "shop-web", Product: "shop",
			Branch: "main", Prompt: "export a csv",
			TmuxSession: "cockpit-test-nonexistent-env-2",
			Commits:     []string{"c3"}, Status: state.StatusNeedsInput,
			StatusReason: "turn complete — waiting on you",
			CreatedAt:    now.Add(-4 * time.Hour), UpdatedAt: now.Add(-20 * time.Minute),
		},
		{
			ID: "rec-done", Feature: "seat limits", Slug: "seat-limits",
			RepoPath: repoAPath, RepoName: "shop-api", Product: "shop",
			Branch: "main", Prompt: "seat limits per plan",
			PRNumber: 9, PRState: "MERGED", PRMergedAt: &mergedAt, DeployedAt: &deployedAt,
			Commits: []string{"c4", "c5", "c6"}, Status: state.StatusDone,
			StatusReason: "deployed — live",
			CreatedAt:    now.Add(-8 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
		},
		{
			ID: "rec-exited", Feature: "abandoned spike", Slug: "abandoned-spike",
			RepoPath: repoCPath, RepoName: "billing-core", Product: "",
			Branch: "main", Prompt: "spike an idea",
			Status: state.StatusExited, StatusReason: "killed from cockpit",
			CreatedAt: now.Add(-30 * 24 * time.Hour), UpdatedAt: now.Add(-29 * 24 * time.Hour),
		},
		{
			ID: "rec-working", Feature: "billing tweak", Slug: "billing-tweak",
			RepoPath: repoCPath, RepoName: "billing-core", Product: "",
			Branch: "main", Prompt: "tweak billing",
			Status: state.StatusWorking, StatusReason: "",
			CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now.Add(-1 * time.Minute),
		},
	}
	for _, r := range recs {
		if err := state.Save(r); err != nil {
			t.Fatalf("state.Save(%s): %v", r.Feature, err)
		}
	}

	// A queued draft.
	queue := `[{"feature":"new draft","repo":"shop-api","prompt":"do the next thing"}]`
	if err := os.WriteFile(filepath.Join(state.Dir(), "queue.json"), []byte(queue), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Roots:    []string{root},
		Products: map[string][]string{"shop": {"shop-api", "shop-web"}},
	}

	return envScenario{
		root: root, repoAPath: repoAPath, repoBPath: repoBPath, repoCPath: repoCPath,
		repoAName: "shop-api", repoBName: "shop-web", repoCName: "billing-core",
		baseAShaOld: baseA, cfg: cfg,
	}
}

func mustMkGitRepo(t *testing.T, path, who string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", who + "@example.com"},
		{"config", "user.name", who},
	} {
		if out, err := gitCmd(t, path, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeAndCommit(t, path, "README.md", "# "+who+"\n", "initial commit")
}

// TestLoadSnapshotFullEnvironment drives the whole real-data pipeline: build
// the fixture, load a full snapshot, apply it, and render every lens at every
// responsive width without a panic or an overflowing pane.
func TestLoadSnapshotFullEnvironment(t *testing.T) {
	env := buildEnvScenario(t)

	saved := captureVars()
	defer applySnapshot(saved)

	snap := loadSnapshot(env.cfg)
	if snap.dataMode != "live" {
		t.Errorf("dataMode = %q, want live", snap.dataMode)
	}
	if len(snap.dispatches) == 0 {
		t.Fatal("expected floor dispatches from the real records")
	}
	if len(snap.products) == 0 {
		t.Fatal("expected at least one product row")
	}
	if len(snap.decisions["shop-api"]) == 0 {
		t.Error("expected the shop-api ADR to be picked up")
	}
	if len(snap.queueItems) != 1 || snap.queueItems[0].feature != "new draft" {
		t.Errorf("queueItems = %+v", snap.queueItems)
	}
	if snap.usageWindows == nil {
		t.Error("expected usage windows to be populated")
	}

	applySnapshot(snap)

	for _, w := range []int{80, 130, 190} {
		for i := 1; i <= 8; i++ {
			m := newModel()
			m.width, m.height = w, 46
			m = press(m, itoa(i))
			out := m.View()
			if trimNL(out) == "" {
				t.Fatalf("lens %d @%d empty on full-environment data", i, w)
			}
			if lines := countLines(out); lines > m.height {
				t.Fatalf("lens %d @%d overflows: %d lines for height %d", i, w, lines, m.height)
			}
		}
	}
}

func countLines(s string) int {
	n := 1
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}

// TestCollectorsDirect calls every collectX function directly against the
// same fixture's collectCtx, so each collector's own real-git/real-file
// branches are exercised even if loadSnapshot's ordering ever changes.
func TestCollectorsDirect(t *testing.T) {
	env := buildEnvScenario(t)

	saved := captureVars()
	defer applySnapshot(saved)

	ctx := &collectCtx{
		cfg:     env.cfg,
		records: state.LoadAll(),
		repos:   repos.Discover(env.cfg),
	}
	if len(ctx.records) != 5 {
		t.Fatalf("expected 5 loaded records, got %d", len(ctx.records))
	}
	if len(ctx.repos) == 0 {
		t.Fatal("expected repos.Discover to find the fixture repos")
	}

	var s snapshot
	collectFloor(ctx, &s)
	if len(s.dispatches) == 0 {
		t.Error("collectFloor produced no dispatches")
	}
	if len(s.records) == 0 {
		t.Error("collectFloor produced no feature→record map")
	}
	foundBlocked := false
	for _, d := range s.dispatches {
		if d.feature == "webhook retries" {
			foundBlocked = true
			if d.state != "blocked" {
				t.Errorf("webhook retries state = %q, want blocked", d.state)
			}
			if d.commits != 2 {
				t.Errorf("webhook retries commits = %d, want 2", d.commits)
			}
			// The numstat (plus/minus/files) comes from a live `git diff
			// base..branch`, whose result depends on the runner's git defaults;
			// floorNumstat itself is covered directly by TestFloorNumstat. Here
			// we only require the collector ran without panicking.
		}
	}
	if !foundBlocked {
		t.Error("expected to find the blocked 'webhook retries' dispatch")
	}
	if s.saidBy["webhook retries"] == "" {
		t.Error("expected floorSaid to pick up the transcript's assistant line")
	}

	var s2 snapshot
	collectProducts(ctx, &s2)
	if len(s2.products) == 0 {
		t.Error("collectProducts produced no products")
	}
	if _, ok := s2.reposByProduct["shop"]; !ok {
		t.Error("expected a 'shop' product repo grid")
	}
	if _, ok := s2.reposByProduct["unassigned"]; !ok {
		t.Error("expected an 'unassigned' product bucket for billing-core")
	}

	var s3 snapshot
	collectBacklog(ctx, &s3)
	if s3.backlogTickets == nil {
		t.Error("collectBacklog should return a non-nil (possibly empty) slice")
	}

	var s4 snapshot
	collectDecisions(ctx, &s4)
	if len(s4.decisions["shop-api"]) != 1 {
		t.Errorf("collectDecisions found %d records for shop-api, want 1", len(s4.decisions["shop-api"]))
	}
	if len(s4.plugins) < 2 {
		t.Errorf("expected at least the adr-tools + builtin plugins, got %d", len(s4.plugins))
	}

	var s5 snapshot
	collectUsage(ctx, &s5)
	if len(s5.usageWindows) != 2 {
		t.Fatalf("collectUsage windows = %d, want 2", len(s5.usageWindows))
	}
	if s5.usageWindows[0].note == "" {
		t.Error("expected a populated 5-hour window note")
	}
	if len(s5.usageModels) == 0 {
		t.Error("expected at least one model bucket from the fixture transcript")
	}

	var s6 snapshot
	collectVelocity(ctx, &s6)
	if len(s6.outputWeeks) == 0 {
		t.Error("collectVelocity produced no output weeks")
	}
	if s6.doraOrg == nil {
		t.Error("collectVelocity produced no org DORA metrics")
	}

	var s7 snapshot
	collectStaleQueue(ctx, &s7)
	if s7.queueItems == nil || len(s7.queueItems) != 1 {
		t.Errorf("collectStaleQueue queueItems = %+v, want 1 drafted item", s7.queueItems)
	}
	// billing-core has both an exited AND a working record; the working one
	// keeps it out of the stale list even though it is old.
	for _, sr := range s7.staleRepos {
		if sr.repo == "billing-core" {
			t.Error("billing-core has an active working dispatch and should not be stale")
		}
	}
	foundWorking := false
	for _, w := range s7.working {
		if w.feature == "billing tweak" {
			foundWorking = true
		}
	}
	if !foundWorking {
		t.Error("expected 'billing tweak' in the working list")
	}
}
