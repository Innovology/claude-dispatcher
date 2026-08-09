package cockpit

// assign_test.go covers the product creation flow end to end: the products
// lens's onboarding, the `a`/`n` overlay's keys, and — the part that matters —
// that every act lands in config.toml, because that file is the only thing that
// remembers a product exists.

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// installInventory fills repoInventory (and the matching empty portfolio) for
// an assign test, restoring the package vars afterwards.
func installInventory(t *testing.T, rows ...repoRow) {
	t.Helper()
	prevInv, prevProducts := repoInventory, products
	t.Cleanup(func() { repoInventory, products = prevInv, prevProducts })
	repoInventory = rows
	products = []product{{name: "unassigned", repos: nRepos(len(rows))}}
}

// assignModel builds a cockpit sitting on the products lens with a real config
// file behind it (HOME is redirected at the temp dir, so config.Save writes
// there), plus the given repo inventory.
func assignModel(t *testing.T, rows ...repoRow) model {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // config.Dir uses os.UserHomeDir on windows too
	installInventory(t, rows...)

	m := newModel()
	m.width, m.height = 150, 34
	m.lens = "products"
	m.cfg = &config.Config{Roots: []string{home}}
	return m
}

// typeInto feeds a string to the model one key message at a time, the way a
// terminal delivers it.
func typeInto(m model, s string) model {
	for _, r := range s {
		m.key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		mm, _, _ := m.updateAssign(string(r))
		m = mm
	}
	return m
}

// pressKey drives a key through the whole router with the untouched key message
// attached, the way a terminal delivers it.
func pressKey(m model, k string) model {
	m.key = keyMsgFor(k)
	mm, _ := m.handleKey(k)
	return mm.(model)
}

func keyMsgFor(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "space", " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// ---- the creation flow ------------------------------------------------------

// The headline path: mark two repos, press n, name it, enter. The product must
// exist in config.toml — on disk, not just in memory — with exactly those repos.
func TestAssignCreatesProductAndWritesConfig(t *testing.T) {
	m := assignModel(t,
		repoRow{name: "alpha-api", forge: "gh"},
		repoRow{name: "alpha-web", forge: "gh"},
		repoRow{name: "zeta-tools", forge: "ado"},
	)

	m = pressKey(m, "n") // opens the overlay with the name prompt already up
	if m.assign == nil || !m.assign.naming {
		t.Fatal("n on the products lens should open the overlay naming a product")
	}
	// Mark the two alpha repos: esc leaves the prompt but keeps the overlay.
	m = pressKey(m, "esc")
	if m.assign == nil {
		t.Fatal("esc out of the name prompt closed the whole overlay")
	}
	m = pressKey(m, "space") // marks alpha-api, moves to alpha-web
	m = pressKey(m, "space") // marks alpha-web
	if got := len(m.assign.marked); got != 2 {
		t.Fatalf("marked %d repos, want 2", got)
	}

	m = pressKey(m, "n")
	m = typeInto(m, "alpha")
	m = pressKey(m, "enter")

	if m.assign.naming {
		t.Error("enter should close the name prompt")
	}
	got := m.cfg.Products["alpha"]
	if len(got) != 2 || got[0] != "alpha-api" || got[1] != "alpha-web" {
		t.Fatalf("cfg.Products[alpha] = %v, want [alpha-api alpha-web]", got)
	}
	if !strings.Contains(m.notice, "created \"alpha\"") || !strings.Contains(m.notice, "2 repos") {
		t.Errorf("notice = %q", m.notice)
	}

	// The file is the record. Read it back the way the next launch would.
	saved, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load after create: %v", err)
	}
	if len(saved.Products["alpha"]) != 2 {
		t.Errorf("config.toml did not keep the product: %v", saved.Products)
	}
	if saved.Roots[0] != m.cfg.Roots[0] {
		t.Errorf("saving the product clobbered the scan roots: %v", saved.Roots)
	}

	// The overlay must show the change immediately, without waiting for the
	// next snapshot (a git/gh round trip away).
	for _, r := range repoInventory {
		if r.name == "alpha-api" && r.product != "alpha" {
			t.Errorf("inventory not updated in place: %+v", r)
		}
	}
}

// With nothing marked, an act applies to the row under the cursor — so the
// common case (this one, into that product) needs no marking at all.
func TestAssignFallsBackToCursorRow(t *testing.T) {
	m := assignModel(t,
		repoRow{name: "one", forge: "gh"},
		repoRow{name: "two", forge: "gh"},
	)
	m = pressKey(m, "a")
	m = pressKey(m, "j") // cursor on "two"
	m = pressKey(m, "n")
	m = typeInto(m, "solo")
	m = pressKey(m, "enter")

	if got := m.cfg.Products["solo"]; len(got) != 1 || got[0] != "two" {
		t.Fatalf("cfg.Products[solo] = %v, want [two]", got)
	}
}

// Naming an existing product moves repos into it rather than creating a second
// one, and says so.
func TestAssignNamingAnExistingProductMovesIn(t *testing.T) {
	m := assignModel(t,
		repoRow{name: "one", forge: "gh", product: "shop"},
		repoRow{name: "two", forge: "gh"},
	)
	m.cfg.Products = map[string][]string{"shop": {"one"}}

	m = pressKey(m, "a")
	m = pressKey(m, "n")
	m = typeInto(m, "shop")
	m = pressKey(m, "enter")

	got := m.cfg.Products["shop"]
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("cfg.Products[shop] = %v, want [one two]", got)
	}
	if !strings.Contains(m.notice, "already existed") {
		t.Errorf("notice = %q, want it to say the product already existed", m.notice)
	}
}

// An empty name is a cancel, not a product called "".
func TestAssignEmptyNameCreatesNothing(t *testing.T) {
	m := assignModel(t, repoRow{name: "one", forge: "gh"})
	m = pressKey(m, "n")
	m = typeInto(m, "   ")
	m = pressKey(m, "enter")

	if len(m.cfg.Products) != 0 {
		t.Fatalf("blank name created %v", m.cfg.Products)
	}
	if m.assign.naming {
		t.Error("enter on a blank name should close the prompt")
	}
}

// enter with the cursor in the product rail moves the marked repos into the
// selected product.
func TestAssignEnterMovesIntoSelectedProduct(t *testing.T) {
	m := assignModel(t,
		repoRow{name: "loose", forge: "gh"},
		repoRow{name: "taken", forge: "gh", product: "beta"},
	)
	m.cfg.Products = map[string][]string{"alpha": {}, "beta": {"taken"}}

	m = pressKey(m, "a")
	m = pressKey(m, "tab") // into the product rail; alpha is first
	if m.assign.pane != "products" {
		t.Fatalf("tab did not move to the product pane: %q", m.assign.pane)
	}
	m = pressKey(m, "enter")

	if got := m.cfg.Products["alpha"]; len(got) != 1 || got[0] != "loose" {
		t.Fatalf("cfg.Products[alpha] = %v, want [loose]", got)
	}
	if !strings.Contains(m.notice, "→ alpha") {
		t.Errorf("notice = %q", m.notice)
	}
}

// u moves repos out; U moves everything out. Neither deletes the product name —
// it survives in config.toml so it can be filled again.
func TestAssignUnassign(t *testing.T) {
	m := assignModel(t,
		repoRow{name: "a1", forge: "gh", product: "alpha"},
		repoRow{name: "a2", forge: "gh", product: "alpha"},
		repoRow{name: "b1", forge: "gh", product: "beta"},
	)
	m.cfg.Products = map[string][]string{"alpha": {"a1", "a2"}, "beta": {"b1"}}

	m = pressKey(m, "a")
	m = pressKey(m, "u") // cursor is on the first row
	if got := len(m.cfg.Products["alpha"]); got != 1 {
		t.Fatalf("after u, alpha has %d repos, want 1: %v", got, m.cfg.Products["alpha"])
	}

	m = pressKey(m, "U")
	for p, names := range m.cfg.Products {
		if len(names) != 0 {
			t.Errorf("after U, %s still holds %v", p, names)
		}
	}
	if _, ok := m.cfg.Products["alpha"]; !ok {
		t.Error("U deleted the product name; it should survive to be filled again")
	}
	if !strings.Contains(m.notice, "every repo unassigned") {
		t.Errorf("notice = %q", m.notice)
	}
}

// The overlay owns the keyboard: a stray key must not reach the lens behind it.
// The lens digits are the one exception — they close it and still switch lens.
func TestAssignSwallowsKeysExceptLensDigits(t *testing.T) {
	m := assignModel(t, repoRow{name: "one", forge: "gh"})
	m = pressKey(m, "a")

	m = pressKey(m, "?")
	if m.helpOpen {
		t.Error("? reached the help sheet through the overlay")
	}
	m = pressKey(m, ":")
	if m.paletteOpen {
		t.Error(": reached the palette through the overlay")
	}

	m = pressKey(m, "4")
	if m.assign != nil {
		t.Error("a lens digit should close the overlay")
	}
	if m.lens != "queue" {
		t.Errorf("lens = %q, want queue", m.lens)
	}
}

// While naming, every printable key is text — including the ones that are
// actions a keystroke earlier.
func TestAssignNamingSwallowsActionKeys(t *testing.T) {
	m := assignModel(t, repoRow{name: "one", forge: "gh"})
	m = pressKey(m, "n")
	m = typeInto(m, "aunq4")
	if m.assign.newName != "aunq4" {
		t.Fatalf("newName = %q, want %q", m.assign.newName, "aunq4")
	}
	if m.assign == nil || m.lens != "products" {
		t.Error("typing a name moved the cockpit")
	}
}

// A burst of typing arrives as one key message; the name prompt must keep all
// of it (the same bug applyEdit exists to prevent elsewhere).
func TestAssignNamingKeepsWholeBurst(t *testing.T) {
	m := assignModel(t, repoRow{name: "one", forge: "gh"})
	m = pressKey(m, "n")
	burst := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("northwind")}
	m.key = burst
	mm, _, _ := m.updateAssign(burst.String())
	if mm.assign.newName != "northwind" {
		t.Errorf("newName = %q, want northwind", mm.assign.newName)
	}
}

// With no config there is nothing to write to; the flow says so rather than
// pretending it worked.
func TestAssignWithoutConfigRefuses(t *testing.T) {
	installInventory(t, repoRow{name: "one", forge: "gh"})
	m := newModel()
	m.width, m.height = 150, 34
	m.lens = "products"

	m = pressKey(m, "n")
	m = typeInto(m, "alpha")
	m = pressKey(m, "enter")
	if !strings.Contains(m.notice, "no config") {
		t.Errorf("notice = %q, want it to name the missing config", m.notice)
	}
}

// ---- the lens around it -----------------------------------------------------

// Nothing assigned: the lens must teach what a product is and offer the two
// keys, not draw a one-row table of the unassigned bucket.
func TestProductsOnboardingWhenNothingAssigned(t *testing.T) {
	m := assignModel(t,
		repoRow{name: "alpha-api", forge: "gh", out: 2, last: "4m"},
		repoRow{name: "alpha-web", forge: "gh", last: "3d"},
	)
	out := m.View()
	for _, want := range []string{"no products yet", "alpha-api", "alpha-web", "name a product", "LAST COMMIT"} {
		if !strings.Contains(out, want) {
			t.Errorf("onboarding is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "where the factory is stuck") {
		t.Error("onboarding should not draw the portfolio table's sections")
	}
	// The header must not count the unassigned bucket as a product.
	if strings.Contains(out, "1 product") {
		t.Error("an empty portfolio claimed a product in the header")
	}
}

// With no repos at all, the answer is about scan roots, not about products.
func TestProductsOnboardingWithNoRepos(t *testing.T) {
	m := assignModel(t)
	out := m.View()
	if !strings.Contains(out, "no repos found") {
		t.Errorf("expected the no-repos empty state:\n%s", out)
	}
	// …and the overlay must refuse to open onto an empty list.
	m = pressKey(m, "a")
	if m.assign != nil {
		t.Error("a opened the assign overlay with no repos to assign")
	}
	if !strings.Contains(m.notice, "scan roots") {
		t.Errorf("notice = %q", m.notice)
	}
}

// Once something is assigned the portfolio table comes back, with the loose
// count in the header so repos discovered later do not go unnoticed.
func TestProductsShowsLooseCountOncePopulated(t *testing.T) {
	m := assignModel(t,
		repoRow{name: "a1", forge: "gh", product: "alpha"},
		repoRow{name: "loose", forge: "gh"},
	)
	products = []product{
		{name: "alpha", repos: "1 repo", forge: "github"},
		{name: "unassigned", repos: "1 repo"},
	}
	out := m.View()
	if !strings.Contains(out, "1 repo is in no product") {
		t.Errorf("expected the loose-repo call-out:\n%s", out)
	}
	if !strings.Contains(out, "assign repos to products") {
		t.Errorf("expected the a/n affordance line:\n%s", out)
	}
}

// The suggestion is a guess from repo names, offered — never an assignment made.
func TestProductsSuggestion(t *testing.T) {
	got := productsSuggestion([]repoRow{
		{name: "cortiva-api"}, {name: "cortiva-web"}, {name: "cortiva-infra"}, {name: "cortiva-docs"},
		{name: "solo"},
	})
	if !strings.Contains(got, "cortiva-api, cortiva-web and cortiva-infra") {
		t.Errorf("suggestion = %q", got)
	}
	// Two repos with a shared stem is still a group; one is not.
	if got := productsSuggestion([]repoRow{{name: "one"}, {name: "two"}}); !strings.Contains(got, "ship together") {
		t.Errorf("no shared stem: %q", got)
	}
	if got := productsSuggestion(nil); !strings.Contains(got, "ship together") {
		t.Errorf("empty: %q", got)
	}
}

// ---- the collector's half ---------------------------------------------------

// A product whose repos have all been moved out must not keep the portfolio
// looking populated — that would hide the onboarding from the person who needs
// it — and a record still claiming it reads as unassigned.
func TestCollectProductsSkipsEmptyProducts(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Roots: []string{dir}, Products: map[string][]string{
		"filled": {"repo-a"},
		"empty":  {},
	}}
	ctx := &collectCtx{cfg: cfg}
	var s snapshot
	collectProducts(ctx, &s)

	for _, p := range s.products {
		if p.name == "empty" {
			t.Errorf("an empty product was rolled up: %+v", s.products)
		}
	}
	if _, ok := s.reposByProduct["filled"]; !ok {
		t.Errorf("the filled product went missing: %v", s.productOrder)
	}
	if got := ctx.productFor(&state.Dispatch{Product: "empty"}); got != "unassigned" {
		t.Errorf("record on an empty product: got %q, want unassigned", got)
	}
}

// collectStaleQueue builds the repo inventory every discovered repo appears in,
// product or not — the thing reposByProduct cannot describe.
func TestCollectStaleQueueBuildsInventory(t *testing.T) {
	repoPath := newTestGitRepo(t, "inv")
	root := t.TempDir()
	// Discover walks roots, so hand the collector the repo directly.
	ctx := &collectCtx{
		cfg:   &config.Config{Roots: []string{root}},
		repos: []repos.Repo{{Name: "inv", Path: repoPath, Product: "shop"}},
	}
	var s snapshot
	collectStaleQueue(ctx, &s)

	if len(s.repoInventory) != 1 {
		t.Fatalf("inventory = %+v, want one row", s.repoInventory)
	}
	got := s.repoInventory[0]
	if got.name != "inv" || got.product != "shop" {
		t.Errorf("row = %+v", got)
	}
	if got.days != 0 || got.last == "" {
		t.Errorf("a repo committed to just now should have a readable age: %+v", got)
	}
	// A fresh repo is not stale, and its inventory row is still there.
	if len(s.staleRepos) != 0 {
		t.Errorf("staleRepos = %+v, want none", s.staleRepos)
	}
}

// A repo git cannot answer for keeps its row, with the age marked unknown
// rather than reported as "0d".
func TestCollectStaleQueueUnknownCommitAge(t *testing.T) {
	ctx := &collectCtx{
		cfg:   &config.Config{},
		repos: []repos.Repo{{Name: "ghost", Path: filepathJoinNonexistent()}},
	}
	var s snapshot
	collectStaleQueue(ctx, &s)

	if len(s.repoInventory) != 1 {
		t.Fatalf("inventory = %+v", s.repoInventory)
	}
	if got := s.repoInventory[0]; got.days != -1 || got.last != "" {
		t.Errorf("row = %+v, want an unknown age", got)
	}
	if len(s.staleRepos) != 0 {
		t.Errorf("an unknown age must not read as stale: %+v", s.staleRepos)
	}
}

func filepathJoinNonexistent() string {
	return string(os.PathSeparator) + "nonexistent-repo-path-xyz"
}
