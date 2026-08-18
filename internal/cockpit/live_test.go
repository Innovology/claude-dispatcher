package cockpit

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// restoreVars puts the data vars back exactly as captureVars found them.
//
// It exists because applySnapshot is NOT a restore: it skips nil fields on
// purpose, so a collector that fetched nothing leaves the last good data on
// screen. That makes it unable to clear a var — which was invisible while the
// package shipped seed data (nothing was ever nil) and became a test-pollution
// bug the moment the vars started out empty. A test that populated the fleet
// leaked it into every test that ran after it.
func restoreVars(s snapshot) {
	dispatches, saidBy, tailLines, diffsBy = s.dispatches, s.saidBy, s.tailLines, s.diffsBy
	fleet, cqLastOutput = s.fleet, s.cqLastOutput
	products, reposByProduct, productOrder = s.products, s.reposByProduct, s.productOrder
	productNote, staleRepos, working = s.productNote, s.staleRepos, s.working
	productStats, backlogTickets = s.productStats, s.backlogTickets
	reviews, team, teamVerdict, shipped = s.reviews, s.team, s.teamVerdict, s.shipped
	productVelocity, decisions = s.productVelocity, s.decisions
	decisionRepoOrder, plugins = s.decisionRepoOrder, s.plugins
	usageWindows, usageModels = s.usageWindows, s.usageModels
	usageProjection, usageAdvice = s.usageProjection, s.usageAdvice
	doraOrg, doraFactory, doraSplit, doraWeeks = s.doraOrg, s.doraFactory, s.doraSplit, s.doraWeeks
	outputWeeks, outputHeadline = s.outputWeeks, s.outputHead
	outputUnit, outputDelta, outputSpark = s.outputUnit, s.outputDelta, s.outputSpark
	outputCoded, outputCodedNote = s.outputCoded, s.outputCodedNote
	notVelocity, liveRecords = s.notVelocity, s.records
	liveByID, productHistory = s.recordsByID, s.productHistory
}

// captureVars snapshots the current data vars so a test can restore them and
// not leak data into the tests that run after it.
func captureVars() snapshot {
	return snapshot{
		dispatches: dispatches, saidBy: saidBy, tailLines: tailLines,
		diffsBy: diffsBy, fleet: fleet,
		cqLastOutput: cqLastOutput, products: products,
		reposByProduct: reposByProduct, productOrder: productOrder,
		productNote: productNote, staleRepos: staleRepos, working: working,
		productStats: productStats, backlogTickets: backlogTickets,
		reviews: reviews, team: team, teamVerdict: teamVerdict, shipped: shipped,
		productVelocity: productVelocity, decisions: decisions,
		decisionRepoOrder: decisionRepoOrder, plugins: plugins,
		usageWindows: usageWindows, usageModels: usageModels,
		usageProjection: usageProjection, usageAdvice: usageAdvice,
		doraOrg: doraOrg, doraFactory: doraFactory, doraSplit: doraSplit,
		doraWeeks: doraWeeks, outputWeeks: outputWeeks, outputHead: outputHeadline,
		outputUnit: outputUnit, outputDelta: outputDelta, outputSpark: outputSpark,
		outputCoded: outputCoded, outputCodedNote: outputCodedNote,
		notVelocity: notVelocity, records: liveRecords,
		recordsByID: liveByID, productHistory: productHistory,
	}
}

// TestLiveSnapshotRenders runs the real collectors against a temp state dir of
// synthetic records, applies the snapshot, and renders every lens — proving the
// record→view mapping and applySnapshot never panic and stay renderable.
func TestLiveSnapshotRenders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)

	now := time.Now()
	recs := []*state.Dispatch{
		{ID: "aaa111", Feature: "webhook retries", Slug: "webhook-retries", RepoName: "shop-api", RepoPath: dir + "/shop-api", Product: "shop", Branch: "feature/webhook-retries", Prompt: "retry webhooks idempotently", Status: state.StatusBlocked, StatusReason: "waiting on a permission approval", PRNumber: 12, PRState: "OPEN", PRURL: "https://x/12", Commits: []string{"a", "b"}, CreatedAt: now.Add(-40 * time.Minute), UpdatedAt: now.Add(-4 * time.Minute)},
		{ID: "bbb222", Feature: "csv export", Slug: "csv-export", RepoName: "shop-web", RepoPath: dir + "/shop-web", Product: "shop", Branch: "feature/csv-export", Prompt: "export a csv", Status: state.StatusNeedsInput, StatusReason: "turn complete — waiting on you", Commits: []string{"c"}, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-22 * time.Minute)},
		{ID: "ccc333", Feature: "seat limits", Slug: "seat-limits", RepoName: "shop-api", RepoPath: dir + "/shop-api", Product: "shop", Branch: "feature/seat-limits", Prompt: "seat limits per plan", Status: state.StatusDone, StatusReason: "deployed — live", PRNumber: 9, PRState: "MERGED", DeployedAt: &now, PRMergedAt: &now, Commits: []string{"d", "e", "f"}, CreatedAt: now.Add(-6 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)},
	}
	for _, r := range recs {
		if err := state.Save(r); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	saved := captureVars()
	defer restoreVars(saved)

	cfg := &config.Config{Products: map[string][]string{"shop": {"shop-api", "shop-web"}}}
	snap := loadSnapshot(cfg)
	applySnapshot(snap)

	// Real records replaced the seed.
	if len(dispatches) != 3 {
		t.Fatalf("expected 3 live dispatches, got %d", len(dispatches))
	}
	var feats []string
	for _, d := range dispatches {
		feats = append(feats, d.feature)
	}
	joined := strings.Join(feats, ",")
	for _, want := range []string{"webhook retries", "csv export", "seat limits"} {
		if !strings.Contains(joined, want) {
			t.Errorf("live dispatches missing %q (got %s)", want, joined)
		}
	}
	// The action layer can resolve a real record.
	if recordFor("webhook retries") == nil {
		t.Error("recordFor did not resolve a live record")
	}

	// Every lens renders against live data without panic or overflow.
	for i := 1; i <= 6; i++ {
		m := newModel()
		m.width, m.height = 190, 44
		m = press(m, itoa(i))
		out := m.View()
		if strings.TrimSpace(out) == "" {
			t.Fatalf("lens %d empty on live data", i)
		}
		if lines := strings.Count(out, "\n") + 1; lines > m.height {
			t.Fatalf("lens %d overflows height on live data", i)
		}
	}
}

// TestLiveSnapshotEmpty proves an empty portfolio (no records, no config)
// degrades to honest empty states rather than crashing.
func TestLiveSnapshotEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)

	saved := captureVars()
	defer restoreVars(saved)

	snap := loadSnapshot(&config.Config{})
	applySnapshot(snap)

	for i := 1; i <= 6; i++ {
		m := newModel()
		m.width, m.height = 130, 40
		m = press(m, itoa(i))
		if strings.TrimSpace(m.View()) == "" {
			t.Fatalf("lens %d empty-portfolio render blank", i)
		}
	}
}

// The sweep that retires a dispatcher whose session is gone has to run on the
// ORDINARY load, not only on the reload that follows a jump-in.
//
// It shipped in one place: recheckCmd, which runs when the human comes back
// from a session. So a record whose session died without getting a SessionEnd
// out — a machine that slept the tmux server away, a WSL distro shut down with
// the last console, a reboot — kept claiming "working" for as long as the
// cockpit was open, and the row went on offering ⏎ attach. Attach found no
// session, said so, and changed nothing; the only thing that would have
// retired the record was coming back from an attach that could never happen.
// A ghost could only be cleared by attaching to it, which is the one thing it
// could not do.
func TestEveryLoadSweepsStraySessions(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	defer restoreVars(captureVars())

	if err := state.Save(&state.Dispatch{
		ID: "ghost", Feature: "ghost", Status: state.StatusWorking,
		TmuxSession: "disp-ghost",
	}); err != nil {
		t.Fatal(err)
	}

	prev := reconcileSessions
	defer func() { reconcileSessions = prev }()
	swept := 0
	var saw []string
	reconcileSessions = func(ds []*state.Dispatch) (int, int) {
		swept++
		for _, d := range ds {
			saw = append(saw, d.ID)
		}
		return 0, 0
	}

	loadSnapshot(&config.Config{})

	if swept != 1 {
		t.Fatalf("the load swept %d times, want 1", swept)
	}
	if len(saw) != 1 || saw[0] != "ghost" {
		t.Errorf("the sweep was handed %v, want the records this load read", saw)
	}
}
