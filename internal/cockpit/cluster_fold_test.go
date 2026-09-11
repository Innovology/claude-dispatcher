package cockpit

// cluster_fold_test.go covers `p` in the assignment editor: the key that says
// where a repo actually is.
//
// It exists because a row stopped being a folder. One row is one repository
// however many checkouts it has and whatever they are called, so the screen you
// assign from can no longer answer "which directory is this?" by being read —
// the fold is the answer, and an editor that cannot give it is an editor you
// assign blind from.

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

// clFoldFixture is a portfolio with one many-checkout repo in it.
func clFoldFixture(t *testing.T) model {
	t.Helper()
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	reposByProduct = map[string][]repoRef{
		clUnassigned: {
			{
				name: "player-app", forge: "gh", out: 3, last: "0d",
				path: "/home/dev/PlayerPulse_org/player-app/main",
				worktrees: []repoWorktree{
					{path: "/home/dev/PlayerPulse_org/player-app/main", branch: "main"},
					{path: "/home/dev/PlayerPulse_org/player-app/ci-scheduling", branch: "ci-scheduling"},
					{path: "/home/dev/PlayerPulse_org/player-app/r2w-wearables", branch: "r2w-wearables"},
				},
			},
			{
				name: "ordain", forge: "gh", out: 0, last: "4d",
				path:      "/home/dev/ord-ai-n",
				worktrees: []repoWorktree{{path: "/home/dev/ord-ai-n", branch: "main", main: true}},
			},
		},
	}
	m := newModel()
	m.cfg = &config.Config{Products: map[string][]string{}}
	m.clOpen = true
	return m
}

func TestClFoldTogglesWithP(t *testing.T) {
	m := clFoldFixture(t)
	rows := m.clRepos()
	if rows[0].name != "ordain" {
		// Both are unassigned, so they sort by name — this keeps the cursor
		// arithmetic below honest if the fixture ever changes.
		t.Fatalf("expected ordain first, got %q", rows[0].name)
	}

	m, _, handled := m.updateCluster("p")
	if !handled {
		t.Fatal("p should be the editor's key, not fall through to a lens")
	}
	if !m.clExpanded["ordain"] {
		t.Fatal("p should unfold the row under the cursor")
	}
	m, _, _ = m.updateCluster("p")
	if m.clExpanded["ordain"] {
		t.Error("p again should fold it back")
	}
}

// The fold survives the cursor moving on: unfolding two repos to compare where
// they sit is the reason it is keyed by name rather than held as one cursor.
func TestClFoldSurvivesCursorMoving(t *testing.T) {
	m := clFoldFixture(t)
	m, _, _ = m.updateCluster("p")
	m, _, _ = m.updateCluster("j")
	m, _, _ = m.updateCluster("p")
	if !m.clExpanded["ordain"] || !m.clExpanded["player-app"] {
		t.Errorf("both rows should stay unfolded, got %v", m.clExpanded)
	}
}

// What the fold says: the absolute path of the checkout the row acts in, then
// the other checkouts by name and branch.
func TestClFoldRendersPathAndCheckouts(t *testing.T) {
	m := clFoldFixture(t)
	m.clExpanded = map[string]bool{"player-app": true}
	body := strings.Join(m.clLeft(120, 40), "\n")

	if !strings.Contains(body, "/home/dev/PlayerPulse_org/player-app/main") {
		t.Errorf("the fold should carry the absolute path:\n%s", body)
	}
	if !strings.Contains(body, "3 checkouts") {
		t.Errorf("the fold should count every checkout:\n%s", body)
	}
	for _, want := range []string{"ci-scheduling", "r2w-wearables"} {
		if !strings.Contains(body, want) {
			t.Errorf("the fold should list %q:\n%s", want, body)
		}
	}
	// The acting checkout is named once, as the place the row works, and is not
	// repeated in the list of the others.
	if n := strings.Count(body, "this row works in main"); n != 1 {
		t.Errorf("expected the acting checkout named once, got %d:\n%s", n, body)
	}
}

// A folded row is one line, as it always was. The fold is opt-in per repo and
// must not cost a portfolio that never presses p.
func TestClFoldedRowIsUnchanged(t *testing.T) {
	m := clFoldFixture(t)
	before := len(m.clLeft(120, 40))
	m.clExpanded = map[string]bool{"ordain": true}
	after := len(m.clLeft(120, 40))
	if after <= before {
		t.Errorf("unfolding should add lines, got %d then %d", before, after)
	}
	// A single-checkout repo unfolds to its path and nothing else: there is no
	// "1 checkouts" line to be said about a repo that is one directory.
	body := strings.Join(m.clLeft(120, 40), "\n")
	if strings.Contains(body, "checkouts") {
		t.Errorf("a single-checkout repo should not count its checkouts:\n%s", body)
	}
}

// The window is taken over rendered lines, not rows: a repo unfolded far down a
// long list still has its own row on screen.
func TestClFoldKeepsTheCursorVisible(t *testing.T) {
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	var refs []repoRef
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		refs = append(refs, repoRef{
			name: "repo-" + n, forge: "gh", last: "1d", path: "/src/" + n,
			worktrees: []repoWorktree{
				{path: "/src/" + n, branch: "main", main: true},
				{path: "/src/" + n + "-wt", branch: "wip"},
			},
		})
	}
	reposByProduct = map[string][]repoRef{clUnassigned: refs}
	m := newModel()
	m.cfg = &config.Config{Products: map[string][]string{}}
	m.clOpen = true
	m.clRepo = 9
	m.clExpanded = map[string]bool{}
	for _, r := range refs {
		m.clExpanded[r.name] = true
	}

	body := strings.Join(m.clLeft(120, 14), "\n")
	if !strings.Contains(body, "repo-j") {
		t.Errorf("the selected row must be on screen:\n%s", body)
	}
}

func TestClElideKeepsBothEnds(t *testing.T) {
	const p = "/home/dev/Projects/Org/player-app/ci-scheduling"
	if got := clElide(p, 100); got != p {
		t.Errorf("a path that fits is not elided, got %q", got)
	}
	got := clElide(p, 30)
	if dispWidth(got) > 30 {
		t.Errorf("elided to %d columns, want at most 30: %q", dispWidth(got), got)
	}
	if !strings.HasPrefix(got, "/home") || !strings.HasSuffix(got, "ci-scheduling") {
		t.Errorf("both ends should survive, got %q", got)
	}
}
