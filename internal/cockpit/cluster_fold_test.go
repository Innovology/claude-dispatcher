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
	"os"
	"path/filepath"
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

// A fold survives the cursor moving on, which is why it is keyed by name rather
// than held as one cursor: comparing where two repos sit is a thing you do.
//
// esc is the step that makes it possible. While a fold has the keyboard j is
// its own — that is the whole point of the focus — so letting go first is how
// the repo cursor moves with the fold still on screen.
func TestClFoldSurvivesCursorMoving(t *testing.T) {
	m := clFoldFixture(t)
	m, _, _ = m.updateCluster("p")   // unfold ordain, fold takes the keys
	m, _, _ = m.updateCluster("esc") // let go, ordain stays open
	m, _, _ = m.updateCluster("j")   // now j moves the repo cursor again
	m, _, _ = m.updateCluster("p")   // unfold player-app
	if !m.clExpanded["ordain"] || !m.clExpanded["player-app"] {
		t.Errorf("both rows should stay unfolded, got %v", m.clExpanded)
	}
	if m.clFoldRow != "player-app" {
		t.Errorf("the keyboard should be on the newly unfolded row, got %q", m.clFoldRow)
	}
}

// With no fold focused, j/k move between repos as they always did.
func TestClRepoCursorUnaffectedWithoutAFold(t *testing.T) {
	m := clFoldFixture(t)
	m, _, _ = m.updateCluster("j")
	if m.clRepo != 1 {
		t.Errorf("j should move the repo cursor when no fold has the keys, got %d", m.clRepo)
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
	for _, want := range []string{"main", "ci-scheduling", "r2w-wearables"} {
		if !strings.Contains(body, want) {
			t.Errorf("the fold should list %q:\n%s", want, body)
		}
	}
	// ● marks the checkout the row acts in. It is a mark on the list rather than
	// a sentence beside it, because the list is the thing you move a cursor
	// through to change it.
	if !strings.Contains(body, "●") {
		t.Errorf("the acting checkout should be marked in the list:\n%s", body)
	}
	if !strings.Contains(body, "chosen · p to change") {
		t.Errorf("an unpinned row should say the choice was automatic:\n%s", body)
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

// ---- picking the checkout a repo works in -----------------------------------
//
// The automatic choice knows three spellings of a trunk — the main worktree,
// main, master. A repo that merges into `dev` matches none of them, so its row
// would read a branch nobody ships from. These cover the way out of that.

// clPinFixture has a repo whose trunk is `dev`, sitting where the automatic
// choice would never look: after the `main` checkout, on a branch named nothing
// like a trunk.
func clPinFixture(t *testing.T) (model, string) {
	t.Helper()
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	home := t.TempDir()
	t.Setenv("HOME", home)

	reposByProduct = map[string][]repoRef{
		clUnassigned: {{
			name: "playerpulse", forge: "gh", last: "7d",
			path: "/src/playerpulse",
			worktrees: []repoWorktree{
				{path: "/src/playerpulse", branch: "main", main: true},
				{path: "/src/playerpulse-dev", branch: "dev"},
				{path: "/src/playerpulse-o6", branch: "o6"},
			},
		}},
	}
	m := newModel()
	m.cfg = &config.Config{Products: map[string][]string{}}
	m.clOpen = true
	return m, home
}

// p opens the fold ON the checkout the row currently works in, not at the top:
// with sixty-nine of them, "which one is it now" must not be a thing you scroll
// to find.
func TestClFoldOpensOnTheActingCheckout(t *testing.T) {
	m, _ := clPinFixture(t)
	m.clRepo = 0
	m, _, _ = m.updateCluster("p")
	if m.clFoldRow != "playerpulse" {
		t.Fatalf("p should give the fold the keyboard, got %q", m.clFoldRow)
	}
	if m.clFoldIdx != 0 {
		t.Errorf("fold should open on the acting checkout (index 0), got %d", m.clFoldIdx)
	}
}

// While the fold has the keyboard, j/k are its own — moving the repo cursor as
// well would make one key mean two things.
func TestClFoldTakesTheArrowKeys(t *testing.T) {
	m, _ := clPinFixture(t)
	m, _, _ = m.updateCluster("p")
	before := m.clRepo
	m, _, _ = m.updateCluster("j")
	if m.clFoldIdx != 1 {
		t.Errorf("j should move inside the fold, got idx %d", m.clFoldIdx)
	}
	if m.clRepo != before {
		t.Errorf("j should not also move the repo cursor, got %d want %d", m.clRepo, before)
	}
	m, _, _ = m.updateCluster("k")
	if m.clFoldIdx != 0 {
		t.Errorf("k should move back, got %d", m.clFoldIdx)
	}
	// esc lets go of the fold without closing it, so what you were reading stays.
	m, _, _ = m.updateCluster("esc")
	if m.clFoldRow != "" {
		t.Errorf("esc should release the fold, got %q", m.clFoldRow)
	}
	if !m.clExpanded["playerpulse"] {
		t.Error("esc should leave the fold open — p is what closes it")
	}
	if !m.clOpen {
		t.Error("esc inside a fold must not close the editor")
	}
}

// enter on a checkout writes it to [checkouts], which is what Repo.Path is read
// through on the next load.
func TestClFoldEnterPinsTheCheckout(t *testing.T) {
	m, home := clPinFixture(t)
	m, _, _ = m.updateCluster("p")
	m, _, _ = m.updateCluster("j") // onto dev
	m, cmd, _ := m.updateCluster("enter")
	if cmd == nil {
		t.Fatal("pinning should return a command carrying the outcome")
	}
	if got := m.cfg.Checkouts["playerpulse"]; got != "/src/playerpulse-dev" {
		t.Errorf("config should hold the picked checkout, got %q", got)
	}
	body, err := os.ReadFile(filepath.Join(home, ".config", "claude-dispatcher", "config.toml"))
	if err != nil {
		t.Fatalf("config should have been written: %v", err)
	}
	if !strings.Contains(string(body), "playerpulse-dev") {
		t.Errorf("the pin should reach the file:\n%s", body)
	}
	if msg, ok := cmd().(actionMsg); !ok || !strings.Contains(msg.notice, "playerpulse-dev") {
		t.Errorf("the notice should name the checkout, got %#v", cmd())
	}
}

// A pin needs a handle on the inside: choosing the one already pinned removes
// the entry, because nothing else on this screen does and "choose automatically
// again" has to be reachable.
func TestClFoldEnterOnThePinnedOneClearsIt(t *testing.T) {
	m, _ := clPinFixture(t)
	m.cfg.Checkouts = map[string]string{"playerpulse": "/src/playerpulse-dev"}
	// The snapshot reflects that pin: the row acts in dev and says it is pinned.
	reposByProduct[clUnassigned][0].path = "/src/playerpulse-dev"
	reposByProduct[clUnassigned][0].pinned = true

	m, _, _ = m.updateCluster("p")
	if m.clFoldIdx != 1 {
		t.Fatalf("fold should open on the pinned checkout, got %d", m.clFoldIdx)
	}
	m, cmd, _ := m.updateCluster("enter")
	if _, still := m.cfg.Checkouts["playerpulse"]; still {
		t.Error("enter on the pinned checkout should clear the pin")
	}
	if msg, ok := cmd().(actionMsg); !ok || !strings.Contains(msg.notice, "chooses its checkout again") {
		t.Errorf("the notice should say it is automatic again, got %#v", cmd())
	}
}

// A pinned row says so, so a decision is visible before it is changed.
func TestClFoldSaysPinnedRatherThanChosen(t *testing.T) {
	m, _ := clPinFixture(t)
	reposByProduct[clUnassigned][0].pinned = true
	m.clExpanded = map[string]bool{"playerpulse": true}
	body := strings.Join(m.clLeft(120, 40), "\n")
	if !strings.Contains(body, "pinned · p to change") {
		t.Errorf("a pinned row should say so:\n%s", body)
	}
}

// The fold list windows around its own cursor. A repo with sixty-nine checkouts
// and a fixed first-eight would put the one you are choosing off screen.
func TestClFoldWindowsAroundTheCursor(t *testing.T) {
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	var wts []repoWorktree
	for i := range 69 {
		wts = append(wts, repoWorktree{path: "/src/big/wt-" + itoa(i), branch: "b" + itoa(i)})
	}
	reposByProduct = map[string][]repoRef{
		clUnassigned: {{name: "big", forge: "gh", last: "1d", path: wts[0].path, worktrees: wts}},
	}
	m := newModel()
	m.cfg = &config.Config{Products: map[string][]string{}}
	m.clOpen = true
	m.clExpanded = map[string]bool{"big": true}
	m.clFoldRow, m.clFoldIdx = "big", 40

	body := strings.Join(m.clLeft(120, 40), "\n")
	if !strings.Contains(body, "wt-40") {
		t.Errorf("the fold cursor must be on screen:\n%s", body)
	}
	if strings.Contains(body, "wt-10 ") {
		t.Errorf("the window should have moved off the top of the list:\n%s", body)
	}
}
