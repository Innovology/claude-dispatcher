package cockpit

// pure_helpers_test.go exercises the package's pure, side-effect-free helper
// functions directly: the collect_*.go classification/formatting helpers,
// and the small layout/palette/util primitives every lens renders through.
// None of these touch global data vars, so no captureVars/applySnapshot
// dance is needed here.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/usage"
)

// ---- collect_backlog.go -----------------------------------------------------

func TestBlkPriFromLabels(t *testing.T) {
	cases := []struct {
		labels []string
		want   string
	}{
		{[]string{"urgent"}, "urgent"},
		{[]string{"incident"}, "urgent"},
		{[]string{"critical"}, "urgent"},
		{[]string{"p0"}, "urgent"},
		{[]string{"customer"}, "high"},
		{[]string{"bug"}, "high"},
		{[]string{"p1"}, "high"},
		{[]string{"high"}, "high"},
		{[]string{"chore"}, "med"},
		{nil, "med"},
	}
	for _, c := range cases {
		if got := blkPriFromLabels(c.labels); got != c.want {
			t.Errorf("blkPriFromLabels(%v) = %q, want %q", c.labels, got, c.want)
		}
	}
}

func TestBlkPriFromLinear(t *testing.T) {
	cases := map[string]string{
		"Urgent": "urgent", "High": "high", "Medium": "med", "Low": "low", "": "low",
	}
	for in, want := range cases {
		if got := blkPriFromLinear(in); got != want {
			t.Errorf("blkPriFromLinear(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlkNormalizePri(t *testing.T) {
	cases := map[string]string{
		"urgent": "urgent", "high": "high", "low": "low",
		"med": "med", "medium": "med", "whatever": "med",
	}
	for in, want := range cases {
		if got := blkNormalizePri(in); got != want {
			t.Errorf("blkNormalizePri(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlkActive(t *testing.T) {
	active := []state.Status{state.StatusWorking, state.StatusLaunching, state.StatusNeedsInput, state.StatusBlocked}
	for _, s := range active {
		if !blkActive(s) {
			t.Errorf("blkActive(%v) = false, want true", s)
		}
	}
	inactive := []state.Status{state.StatusDone, state.StatusExited}
	for _, s := range inactive {
		if blkActive(s) {
			t.Errorf("blkActive(%v) = true, want false", s)
		}
	}
}

func TestBlkTakenBy(t *testing.T) {
	recs := []*state.Dispatch{
		{Feature: "webhook retries", Branch: "feature/webhook-retries", Status: state.StatusWorking},
		{Feature: "done thing", Branch: "feature/done-thing", Status: state.StatusDone},
	}
	if got := blkTakenBy(recs, "gh#12", "webhook retries"); got != "webhook retries" {
		t.Errorf("branch match: got %q", got)
	}
	if got := blkTakenBy(recs, "gh#99", "totally unrelated"); got != "" {
		t.Errorf("no match: got %q, want empty", got)
	}
	if got := blkTakenBy(recs, "gh#1", "done thing"); got != "" {
		t.Errorf("inactive record should not match: got %q", got)
	}
}

func TestBlkAge(t *testing.T) {
	if got := blkAge(time.Time{}); got != "" {
		t.Errorf("zero time: got %q, want empty", got)
	}
	now := time.Now()
	if got := blkAge(now.Add(-5 * time.Minute)); got != "5m" {
		t.Errorf("5m ago: got %q", got)
	}
	if got := blkAge(now.Add(-3 * time.Hour)); got != "3h" {
		t.Errorf("3h ago: got %q", got)
	}
	if got := blkAge(now.Add(-3 * 24 * time.Hour)); got != "3d" {
		t.Errorf("3d ago: got %q", got)
	}
	// future timestamp clamps to non-negative duration.
	if got := blkAge(now.Add(5 * time.Minute)); got != "0m" {
		t.Errorf("future time: got %q, want 0m", got)
	}
}

// ---- collect_decisions.go ---------------------------------------------------

func TestDcnStatus(t *testing.T) {
	cases := map[string]string{
		"Superseded": "superseded",
		"deprecated": "superseded",
		"rejected":   "superseded",
		"Proposed":   "proposed",
		"draft":      "proposed",
		"Accepted":   "accepted",
		"who knows":  "accepted",
	}
	for in, want := range cases {
		if got := dcnStatus(in); got != want {
			t.Errorf("dcnStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDcnInlineStatus(t *testing.T) {
	text := "# Title\n\nsome text\nStatus: Proposed\nmore text"
	if got := dcnInlineStatus(text); got != "proposed" {
		t.Errorf("dcnInlineStatus = %q, want proposed", got)
	}
	if got := dcnInlineStatus("no status line here"); got != "" {
		t.Errorf("dcnInlineStatus with none = %q, want empty", got)
	}
}

func TestDcnSections(t *testing.T) {
	text := "# T\n## Status\naccepted\n## Context\nline one\nline two\n## Decision\nwe decided\n## Consequences\nit follows"
	sec := dcnSections(text)
	if sec["status"] != "accepted" {
		t.Errorf("status section = %q", sec["status"])
	}
	if sec["context"] != "line one line two" {
		t.Errorf("context section = %q", sec["context"])
	}
	if sec["decision"] != "we decided" {
		t.Errorf("decision section = %q", sec["decision"])
	}
	if sec["consequences"] != "it follows" {
		t.Errorf("consequences section = %q", sec["consequences"])
	}
}

func TestDcnAge(t *testing.T) {
	if got := dcnAge(time.Time{}); got != "" {
		t.Errorf("zero time: got %q", got)
	}
	now := time.Now()
	if got := dcnAge(now.Add(-10 * time.Minute)); got != "10m ago" {
		t.Errorf("10m ago: got %q", got)
	}
	if got := dcnAge(now.Add(-2 * time.Hour)); got != "2h ago" {
		t.Errorf("2h ago: got %q", got)
	}
	if got := dcnAge(now.Add(-2 * 24 * time.Hour)); got != "2d ago" {
		t.Errorf("2d ago: got %q", got)
	}
}

func TestDcnFindAdrsAndParseFile(t *testing.T) {
	dir := t.TempDir()
	// No ADR dir at all.
	if d, files := dcnFindAdrs(dir); d != "" || files != nil {
		t.Errorf("empty repo: got dir=%q files=%v", d, files)
	}

	adrDir := filepath.Join(dir, "doc", "adr")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# Use Postgres\n\n## Status\nAccepted\n\n## Context\nWe need a database.\n\n## Decision\nUse Postgres.\n\n## Consequences\nOperational cost.\n"
	path := filepath.Join(adrDir, "0001-use-postgres.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-markdown file in the same dir must be skipped.
	if err := os.WriteFile(filepath.Join(adrDir, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}

	gotDir, files := dcnFindAdrs(dir)
	if gotDir != adrDir {
		t.Errorf("dcnFindAdrs dir = %q, want %q", gotDir, adrDir)
	}
	if len(files) != 1 {
		t.Fatalf("dcnFindAdrs files = %v, want 1", files)
	}

	d, ok := dcnParseFile(dir, files[0])
	if !ok {
		t.Fatal("dcnParseFile ok = false")
	}
	if d.id != "0001" {
		t.Errorf("id = %q, want 0001", d.id)
	}
	if d.title != "Use Postgres" {
		t.Errorf("title = %q", d.title)
	}
	if d.status != "accepted" {
		t.Errorf("status = %q", d.status)
	}
	if d.context == "" || d.decision == "" || d.consequences == "" {
		t.Errorf("sections not populated: %+v", d)
	}

	// A file with no leading digits and no "# " heading falls back to base name.
	path2 := filepath.Join(adrDir, "no-heading.md")
	if err := os.WriteFile(path2, []byte("no heading here"), 0o644); err != nil {
		t.Fatal(err)
	}
	d2, ok := dcnParseFile(dir, path2)
	if !ok {
		t.Fatal("dcnParseFile(no-heading) ok = false")
	}
	if d2.id != "no-heading" || d2.title != "no-heading" {
		t.Errorf("fallback id/title = %q/%q", d2.id, d2.title)
	}

	// A missing file returns ok=false.
	if _, ok := dcnParseFile(dir, filepath.Join(adrDir, "missing.md")); ok {
		t.Error("dcnParseFile(missing) ok = true, want false")
	}
}

// ---- collect_stale_queue.go -------------------------------------------------

func TestStqActive(t *testing.T) {
	for _, s := range []state.Status{state.StatusWorking, state.StatusLaunching, state.StatusNeedsInput, state.StatusBlocked} {
		if !stqActive(s) {
			t.Errorf("stqActive(%v) = false", s)
		}
	}
	for _, s := range []state.Status{state.StatusDone, state.StatusExited} {
		if stqActive(s) {
			t.Errorf("stqActive(%v) = true", s)
		}
	}
}

func TestStqDaysSinceCommit(t *testing.T) {
	if _, ok := stqDaysSinceCommit("/nonexistent/path/xyz"); ok {
		t.Error("nonexistent path: ok = true, want false")
	}
	repo := newTestGitRepo(t, "committer")
	if days, ok := stqDaysSinceCommit(repo); !ok || days != 0 {
		t.Errorf("fresh repo: days=%d ok=%v, want 0/true", days, ok)
	}
}

func TestStqStartOf(t *testing.T) {
	now := time.Now()
	d := &state.Dispatch{CreatedAt: now, UpdatedAt: now.Add(time.Hour)}
	if got := stqStartOf(d); !got.Equal(now) {
		t.Errorf("stqStartOf prefers CreatedAt: got %v", got)
	}
	d2 := &state.Dispatch{UpdatedAt: now}
	if got := stqStartOf(d2); !got.Equal(now) {
		t.Errorf("stqStartOf falls back to UpdatedAt: got %v", got)
	}
}

func TestStqAge(t *testing.T) {
	if got := stqAge(time.Time{}); got != "" {
		t.Errorf("zero time: got %q", got)
	}
	now := time.Now()
	if got := stqAge(now.Add(-5 * time.Minute)); got != "5m" {
		t.Errorf("5m: got %q", got)
	}
	if got := stqAge(now.Add(-5 * time.Hour)); got != "5h" {
		t.Errorf("5h: got %q", got)
	}
	if got := stqAge(now.Add(-5 * 24 * time.Hour)); got != "5d" {
		t.Errorf("5d: got %q", got)
	}
}

// ---- collect_floor.go --------------------------------------------------------

func TestFloorStateBranches(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		rec  *state.Dispatch
		want string
	}{
		{"blocked", &state.Dispatch{Status: state.StatusBlocked}, "blocked"},
		{"needs-review", &state.Dispatch{Status: state.StatusNeedsInput, PRNumber: 5, PRState: "OPEN"}, "review"},
		{"needs-plain", &state.Dispatch{Status: state.StatusNeedsInput}, "needs"},
		{"needs-deployed", &state.Dispatch{Status: state.StatusNeedsInput, PRNumber: 5, PRState: "OPEN", DeployedAt: &now}, "needs"},
		{"working", &state.Dispatch{Status: state.StatusWorking}, "working"},
		{"launching", &state.Dispatch{Status: state.StatusLaunching}, "working"},
		{"done", &state.Dispatch{Status: state.StatusDone}, "live"},
		{"exited-merged", &state.Dispatch{Status: state.StatusExited, PRState: "MERGED"}, "live"},
		{"exited-merged-at", &state.Dispatch{Status: state.StatusExited, PRMergedAt: &now}, "live"},
		{"exited-nothing", &state.Dispatch{Status: state.StatusExited}, ""},
	}
	for _, c := range cases {
		if got := floorState(c.rec); got != c.want {
			t.Errorf("%s: floorState = %q, want %q", c.name, got, c.want)
		}
	}
}

// productFor resolves from the config in ctx, not from the package's product
// vars — collectFloor runs before collectProducts fills those, so reading them
// meant every dispatch landed in no group at all on the first load.
func TestCollectCtxProductFor(t *testing.T) {
	ctx := &collectCtx{cfg: &config.Config{
		Products: map[string][]string{"shop": {"shop-api"}},
	}}

	if got := ctx.productFor(&state.Dispatch{Product: "shop"}); got != "shop" {
		t.Errorf("recorded product: got %q, want shop", got)
	}
	if got := ctx.productFor(&state.Dispatch{RepoName: "shop-api"}); got != "shop" {
		t.Errorf("mapped by repo name: got %q, want shop", got)
	}
	// A repo with no mapping, and a stale product no longer in the config,
	// both fold into "unassigned" — the same key collectProducts uses, so the
	// floor's groups line up with the products lens.
	if got := ctx.productFor(&state.Dispatch{RepoName: "not-mapped"}); got != "unassigned" {
		t.Errorf("unmapped repo: got %q, want unassigned", got)
	}
	if got := ctx.productFor(&state.Dispatch{Product: "retired"}); got != "unassigned" {
		t.Errorf("product no longer in config: got %q, want unassigned", got)
	}
}

func TestFloorAgentStateAndSignal(t *testing.T) {
	if got := floorAgentState("working"); got != "now" {
		t.Errorf("working: got %q", got)
	}
	if got := floorAgentState("live"); got != "ok" {
		t.Errorf("live: got %q", got)
	}
	if got := floorAgentState("blocked"); got != "idle" {
		t.Errorf("blocked: got %q", got)
	}

	if got := floorSignal("blocked", gh.Checks{}, gh.Review{}); got != "approve" {
		t.Errorf("blocked signal: %q", got)
	}
	if got := floorSignal("needs", gh.Checks{}, gh.Review{}); got != "needs you" {
		t.Errorf("needs signal: %q", got)
	}
	if got := floorSignal("review", gh.Checks{Failing: 1}, gh.Review{}); got != "checks ✗" {
		t.Errorf("review-failing: %q", got)
	}
	if got := floorSignal("review", gh.Checks{Running: 1}, gh.Review{}); got != "ci running" {
		t.Errorf("review-running: %q", got)
	}
	if got := floorSignal("review", gh.Checks{}, gh.Review{Approvals: 1}); got != "approved" {
		t.Errorf("review-approved: %q", got)
	}
	if got := floorSignal("review", gh.Checks{}, gh.Review{}); got != "mergeable" {
		t.Errorf("review-mergeable: %q", got)
	}
	if got := floorSignal("working", gh.Checks{}, gh.Review{}); got != "working" {
		t.Errorf("working: %q", got)
	}
	if got := floorSignal("live", gh.Checks{}, gh.Review{}); got != "live" {
		t.Errorf("live: %q", got)
	}
	if got := floorSignal("unknown", gh.Checks{}, gh.Review{}); got != "" {
		t.Errorf("unknown: %q, want empty", got)
	}
}

func TestFloorAsk(t *testing.T) {
	rec := &state.Dispatch{RepoName: "shop-api", StatusReason: "waiting"}
	if got := floorAsk(rec, "working", 3, "5m"); got != nil {
		t.Errorf("working state should have no ask block, got %+v", got)
	}
	got := floorAsk(rec, "blocked", 3, "5m")
	if got == nil {
		t.Fatal("blocked state should produce an ask block")
	}
	if got.evidence != "3 commits · 5m" {
		t.Errorf("evidence = %q", got.evidence)
	}
}

func TestFloorNumstat(t *testing.T) {
	plus, minus, files, dfs := floorNumstat("", "", "")
	if plus != 0 || minus != 0 || files != 0 || len(dfs) != 0 {
		t.Errorf("empty inputs should degrade to zero: %d %d %d %v", plus, minus, files, dfs)
	}

	repo := newTestGitRepo(t, "numstat")
	base := gitOutput(t, repo, "rev-parse", "HEAD")
	writeAndCommit(t, repo, "file.txt", "line one\nline two\n", "second commit")

	plus, minus, files, dfs = floorNumstat(repo, base, "HEAD")
	if files != 1 || plus == 0 {
		t.Errorf("real diff: plus=%d minus=%d files=%d dfs=%v", plus, minus, files, dfs)
	}
}

func TestFloorChecksMetaAndApprovals(t *testing.T) {
	if label, color := floorChecksMeta(gh.Checks{}); label != "" || color != cFaint {
		t.Errorf("no checks: %q %q", label, color)
	}
	if label, _ := floorChecksMeta(gh.Checks{Total: 4, Failing: 1, Passed: 2}); label != "✗ 2/4" {
		t.Errorf("failing: %q", label)
	}
	if label, _ := floorChecksMeta(gh.Checks{Total: 4, Running: 1, Passed: 2}); label != "● 2/4" {
		t.Errorf("running: %q", label)
	}
	if label, _ := floorChecksMeta(gh.Checks{Total: 4, Passed: 4}); label != "✓ 4/4" {
		t.Errorf("green: %q", label)
	}
	if got := floorApprovals(1); got != "1 approval" {
		t.Errorf("1 approval: %q", got)
	}
	if got := floorApprovals(3); got != "3 approvals" {
		t.Errorf("3 approvals: %q", got)
	}
}

func TestPrIDAndForgeLabel(t *testing.T) {
	if got := prID("ado", 12); got != "!12" {
		t.Errorf("ado prID: %q", got)
	}
	if got := prID("gh", 12); got != "#12" {
		t.Errorf("gh prID: %q", got)
	}
	if got := forgeLabel("ado"); got != "azure devops" {
		t.Errorf("ado label: %q", got)
	}
	if got := forgeLabel("gh"); got != "github" {
		t.Errorf("gh label: %q", got)
	}
}

func TestFloorAgeBuckets(t *testing.T) {
	if got := floorAge(time.Time{}); got != "—" {
		t.Errorf("zero: %q", got)
	}
	now := time.Now()
	if got := floorAge(now.Add(-30 * time.Second)); got != "now" {
		t.Errorf("30s: %q", got)
	}
	if got := floorAge(now.Add(-10 * time.Minute)); got != "10m" {
		t.Errorf("10m: %q", got)
	}
	if got := floorAge(now.Add(-5 * time.Hour)); got != "5h" {
		t.Errorf("5h: %q", got)
	}
	if got := floorAge(now.Add(-5 * 24 * time.Hour)); got != "5d" {
		t.Errorf("5d: %q", got)
	}
}

func TestFloorChainBranches(t *testing.T) {
	now := time.Now()
	merged := now.Add(-time.Hour)
	deployed := now.Add(-30 * time.Minute)

	// zero commits, no PR, no checks, no merge, no deploy.
	ch := floorChain(&state.Dispatch{}, "gh", 0, 0, 0, gh.Checks{})
	if len(ch) != 5 {
		t.Fatalf("floorChain len = %d, want 5", len(ch))
	}
	if ch[0].state != "idle" || ch[1].label != "no pr" {
		t.Errorf("commit/PR steps: %+v %+v", ch[0], ch[1])
	}

	// full pipeline: commits, merged PR, failing checks, merged, deployed.
	rec := &state.Dispatch{
		Commits: []string{"a", "b"}, PRNumber: 7, PRState: "MERGED",
		PRMergedAt: &merged, DeployedAt: &deployed,
	}
	ch = floorChain(rec, "ado", 10, 2, 3, gh.Checks{Total: 4, Failing: 1, Passed: 2})
	if ch[0].state != "ok" {
		t.Errorf("commits step: %+v", ch[0])
	}
	if ch[1].label == "" {
		t.Errorf("PR step empty")
	}
	if ch[2].state != "bad" {
		t.Errorf("checks step should be bad: %+v", ch[2])
	}
	if ch[3].label != "merged" {
		t.Errorf("merge step: %+v", ch[3])
	}
	if ch[4].label != "deployed" {
		t.Errorf("deploy step: %+v", ch[4])
	}

	// open PR waiting to merge; no checks recorded; deploy pending after merge.
	rec2 := &state.Dispatch{PRNumber: 8, PRState: "OPEN"}
	ch = floorChain(rec2, "gh", 1, 0, 1, gh.Checks{})
	if ch[2].label != "checks" {
		t.Errorf("no-checks step: %+v", ch[2])
	}
	if ch[3].state != "now" {
		t.Errorf("open-PR merge step should be 'now': %+v", ch[3])
	}
	if ch[4].label != "deploy" {
		t.Errorf("deploy pending step: %+v", ch[4])
	}

	// running checks branch.
	ch = floorChain(&state.Dispatch{}, "gh", 0, 0, 0, gh.Checks{Total: 4, Running: 1, Passed: 2})
	if ch[2].state != "now" {
		t.Errorf("running checks step: %+v", ch[2])
	}

	// merged PR with no PRMergedAt timestamp.
	rec3 := &state.Dispatch{PRState: "MERGED"}
	ch = floorChain(rec3, "gh", 0, 0, 0, gh.Checks{})
	if ch[3].meta != "on github" {
		t.Errorf("merged-no-time meta: %+v", ch[3])
	}

	// closed PR (not merged): merge/deploy both idle.
	rec4 := &state.Dispatch{PRNumber: 9, PRState: "CLOSED"}
	ch = floorChain(rec4, "gh", 0, 0, 0, gh.Checks{})
	if ch[3].state != "idle" || ch[4].state != "idle" {
		t.Errorf("closed PR merge/deploy: %+v %+v", ch[3], ch[4])
	}
}

// ---- collect_products.go -----------------------------------------------------

func TestProdLiveAtAndDay(t *testing.T) {
	now := time.Now()
	deployed := now.Add(-time.Hour)
	merged := now.Add(-2 * time.Hour)

	if got, ok := prodLiveAt(&state.Dispatch{DeployedAt: &deployed}); !ok || !got.Equal(deployed) {
		t.Errorf("deployed: %v %v", got, ok)
	}
	if got, ok := prodLiveAt(&state.Dispatch{PRMergedAt: &merged}); !ok || !got.Equal(merged) {
		t.Errorf("merged: %v %v", got, ok)
	}
	if _, ok := prodLiveAt(&state.Dispatch{}); ok {
		t.Error("neither: ok should be false")
	}

	if !prodSameDay(now, now) {
		t.Error("prodSameDay(now, now) should be true")
	}
	if prodSameDay(now, now.AddDate(0, 0, -1)) {
		t.Error("prodSameDay across days should be false")
	}
}

func TestProdDayLabel(t *testing.T) {
	now := time.Now()
	if got := prodDayLabel(now, now); got != "today" {
		t.Errorf("today: %q", got)
	}
	if got := prodDayLabel(now.AddDate(0, 0, -1), now); got != "yesterday" {
		t.Errorf("yesterday: %q", got)
	}
	if got := prodDayLabel(now.AddDate(0, 0, -5), now); got == "today" || got == "yesterday" {
		t.Errorf("5 days ago should not be today/yesterday: %q", got)
	}
}

func TestProdAgeAndDur(t *testing.T) {
	now := time.Now()
	if got := prodAge(now.Add(5 * time.Minute)); got != "0m" {
		t.Errorf("future time clamps: %q", got)
	}
	if got := prodAge(now.Add(-10 * time.Minute)); got != "10m" {
		t.Errorf("10m: %q", got)
	}
	if got := prodAge(now.Add(-5 * time.Hour)); got != "5h" {
		t.Errorf("5h: %q", got)
	}
	if got := prodAge(now.Add(-5 * 24 * time.Hour)); got != "5d" {
		t.Errorf("5d: %q", got)
	}

	if got := prodDur(30 * time.Second); got != "0m" {
		t.Errorf("sub-minute: %q", got)
	}
	if got := prodDur(45 * time.Minute); got != "45m" {
		t.Errorf("45m: %q", got)
	}
	if got := prodDur(3*time.Hour + 20*time.Minute); got != "3h 20m" {
		t.Errorf("3h20m: %q", got)
	}
	if got := prodDur(2*24*time.Hour + 4*time.Hour); got != "2d 4h" {
		t.Errorf("2d4h: %q", got)
	}
}

func TestProdMedianAndSpark(t *testing.T) {
	if _, ok := prodMedian(nil); ok {
		t.Error("empty durations: ok should be false")
	}
	med, ok := prodMedian([]time.Duration{time.Minute, 3 * time.Minute, 2 * time.Minute})
	if !ok || med != 2*time.Minute {
		t.Errorf("median = %v %v", med, ok)
	}

	if got := prodSpark(nil); got != "" {
		t.Errorf("empty spark: %q", got)
	}
	if got := prodSpark([]int{0, 1, 2, 3}); got == "" {
		t.Error("non-empty spark should render")
	}
	if got := prodSpark([]int{0, 0, 0}); got == "" {
		t.Error("all-zero spark should still render (max=0 branch)")
	}
}

func TestProdRepoCountAndForge(t *testing.T) {
	if got := prodRepoCount(1); got != "1 repo" {
		t.Errorf("1 repo: %q", got)
	}
	if got := prodRepoCount(3); got != "3 repos" {
		t.Errorf("3 repos: %q", got)
	}

	ctx := &collectCtx{}
	if got := prodForge(nil, nil, ctx); got != "—" {
		t.Errorf("no names: %q", got)
	}

	ghRepo := newTestGitRepo(t, "prodforge")
	disc := map[string]repos.Repo{"ghrepo": {Name: "ghrepo", Path: ghRepo}}
	if got := prodForge([]string{"ghrepo"}, disc, ctx); got != "github" {
		t.Errorf("gh-only repo set: got %q", got)
	}
	// A name with no discovered path is simply skipped (still resolves).
	if got := prodForge([]string{"unknown"}, disc, ctx); got != "—" {
		t.Errorf("unknown-only repo set: got %q", got)
	}
}

func TestProdNoteAndWaitingAndChecksAndSize(t *testing.T) {
	if got := prodNote(0, 0, 0, 0); got != "" {
		t.Errorf("all zero: %q", got)
	}
	if got := prodNote(2, 1, 3, 4); got == "" {
		t.Error("mixed counts should produce a note")
	}

	if got := prodWaiting("APPROVED", false); got != "approved" {
		t.Errorf("approved: %q", got)
	}
	if got := prodWaiting("CHANGES_REQUESTED", false); got != "changes" {
		t.Errorf("changes: %q", got)
	}
	if got := prodWaiting("REVIEW_REQUIRED", true); got != "you" {
		t.Errorf("review-required mine: %q", got)
	}
	if got := prodWaiting("REVIEW_REQUIRED", false); got != "review" {
		t.Errorf("review-required not mine: %q", got)
	}
	if got := prodWaiting("", true); got != "you" {
		t.Errorf("default mine: %q", got)
	}
	if got := prodWaiting("", false); got != "" {
		t.Errorf("default not mine: %q", got)
	}

	if got := prodChecks(gh.Checks{}); got != "—" {
		t.Errorf("no checks: %q", got)
	}
	if got := prodChecks(gh.Checks{Total: 4, Failing: 1, Passed: 2}); got != "✗ 1/4" {
		t.Errorf("failing: %q", got)
	}
	if got := prodChecks(gh.Checks{Total: 4, Running: 1, Passed: 2}); got != "● 2/4" {
		t.Errorf("running: %q", got)
	}
	if got := prodChecks(gh.Checks{Total: 4, Passed: 4}); got != "✓ 4/4" {
		t.Errorf("green: %q", got)
	}

	if got := prodSize(10, 3); got != "+10 −3" {
		t.Errorf("size: %q", got)
	}
}

func TestProdBands(t *testing.T) {
	if got := prodDeployBand(5); got != "elite" {
		t.Errorf("elite: %q", got)
	}
	if got := prodDeployBand(1.5); got != "high" {
		t.Errorf("high: %q", got)
	}
	if got := prodDeployBand(0.5); got != "medium" {
		t.Errorf("medium: %q", got)
	}
	if got := prodDeployBand(0.05); got != "low" {
		t.Errorf("low: %q", got)
	}

	if got := prodLeadBand(time.Hour); got != "elite" {
		t.Errorf("lead elite: %q", got)
	}
	if got := prodLeadBand(4 * time.Hour); got != "high" {
		t.Errorf("lead high: %q", got)
	}
	if got := prodLeadBand(12 * time.Hour); got != "medium" {
		t.Errorf("lead medium: %q", got)
	}
	if got := prodLeadBand(48 * time.Hour); got != "low" {
		t.Errorf("lead low: %q", got)
	}

	if got := prodWaitBand(30 * time.Minute); got != "elite" {
		t.Errorf("wait elite: %q", got)
	}
	if got := prodWaitBand(2 * time.Hour); got != "high" {
		t.Errorf("wait high: %q", got)
	}
	if got := prodWaitBand(8 * time.Hour); got != "medium" {
		t.Errorf("wait medium: %q", got)
	}
	if got := prodWaitBand(24 * time.Hour); got != "low" {
		t.Errorf("wait low: %q", got)
	}
}

// ---- collect_usage.go --------------------------------------------------------

func TestUsgTokens(t *testing.T) {
	if got := usgTokens(2_500_000); got != "2.5M" {
		t.Errorf("2.5M: %q", got)
	}
	if got := usgTokens(40_000); got != "40K" {
		t.Errorf("40K: %q", got)
	}
	if got := usgTokens(400); got != "400" {
		t.Errorf("400: %q", got)
	}
}

func TestUsgWindow(t *testing.T) {
	w := usgWindow("5-hour session", usage.Stat{Total: 100, Cap: 50, CapSource: "limit", Sessions: 2})
	if w.used != 100 {
		t.Errorf("used should clamp to 100: got %d", w.used)
	}
	w2 := usgWindow("weekly", usage.Stat{Total: 10, Cap: 100, CapSource: "observed"})
	if w2.used != 10 {
		t.Errorf("used = %d, want 10", w2.used)
	}
	w3 := usgWindow("weekly", usage.Stat{Total: 10, HighWater: 40})
	if w3.used != 25 {
		t.Errorf("denom-fallback used = %d, want 25", w3.used)
	}
	w4 := usgWindow("weekly", usage.Stat{})
	if w4.note != "0 tok · no usage yet" {
		t.Errorf("empty stat note = %q", w4.note)
	}
}

func TestUsgAgo(t *testing.T) {
	now := time.Now()
	if got := usgAgo(now.Add(-10 * time.Minute)); got != "10m" {
		t.Errorf("10m: %q", got)
	}
	if got := usgAgo(now.Add(-5 * time.Hour)); got != "5h" {
		t.Errorf("5h: %q", got)
	}
	if got := usgAgo(now.Add(-3 * 24 * time.Hour)); got != "3d" {
		t.Errorf("3d: %q", got)
	}
}

// ---- layout.go / util.go / palette.go ---------------------------------------

func TestTruncateEdgeCases(t *testing.T) {
	if got := truncate("hello", 0); got != "" {
		t.Errorf("w<=0: %q", got)
	}
	if got := truncate("hello", 1); got != "…" {
		t.Errorf("w=1: %q", got)
	}
	if got := truncate("hi", 10); got != "hi" {
		t.Errorf("fits already: %q", got)
	}
	if got := truncate("hello world", 5); dispWidth(got) > 5 {
		t.Errorf("overflow not clipped: %q", got)
	}
}

func TestTruncateAnsiEdge(t *testing.T) {
	if got := truncateAnsi("hello", 0); got != "" {
		t.Errorf("w<=0: %q", got)
	}
	if got := truncateAnsi("hello", -1); got != "" {
		t.Errorf("w<0: %q", got)
	}
}

func TestClampCursor(t *testing.T) {
	if got := clampCursor(0, 0); got != 0 {
		t.Errorf("n=0: %d", got)
	}
	if got := clampCursor(-5, 10); got != 0 {
		t.Errorf("negative i: %d", got)
	}
	if got := clampCursor(20, 10); got != 9 {
		t.Errorf("i>=n: %d", got)
	}
	if got := clampCursor(3, 10); got != 3 {
		t.Errorf("in range: %d", got)
	}
}

func TestBarEdges(t *testing.T) {
	if got := bar(-10, 10); got != "░░░░░░░░░░" {
		t.Errorf("negative pct: %q", got)
	}
	if got := bar(150, 10); got != "██████████" {
		t.Errorf("over 100 pct: %q", got)
	}
	if got := bar(50, 10); got != "█████░░░░░" {
		t.Errorf("50 pct: %q", got)
	}
}

func TestOrBg(t *testing.T) {
	if got := orBg("#111", "#222"); got != "#111" {
		t.Errorf("cell wins: %q", got)
	}
	if got := orBg("", "#222"); got != "#222" {
		t.Errorf("falls back to row: %q", got)
	}
}

func TestClampLinesAndPadBlockTo(t *testing.T) {
	if got := clampLines("a\nb\nc", 0); got != "" {
		t.Errorf("h<=0: %q", got)
	}
	if got := clampLines("a\nb\nc", 2); got != "a\nb" {
		t.Errorf("truncate: %q", got)
	}
	if got := padBlockTo("a", 3); got != "a\n\n" {
		t.Errorf("grow: %q", got)
	}
	if got := padBlockTo("a\nb\nc", 2); got != "a\nb" {
		t.Errorf("shrink: %q", got)
	}
}

func TestFgAndPaint(t *testing.T) {
	// fg/paint memoise styles per colour and delegate rendering to lipgloss,
	// which may or may not emit ANSI codes depending on the terminal profile
	// detected in the test environment — so these calls exercise every
	// branch (empty vs set hex/bg, cache hit vs miss) without asserting on
	// the exact rendered bytes.
	if got := fg("", "plain"); got != "plain" {
		t.Errorf("empty hex passthrough: %q", got)
	}
	if got := fg(cRed, "x"); got == "" {
		t.Error("fg with a colour should still return the text")
	}
	if got := fg(cRed, "y"); got == "" { // second call hits the style cache
		t.Error("cached fg should still return the text")
	}
	if got := paint("", "", "plain"); got != "plain" {
		t.Errorf("no colours passthrough: %q", got)
	}
	if got := paint(cRed, "", "x"); got == "" {
		t.Error("fg-only paint should still return the text")
	}
	if got := paint("", cSel, "x"); got == "" {
		t.Error("bg-only paint should still return the text")
	}
	if got := paint(cRed, cSel, "x"); got == "" {
		t.Error("fg+bg paint should still return the text")
	}
	if got := paint(cRed, cSel, "x"); got == "" { // second call hits the style cache
		t.Error("cached paint should still return the text")
	}
}

// ---- test helpers used across this package's tests --------------------------

// newTestGitRepo creates a real git repo in a temp dir with one commit and
// returns its path.
func newTestGitRepo(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := gitCmd(t, dir, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", name+"@example.com")
	run("config", "user.name", name)
	writeAndCommit(t, dir, "README.md", "hello\n", "initial commit")
	return dir
}

func writeAndCommit(t *testing.T, repo, file, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := gitCmd(t, repo, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	if out, err := gitCmd(t, repo, "commit", "-q", "-m", msg).CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	return exec.Command("git", append([]string{"-C", dir}, args...)...)
}

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := gitCmd(t, repo, args...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return trimNL(string(out))
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
