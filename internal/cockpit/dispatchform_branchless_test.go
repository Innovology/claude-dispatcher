package cockpit

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
)

// overlayModel is the + overlay open over a product of two repos and one repo
// in no product.
func overlayModel(t *testing.T) model {
	t.Helper()
	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{
		Roots:    []string{seedRepoRoot(t, "shop-api", "shop-web", "blog")},
		Products: map[string][]string{"shop": {"shop-api", "shop-web"}},
	}
	m.lens = "products"
	m = press(m, "+")
	if m.dispatchForm == nil {
		t.Fatal("'+' did not open the dispatch form")
	}
	return m
}

// The overlay offers the product as a whole after its repos, so enter on an
// untouched list still picks the first repo as it always has; typing the
// product's name finds it.
func TestOverlayOffersAWholeProductAfterItsRepos(t *testing.T) {
	m := overlayModel(t)
	ts := m.dispatchForm.targets()
	if len(ts) != 4 || ts[0].product != "" || ts[3].product != "shop" || ts[3].repos != 2 {
		t.Fatalf("targets = %+v, want the three repos and then shop as a whole", ts)
	}
	m = typeStr(m, "shop")
	ts = m.dispatchForm.targets()
	if last := ts[len(ts)-1]; last.product != "shop" {
		t.Errorf("filtering on the product does not find its row: %+v", ts)
	}
	if !strings.Contains(stripAnsi(m.View()), "all 2 repos") {
		t.Error("the whole-product row does not say what it spans")
	}
}

// Picked whole, the product is a conversation with no branch: the feature step
// says so, enter goes straight past ROOT, and the hand-off is the product with
// no repo — never the branch launch.
func TestOverlayDispatchesAConversationAboutAWholeProduct(t *testing.T) {
	branched := captureLaunches(t)
	got := captureBranchless(t)
	m := overlayModel(t)
	m = typeStr(m, "shop")
	for range m.dispatchForm.targets() {
		m = press(m, "down")
	}
	m = press(m, "enter")
	df := m.dispatchForm
	if df.step != dispatchFeature || df.whole != "shop" || !df.noBranch() {
		t.Fatalf("after picking the product: step %d, whole %q, no branch %v", df.step, df.whole, df.noBranch())
	}
	m = typeStr(m, "shop roadmap")
	if out := stripAnsi(m.View()); !strings.Contains(out, "no branch") || strings.Contains(out, "the branch will be") {
		t.Errorf("the feature step promises a branch:\n%s", out)
	}
	m = press(m, "enter")
	if m.dispatchForm.step != dispatchMode {
		t.Fatalf("with no branch, enter on the feature went to step %d, want mode (ROOT has nothing to cut)", m.dispatchForm.step)
	}
	if out := stripAnsi(m.View()); !strings.Contains(out, "esc → feature") {
		t.Errorf("the mode step points esc at a ROOT step it skipped:\n%s", out)
	}
	if m = press(m, "esc"); m.dispatchForm.step != dispatchFeature {
		t.Fatalf("esc from mode with no branch went to step %d, want feature", m.dispatchForm.step)
	}
	m = press(m, "enter")
	for _, k := range []string{"enter", "enter", "enter", "enter"} { // mode, model, account, fan out
		m = press(m, k)
	}
	m = typeStr(m, "where is shop going?")
	m = press(m, "enter")
	if got.n != 1 || branched.n != 0 || got.product != "shop" || got.repo != "" || got.feature != "shop roadmap" {
		t.Fatalf("handed off branchless %+v, branch %d", got, branched.n)
	}
}

// On a repo, the feature step has a third line: tab reaches BRANCH, space flips
// it, and the prefix is skipped once there is no branch to prefix. The hand-off
// is a conversation that starts on that repo.
func TestOverlayBranchNoneOnARepo(t *testing.T) {
	got := captureBranchless(t)
	m := overlayModel(t)
	m = typeStr(m, "shop-api")
	m = press(m, "enter")
	m = typeStr(m, "api look")
	m = press(m, "tab") // prefix
	m = press(m, "tab") // branch
	if !m.dispatchForm.branchFocus {
		t.Fatal("two tabs from the feature did not reach BRANCH")
	}
	m = press(m, " ")
	if !m.dispatchForm.noBranch() {
		t.Fatal("space on BRANCH did not choose no branch")
	}
	if m = press(m, "tab"); !m.dispatchForm.branchFocus && m.dispatchForm.prefixFocus {
		t.Error("tab with no branch went to the prefix")
	}
	m = press(m, "enter")
	if m.dispatchForm.step != dispatchMode {
		t.Fatalf("step %d, want mode", m.dispatchForm.step)
	}
	for range 4 {
		m = press(m, "enter")
	}
	m = typeStr(m, "how does auth work here?")
	m = press(m, "enter")
	if got.n != 1 || got.repo != "shop-api" || got.product != "" || got.mode != dispatchpkg.DefaultMode {
		t.Errorf("handed off %+v, want shop-api as the focus", got)
	}
}
