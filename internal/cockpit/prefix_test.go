package cockpit

import (
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

func strp(s string) *string { return &s }

// The dx form opens on settings' prefix and previews the branch with it: it
// printed feature/ whatever the config said, so a machine set to no prefix was
// shown a branch the launch never cut.
func TestDXPrefixOpensOnSettingsAndOverridesOneDispatch(t *testing.T) {
	got := captureLaunches(t)
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
	if _, _ = m.dxSubmit(); got.prefix == nil || *got.prefix != "fix/" {
		t.Fatalf("the launch was asked for prefix %v, want fix/", got.prefix)
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
	got := captureLaunches(t)
	m := dxFormModel(t)
	m.dxTitle, m.dxWhat = "payment retries", "retry declined cards"
	m = m.dxSetPrefix("my fix/")
	mm, _ := m.dxSubmit()
	if got.n > 0 || mm.dxField != dxPrefixF || !strings.Contains(mm.notice, "space") {
		t.Fatalf("launched=%v field=%d notice=%q", got.n > 0, mm.dxField, mm.notice)
	}
	if mm.dxTitle != "payment retries" {
		t.Error("a refusal threw away the form")
	}
}

// The `+` overlay keeps the prefix on the feature step — tab reaches it — so
// changing it costs nothing on the dispatches that do not.
func TestDispatchFormPrefixIsOnTheFeatureStep(t *testing.T) {
	root, _ := seedGitRepo(t, "shop-web")
	got := captureLaunches(t)
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
	if got.prefix == nil || *got.prefix != "vk/" {
		t.Fatalf("the launch was asked for prefix %v, want vk/", got.prefix)
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
