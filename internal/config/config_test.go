package config

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ExpandHome("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("ExpandHome(~/x) = %q", got)
	}
	if got := ExpandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("absolute path must pass through, got %q", got)
	}
	if got := ExpandHome("~"); got != home {
		t.Errorf("ExpandHome(~) = %q, want home", got)
	}
}

func TestExpandedRootsDedupes(t *testing.T) {
	c := &Config{Roots: []string{"/a", "/a", "~/b", ""}}
	got := c.ExpandedRoots()
	if len(got) != 2 {
		t.Fatalf("expected 2 roots, got %v", got)
	}
	if got[0] != "/a" || !strings.HasSuffix(got[1], "/b") {
		t.Errorf("unexpected roots %v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	in := &Config{
		Roots: []string{"~/repos", "/work/other"},
		Products: map[string][]string{
			"acme shop": {"shop-api", "shop-web"},
		},
		DeployWorkflows: map[string]string{
			"shop-api": "Deploy production",
		},
	}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out.Roots, in.Roots) {
		t.Errorf("roots = %v, want %v", out.Roots, in.Roots)
	}
	if !slices.Equal(out.Products["acme shop"], in.Products["acme shop"]) {
		t.Errorf("products = %v, want %v", out.Products, in.Products)
	}
	if out.DeployWorkflows["shop-api"] != "Deploy production" {
		t.Errorf("deploy_workflows = %v", out.DeployWorkflows)
	}
}

// TestSaveLoadLinearTokens proves the per-product Linear table survives the
// hand-written template Save regenerates, alongside the unscoped key every
// product without an entry reads with.
func TestSaveLoadLinearTokens(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	in := &Config{
		Roots:        []string{"~/repos"},
		LinearAPIKey: "lin_api_default",
		Linear: map[string]string{
			"acme shop": "lin_api_acme",
			"bluefin":   "lin_api_bluefin",
		},
		Products: map[string][]string{"bluefin": {"bluefin-core"}},
	}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.LinearAPIKey != "lin_api_default" {
		t.Errorf("linear_api_key = %q", out.LinearAPIKey)
	}
	if out.Linear["acme shop"] != "lin_api_acme" || out.Linear["bluefin"] != "lin_api_bluefin" {
		t.Errorf("linear tokens = %v", out.Linear)
	}
	// The tables that follow it must still be readable — a table written in the
	// wrong place swallows whatever comes next.
	if !slices.Equal(out.Products["bluefin"], []string{"bluefin-core"}) {
		t.Errorf("products after the linear table = %v", out.Products)
	}
}

// TestSaveLoadGates proves the per-repo gate table survives the hand-written
// template Save regenerates, and that the tables after it are still readable —
// a table emitted in the wrong place swallows whatever follows it. The name is
// a check name, so it carries spaces and has to come back verbatim: "All gates
// green" is the aggregator job on the repository this landed for.
func TestSaveLoadGates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	in := &Config{
		Roots: []string{"~/repos"},
		Gates: map[string]string{
			"player-app": "All gates green",
			"shop-api":   "ci/required",
		},
		Products: map[string][]string{"bluefin": {"bluefin-core"}},
		Accounts: map[string]string{"work": "~/.claude-work"},
	}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.Gates["player-app"] != "All gates green" || out.Gates["shop-api"] != "ci/required" {
		t.Errorf("gates = %v", out.Gates)
	}
	if out.Accounts["work"] != "~/.claude-work" {
		t.Errorf("accounts after the gates table = %v", out.Accounts)
	}
	if !slices.Equal(out.Products["bluefin"], []string{"bluefin-core"}) {
		t.Errorf("products after the gates table = %v", out.Products)
	}
	// A config naming no gate must not grow one: an unlisted repo keeps the
	// reading of every check it has always had, and an empty table is how the
	// key documents itself without turning the feature on.
	if err := Save(&Config{Roots: []string{"~/repos"}}); err != nil {
		t.Fatal(err)
	}
	bare, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(bare.Gates) != 0 {
		t.Errorf("a config naming no gate loaded %v", bare.Gates)
	}
}

func TestWriteDefaultDoesNotOverwrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, created, err := WriteDefault(); err != nil || !created {
		t.Fatalf("first WriteDefault: created=%v err=%v", created, err)
	}
	if err := Save(&Config{Roots: []string{"/custom"}}); err != nil {
		t.Fatal(err)
	}
	if _, created, err := WriteDefault(); err != nil || created {
		t.Fatalf("second WriteDefault must be a no-op: created=%v err=%v", created, err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Roots, []string{"/custom"}) {
		t.Errorf("roots clobbered: %v", cfg.Roots)
	}
}

func TestProductFor(t *testing.T) {
	c := &Config{Products: map[string][]string{
		"shop": {"shop-api", "shop-web"},
	}}
	if got := c.ProductFor("shop-web"); got != "shop" {
		t.Errorf("ProductFor(shop-web) = %q", got)
	}
	if got := c.ProductFor("unrelated"); got != "" {
		t.Errorf("unmapped repo should be empty, got %q", got)
	}
}

// [root_branches] survives the cockpit's own save, which rewrites the whole
// file from what it holds: a table it did not write back would be lost the
// first time anyone changed a setting in-app.
func TestRootBranchesSurviveSave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := &Config{RootBranches: map[string]string{"playerpulse": "dev"}}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	back, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if back.RootBranchFor("playerpulse") != "dev" || back.RootBranchFor("other") != "" {
		t.Errorf("root_branches after a save = %v", back.RootBranches)
	}
}

// Every table the cockpit edits in-app comes back from its own save. Save
// rewrites the whole file from what it holds, so a field it forgot to write is
// lost the first time anyone changes any setting — and these are the ones a
// human sets once and stops looking at: a rebound key, a pinned checkout, a
// server name, a repo told not to run under nix.
func TestEverySettingTheCockpitWritesSurvivesItsOwnSave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	none := ""
	in := &Config{
		Theme:        "light",
		Worktrees:    "project",
		BranchPrefix: &none,
		Keys:         map[string]string{"products.new": "N", "search.next": "n"},
		Checkouts:    map[string]string{"shop-api": "/src/shop-api/dev"},
		Sockets:      map[string]string{"shop-api": "shop"},
		// "" is a setting, not an absence: it stops a flake repo being
		// launched under nix develop.
		SessionEnv:   map[string]string{"shop-api": "", "shop-web": "direnv exec ."},
		RootBranches: map[string]string{"shop-api": "dev"},
	}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.Theme != "light" || out.Worktrees != "project" {
		t.Errorf("theme = %q, worktrees = %q", out.Theme, out.Worktrees)
	}
	if out.BranchPrefix == nil || *out.BranchPrefix != "" {
		t.Errorf("an empty branch_prefix came back as %v — empty means no prefix, unset means feature/", out.BranchPrefix)
	}
	for name, pair := range map[string][2]map[string]string{
		"keys":          {out.Keys, in.Keys},
		"checkouts":     {out.Checkouts, in.Checkouts},
		"sockets":       {out.Sockets, in.Sockets},
		"session_env":   {out.SessionEnv, in.SessionEnv},
		"root_branches": {out.RootBranches, in.RootBranches},
	} {
		if !maps.Equal(pair[0], pair[1]) {
			t.Errorf("[%s] = %v, want %v", name, pair[0], pair[1])
		}
	}
	if v, ok := out.SessionEnv["shop-api"]; !ok || v != "" {
		t.Error("an empty session_env entry was dropped, so the repo would go back under nix develop")
	}
}

// The prefix the forms open on: feature/ when nothing is written, nothing at
// all when an empty string is, and what is written otherwise. Unset and empty
// are different settings — one is the default, the other is how a layout whose
// folders are named after their branches makes the two read the same.
func TestBranchPrefixIsFeatureUnlessWrittenAndEmptyMeansNone(t *testing.T) {
	empty, custom := "", " fix/ "
	for _, tc := range []struct {
		name string
		cfg  *Config
		want string
	}{
		{"no config", nil, DefaultBranchPrefix},
		{"unset", &Config{}, DefaultBranchPrefix},
		{"empty", &Config{BranchPrefix: &empty}, ""},
		{"written", &Config{BranchPrefix: &custom}, "fix/"},
	} {
		if got := tc.cfg.EffectiveBranchPrefix(); got != tc.want {
			t.Errorf("%s: EffectiveBranchPrefix() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A dispatch's checkout leaves the state directory only when the human asked
// for "project": the state directory is ours to delete folders in, a project
// folder is not, so anything else — a missing config included — keeps the
// default.
func TestWorktreesGoBesideTheProjectOnlyWhenAskedFor(t *testing.T) {
	for _, tc := range []struct {
		cfg  *Config
		want bool
	}{
		{nil, false},
		{&Config{}, false},
		{&Config{Worktrees: "state"}, false},
		{&Config{Worktrees: " Project "}, true},
	} {
		if got := tc.cfg.WorktreesBesideProject(); got != tc.want {
			t.Errorf("WorktreesBesideProject(%+v) = %v, want %v", tc.cfg, got, tc.want)
		}
	}
	var none *Config
	if got := none.RootBranchFor("shop-api"); got != "" {
		t.Errorf("no config named root %q; it must mean the remote's default", got)
	}
}

// A config file that does not parse is an error, never an empty config: an
// empty one has no roots and no products, and the cockpit would open on a
// portfolio of nothing with no word about why.
func TestAConfigThatDoesNotParseIsAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte("roots = [unclosed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, err := Load(); err == nil {
		t.Errorf("a malformed file loaded as %+v", cfg)
	}
}

// Where the config cannot be written, Save and WriteDefault say so instead of
// reporting a file that is not there.
func TestAConfigThatCannotBeWrittenSaysSo(t *testing.T) {
	// A HOME that is a file: the config directory cannot be made under it.
	home := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(home, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	if err := Save(&Config{}); err == nil {
		t.Error("Save reported success with nowhere to write")
	}
	if _, created, err := WriteDefault(); err == nil || created {
		t.Errorf("WriteDefault: created=%v err=%v, want the failure reported", created, err)
	}
}

// With no home directory to expand into, a ~ path is left as written rather
// than turned into a path under the filesystem root.
func TestExpandHomeWithNoHomeLeavesThePathAlone(t *testing.T) {
	t.Setenv("HOME", "")
	if got := ExpandHome("~/x"); got != "~/x" {
		t.Errorf("ExpandHome(~/x) with no home = %q", got)
	}
}
