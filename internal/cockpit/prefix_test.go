package cockpit

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// capturePrefix replaces the seam both forms hand off through and returns a
// pointer to the prefix the next dispatch is asked to cut with.
func capturePrefix(t *testing.T) **string {
	t.Helper()
	var got *string
	prev, dxPrev := launchDispatch, dxLaunch
	launchDispatch = func(_ *config.Config, _, _, _ string, _ dispatchpkg.Mode, _ dispatchpkg.Model, _ dispatchpkg.Root, prefix *string, _ bool, _ string) tea.Cmd {
		got = prefix
		return func() tea.Msg { return nil }
	}
	dxLaunch = launchDispatch
	t.Cleanup(func() { launchDispatch, dxLaunch = prev, dxPrev })
	return &got
}

func strp(s string) *string { return &s }

// The dx form opens on settings' prefix and previews the branch with it: it
// printed feature/ whatever the config said, so a machine set to no prefix was
// shown a branch the launch never cut.
func TestDXPrefixOpensOnSettingsAndOverridesOneDispatch(t *testing.T) {
	got := capturePrefix(t)
	m := dxFormModel(t)
	m.cfg.BranchPrefix = strp("")
	m = m.dxOpen("")
	if m.dxTouched() {
		t.Error("a PREFIX nobody typed into counts as typing, which strands the human on the form")
	}
	m.dxTitle, m.dxWhat = "payment retries", "retry declined cards"

	if b := m.dxBranch(); b != "payment-retries" {
		t.Errorf("with no prefix configured the branch previews as %q", b)
	}
	if !strings.Contains(m.View(), "PREFIX") {
		t.Error("the form does not show PREFIX")
	}

	// Typed on the form, for this dispatch: the default stays as it was.
	m.dxField = dxPrefixF
	for _, k := range strings.Split("fix/", "") {
		m = press(m, k)
	}
	if b := m.dxBranch(); b != "fix/payment-retries" {
		t.Errorf("an override previews as %q", b)
	}
	if !strings.Contains(m.dxPrefixNote(), "this dispatch only") {
		t.Errorf("the note does not say the change is for this dispatch: %q", m.dxPrefixNote())
	}
	if _, _ = m.dxSubmit(); *got == nil || **got != "fix/" {
		t.Fatalf("the launch was asked for prefix %v, want fix/", *got)
	}
	if m.cfg.EffectiveBranchPrefix() != "" {
		t.Error("an override on the form changed the default")
	}

	// And the next form opens on the default again.
	if m = m.dxOpen(""); m.dxPrefixText() != "" {
		t.Errorf("a new form opened on %q, want settings' default", m.dxPrefixText())
	}
}

// A prefix git would refuse is refused on the form, which keeps what was typed,
// rather than by git after the form has gone.
func TestDXRefusesAPrefixGitWouldRefuse(t *testing.T) {
	got := capturePrefix(t)
	m := dxFormModel(t)
	m.dxTitle, m.dxWhat = "payment retries", "retry declined cards"
	m = m.dxSetPrefix("my fix/")
	mm, _ := m.dxSubmit()
	if *got != nil || mm.dxField != dxPrefixF || !strings.Contains(mm.notice, "space") {
		t.Fatalf("launched=%v field=%d notice=%q", *got != nil, mm.dxField, mm.notice)
	}
	if mm.dxTitle != "payment retries" {
		t.Error("a refusal threw away the form")
	}
}

// The `+` overlay keeps the prefix on the feature step — tab reaches it — so
// changing it costs nothing on the dispatches that do not.
func TestDispatchFormPrefixIsOnTheFeatureStep(t *testing.T) {
	root, _ := seedGitRepo(t, "shop-web")
	got := capturePrefix(t)
	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = &config.Config{Roots: []string{root}, BranchPrefix: strp("")}
	m.lens = "products"

	m = press(m, "+")
	m = press(m, "enter") // the only repo
	m = typeStr(m, "csv export")
	if out := m.View(); !strings.Contains(out, "the branch will be csv-export") {
		t.Errorf("the preview ignores the configured prefix:\n%s", out)
	}
	m = press(m, "tab")
	m = typeStr(m, "vk/")
	if out := m.View(); !strings.Contains(out, "the branch will be vk/csv-export") ||
		!strings.Contains(out, "this dispatch only") {
		t.Errorf("the preview does not follow the prefix typed:\n%s", out)
	}
	for _, k := range []string{"enter", "enter", "enter", "enter", "enter", "enter"} {
		m = press(m, k) // root, mode, model, account, fan out → prompt
	}
	m = typeStr(m, "export the orders as csv")
	m = press(m, "enter")
	if *got == nil || **got != "vk/" {
		t.Fatalf("the launch was asked for prefix %v, want vk/", *got)
	}
}

// Settings holds the default: blank is "none" and is written as such, and a
// prefix git would refuse is not saved at all.
func TestSettingsBranchPrefix(t *testing.T) {
	var f settingsField
	for _, sf := range settingsFields {
		if sf.key == "branch_prefix" {
			f = sf
		}
	}
	if f.key == "" {
		t.Fatal("settings has no branch prefix")
	}
	cfg := &config.Config{}
	if v := settingsValue(cfg, f); v != config.DefaultBranchPrefix {
		t.Errorf("unset shows %q, want the default it means", v)
	}
	settingsCommit(cfg, f, "  ")
	if cfg.BranchPrefix == nil || *cfg.BranchPrefix != "" {
		t.Errorf("blank saved as %v, want an explicit none", cfg.BranchPrefix)
	}
	if settingsProblem(f, "a..b/") == "" || settingsProblem(f, "fix/") != "" || settingsProblem(f, "") != "" {
		t.Error("settings validates the prefix differently from git")
	}
}

// A row whose launch never ran offers to start it again, not a resume that can
// only refuse — "never recorded a session id — there is nothing to resume" was
// the whole of what ⏎ did there.
func TestANeverStartedRowOffersToStartAgain(t *testing.T) {
	never := &state.Dispatch{Feature: "explore-code", Status: state.StatusExited, StatusReason: "did not start: …"}
	if acts := cqActs(never, "done"); len(acts) == 0 || acts[0].k != "⏎" || acts[0].d != "start again" {
		t.Errorf("acts = %+v", acts)
	}
	ran := &state.Dispatch{Feature: "robustness", Status: state.StatusExited, SessionID: "sess-1"}
	if acts := cqActs(ran, "done"); acts[0].d != "resume" {
		t.Errorf("a dispatcher with a conversation should resume it: %+v", acts[0])
	}
}

// A blank ROOT on a repo with a root branch of its own says that branch, on
// both forms — "as origin sees it" would be describing a launch that will not
// happen.
func TestFormsNameTheRepoRootBranch(t *testing.T) {
	m := dxFormModel(t)
	m.cfg.RootBranches = map[string]string{"shop-api": "dev"}
	if h := m.dxRootHint(); !strings.Contains(h, "dev") || strings.Contains(h, "as origin sees it") {
		t.Errorf("dx ROOT hint = %q", h)
	}
	m.dxTitle = "payment retries"
	if s := m.dxSummary(); !strings.Contains(s, "from dev") {
		t.Errorf("dx summary = %q", s)
	}
	df := &dispatchForm{}
	df.repo.RootBranch = "dev"
	if o := df.rootOptions()[0]; !strings.Contains(o.hint, "dev") {
		t.Errorf("+ overlay default row = %+v", o)
	}
	if df.rootLabel() != "default · dev" {
		t.Errorf("+ overlay breadcrumb = %q", df.rootLabel())
	}
}
