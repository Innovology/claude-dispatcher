package cockpit

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
)

// branchlessCall is the last conversation a form handed off, and how many.
type branchlessCall struct {
	product, repo, feature, prompt string
	mode                           dispatchpkg.Mode
	model                          dispatchpkg.Model
	fanOut                         bool
	account                        string
	n                              int
}

// captureBranchless replaces the seam both forms hand a branchless dispatch
// through, the way captureLaunches does for a branch dispatch.
func captureBranchless(t *testing.T) *branchlessCall {
	t.Helper()
	got := &branchlessCall{}
	prev := launchBranchless
	launchBranchless = func(_ *config.Config, product, repo, feature, prompt string, mode dispatchpkg.Mode,
		mdl dispatchpkg.Model, fanOut bool, acct string) tea.Cmd {
		*got = branchlessCall{product: product, repo: repo, feature: feature, prompt: prompt, mode: mode,
			model: mdl, fanOut: fanOut, account: acct, n: got.n + 1}
		return func() tea.Msg { return nil }
	}
	t.Cleanup(func() { launchBranchless = prev })
	return got
}

// dxProductModel is the dx form over a product of two repos and one repo in
// no product.
func dxProductModel(t *testing.T) model {
	t.Helper()
	m := newModel()
	m.cfg = &config.Config{
		Roots:    []string{seedRepoRoot(t, "player-app", "playerpulse", "scratch")},
		Products: map[string][]string{"pp": {"player-app", "playerpulse"}},
	}
	m.width, m.height = 130, 40
	return m.dxOpen("")
}

// dxCursorTo puts WHERE's cursor on the row match picks.
func dxCursorTo(t *testing.T, m model, match func(dxRepoRow) bool) model {
	t.Helper()
	for i, r := range m.dxRows() {
		if match(r) {
			m.dxRepo = i
			return m
		}
	}
	t.Fatalf("no row matches among %+v", m.dxRows())
	return m
}

// A conversation about a whole product is picked where everything else is,
// on WHERE: one row per product with more than one repo, after its repos.
// After, because an untouched form's cursor sits on the first row: leading the
// group, the product row was what a form opened on, and pressing through it
// sent a conversation where the form had always cut a branch — found running
// the real binary, not by a test. A product of one repo needs no such row —
// its repo is the whole of it — and the repos in no product have no product to
// speak of.
func TestDXOffersAWholeProductOnItsOwnRow(t *testing.T) {
	m := dxProductModel(t)
	rows := m.dxRows()
	var order []string
	for _, r := range rows {
		name := r.repo
		if r.whole {
			name = "ALL:" + r.product
		}
		order = append(order, name)
	}
	if got, want := strings.Join(order, " "), "player-app playerpulse ALL:pp scratch"; got != want {
		t.Errorf("WHERE = %s, want %s", got, want)
	}
	// It is a way to dispatch, not a repo, and the count says how many repos.
	if got := m.dxRepoCount(); !strings.HasPrefix(got, "3 repos") {
		t.Errorf("count = %q, want 3 repos", got)
	}
	// And so the form opens on a repo, cutting a branch, as it always did.
	if m.dxNoBranch() {
		t.Error("an untouched form opens on a dispatch with no branch")
	}
	// Typing the product finds it, as it finds the product's repos.
	m.dxFilter = "pp"
	if rows := m.dxRows(); len(rows) == 0 || !rows[len(rows)-1].whole {
		t.Errorf("filtering on the product does not find its row: %+v", rows)
	}
}

// BRANCH none on a repo is a conversation that starts there and reaches the
// rest of its product. It goes out through the branchless launch, never the
// branch one, and ROOT and PREFIX — which only shape a branch — are not
// consulted: a root that would be refused for a branch dispatch does not stop
// a dispatch that cuts nothing.
func TestDXBranchNoneDispatchesAConversation(t *testing.T) {
	branched := captureLaunches(t)
	got := captureBranchless(t)

	m := dxCursorTo(t, dxProductModel(t), func(r dxRepoRow) bool { return r.repo == "player-app" })
	m.dxTitle, m.dxWhat = "byga sitrep", "what is the status of byga?"
	m.dxRoot = "no-such-branch"
	m.dxField = dxBranchF
	m, _ = m.dxKey(" ")
	if !m.dxBranching.None() {
		t.Fatal("space on BRANCH did not choose no branch")
	}
	if !strings.Contains(m.dxSummary(), "no branch") || !strings.Contains(m.dxTitleHint(), "no branch") {
		t.Errorf("summary %q / title hint %q do not say there is no branch", m.dxSummary(), m.dxTitleHint())
	}

	m, cmd := m.dxSubmit()
	if cmd == nil || got.n != 1 || branched.n != 0 {
		t.Fatalf("submit: branchless %d, branch %d, notice %q", got.n, branched.n, m.notice)
	}
	if got.repo != "player-app" || got.product != "" || got.feature != "byga sitrep" {
		t.Errorf("handed off %+v, want player-app as the focus", got)
	}
	if !strings.Contains(m.notice, "no branch") {
		t.Errorf("notice = %q", m.notice)
	}
}

// The whole-product row is branchless by construction: there is no one repo
// to cut a branch in, so BRANCH reads none there and will not flip, and submit
// hands off the product with no repo.
func TestDXAWholeProductRowIsAlwaysBranchless(t *testing.T) {
	got := captureBranchless(t)
	m := dxCursorTo(t, dxProductModel(t), func(r dxRepoRow) bool { return r.whole })
	m.dxTitle, m.dxWhat = "pp roadmap", "where is pp going?"
	if !m.dxNoBranch() || m.dxBranchSel() != 1 {
		t.Fatal("a whole-product row offers a branch")
	}
	m.dxField = dxBranchF
	m, _ = m.dxKey(" ")
	if !m.dxNoBranch() || !strings.Contains(m.notice, "pick a repo") {
		t.Errorf("BRANCH flipped on a whole product, or said nothing: %q", m.notice)
	}
	if _, _ = m.dxSubmit(); got.n != 1 || got.product != "pp" || got.repo != "" {
		t.Errorf("handed off %+v, want pp with no repo", got)
	}
}

// With no branch, ROOT and PREFIX have nothing to say, so the field walk goes
// past them rather than stopping on two lines that do nothing.
func TestDXFieldWalkSkipsRootAndPrefixWithNoBranch(t *testing.T) {
	m := dxCursorTo(t, dxProductModel(t), func(r dxRepoRow) bool { return r.repo == "player-app" })
	m.dxField = dxBranchF
	if m, _ = m.dxKey("tab"); m.dxField != dxRootF {
		t.Fatalf("with a branch, tab from BRANCH went to %d, want ROOT", m.dxField)
	}
	m.dxField = dxBranchF
	m, _ = m.dxKey(" ")
	for _, k := range []string{"tab", "enter", "down"} {
		m.dxField = dxBranchF
		if next, _ := m.dxKey(k); next.dxField != dxModeF {
			t.Errorf("%s from BRANCH with no branch went to %d, want MODE", k, next.dxField)
		}
	}
	m.dxField = dxModeF
	for _, k := range []string{"shift+tab", "up"} {
		if next, _ := m.dxKey(k); next.dxField != dxBranchF {
			t.Errorf("%s from MODE with no branch went to %d, want BRANCH", k, next.dxField)
		}
	}
}

// A form that is closed and opened again cuts a branch: no branch is a choice
// made for one dispatch, never a mode the form is left in.
func TestDXResetCutsABranchAgain(t *testing.T) {
	m := dxProductModel(t)
	m.dxBranching = dispatchpkg.BranchNone
	if m = m.dxReset(); m.dxBranching.None() {
		t.Error("reset left the form with no branch")
	}
}

// The row that goes on the table at once says what is coming: no branch where
// the branch would be, filed under the product — which for a whole product is
// the only thing there is to file it by — and "all repos" where it names none.
// And the form it closed over had shown no prefix it was not going to use.
func TestDXABranchlessDispatchShowsAsOneWhileItStarts(t *testing.T) {
	captureBranchless(t)
	m := dxCursorTo(t, dxProductModel(t), func(r dxRepoRow) bool { return r.whole })
	m.dxTitle, m.dxWhat = "pp roadmap", "where is pp going?"
	for _, ln := range strings.Split(stripAnsi(m.dxView(120, 38)), "\n") {
		if strings.Contains(ln, "PREFIX") && strings.Contains(ln, "feature/") {
			t.Errorf("PREFIX still shows a prefix with no branch: %q", ln)
		}
	}
	m, _ = m.dxSubmit()
	if len(m.pending) != 1 {
		t.Fatalf("pending = %+v", m.pending)
	}
	p := m.pending[0]
	if p.branch != "no branch" || p.product != "pp" || p.repo != "all repos" || !p.branchless {
		t.Errorf("pending row = %+v", p)
	}
}

// A scope that cannot be resolved by the time the launch runs — no config, a
// repo gone since the form was drawn — is a failed launch with its reason,
// which is what keeps the row on the table saying why (ADR 0009).
func TestLaunchingAConversationWithNowhereToGoSaysSo(t *testing.T) {
	if msg, _ := launchBranchlessCmd(nil, "pp", "", "x", "y", dispatchpkg.ModeAuto, dispatchpkg.DefaultModel, false, "")().(launchedMsg); !msg.failed {
		t.Errorf("no config: %+v", msg)
	}
	cfg := &config.Config{Roots: []string{seedRepoRoot(t, "player-app")}}
	msg, _ := launchBranchlessCmd(cfg, "", "gone", "x", "y", dispatchpkg.ModeAuto, dispatchpkg.DefaultModel, false, "")().(launchedMsg)
	if !msg.failed || !strings.Contains(msg.reason, "gone") {
		t.Errorf("a repo that is not there: %+v", msg)
	}
}
