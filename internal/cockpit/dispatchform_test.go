package cockpit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
)

// seedRepoRoot creates a scan root with the given repo dirs (each a fake git
// repo) so repos.Discover finds them without any real git plumbing.
func seedRepoRoot(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n, ".git"), 0o755); err != nil {
			t.Fatalf("seed repo %q: %v", n, err)
		}
	}
	return root
}

func typeStr(m model, s string) model {
	for _, ch := range strings.Split(s, "") {
		m = press(m, ch)
	}
	return m
}

// TestDispatchFormFlow opens the overlay with `+`, filters and picks a repo,
// names a feature, writes a prompt, and submits — asserting each step
// transition and the final launch hand-off, without touching real git.
func TestDispatchFormFlow(t *testing.T) {
	root := seedRepoRoot(t, "shop-api", "shop-web", "blog")
	cfg := &config.Config{
		Roots:    []string{root},
		Products: map[string][]string{"shop": {"shop-api", "shop-web"}},
	}

	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = cfg

	// Open the form.
	m = press(m, "+")
	if m.dispatchForm == nil {
		t.Fatal("'+' did not open the dispatch form")
	}
	if df := m.dispatchForm; len(df.repos) != 3 {
		t.Fatalf("discovered %d repos, want 3", len(df.repos))
	}
	out := m.View()
	if !strings.Contains(out, "new dispatch") {
		t.Fatal("overlay does not render the 'new dispatch' title")
	}

	// Step 1: filter down to shop-web and select it.
	m = typeStr(m, "web")
	if vis := m.dispatchForm.filtered(); len(vis) != 1 || vis[0].Name != "shop-web" {
		t.Fatalf("filter 'web' → %v, want [shop-web]", vis)
	}
	m = press(m, "enter")
	if m.dispatchForm.step != dispatchFeature {
		t.Fatalf("after repo select, step = %d, want feature", m.dispatchForm.step)
	}
	if m.dispatchForm.repo.Name != "shop-web" {
		t.Fatalf("selected repo = %q, want shop-web", m.dispatchForm.repo.Name)
	}

	// Step 2: empty feature is rejected, then a real name advances.
	m = press(m, "enter")
	if m.dispatchForm.step != dispatchFeature || m.dispatchForm.errMsg == "" {
		t.Fatal("empty feature name should be rejected and stay on the feature step")
	}
	m = typeStr(m, "csv export")
	m = press(m, "enter")
	if m.dispatchForm.step != dispatchPrompt {
		t.Fatalf("after feature name, step = %d, want prompt", m.dispatchForm.step)
	}
	if strings.TrimSpace(strings.Join(strings.Fields(m.View()), " ")) == "" {
		t.Fatal("prompt step render empty")
	}

	// Step 3: empty prompt is rejected, then submitting launches and closes.
	m = press(m, "enter")
	if m.dispatchForm == nil || m.dispatchForm.errMsg == "" {
		t.Fatal("empty prompt should be rejected and keep the form open")
	}
	m = typeStr(m, "do the thing")
	var cmd interface{}
	next, c := m.handleKey("enter")
	m = next.(model)
	cmd = c
	if m.dispatchForm != nil {
		t.Fatal("submitting the prompt should close the form")
	}
	if cmd == nil {
		t.Fatal("submitting should return a launch command")
	}
	if !strings.Contains(m.notice, "dispatching") {
		t.Fatalf("notice = %q, want a 'dispatching' hint", m.notice)
	}
}

// TestDispatchFormEscBacksOut walks the esc chain: prompt → feature → repo →
// closed, mirroring the classic form's back navigation.
func TestDispatchFormEscBacksOut(t *testing.T) {
	root := seedRepoRoot(t, "api")
	cfg := &config.Config{Roots: []string{root}}

	m := newModel()
	m.width, m.height = 130, 30
	m.cfg = cfg

	m = press(m, "+")
	m = press(m, "enter") // pick the only repo
	m = typeStr(m, "thing")
	m = press(m, "enter") // → prompt
	if m.dispatchForm.step != dispatchPrompt {
		t.Fatalf("expected prompt step, got %d", m.dispatchForm.step)
	}
	m = press(m, "esc")
	if m.dispatchForm.step != dispatchFeature {
		t.Fatal("esc from prompt should go back to feature")
	}
	if got := strings.TrimSpace(m.dispatchForm.feature.Value()); got != "thing" {
		t.Fatalf("feature value lost on back-nav: %q", got)
	}
	m = press(m, "esc")
	if m.dispatchForm.step != dispatchRepo {
		t.Fatal("esc from feature should go back to repo")
	}
	m = press(m, "esc")
	if m.dispatchForm != nil {
		t.Fatal("esc from repo should close the form")
	}
}

// TestDispatchFormViaPalette confirms the palette 'dispatch' command opens the
// form rather than switching to the queue lens.
func TestDispatchFormViaPalette(t *testing.T) {
	root := seedRepoRoot(t, "api")
	cfg := &config.Config{Roots: []string{root}}

	m := newModel()
	m.width, m.height = 160, 40
	m.cfg = cfg

	m = press(m, ":")
	m = typeStr(m, "dispatch")
	m = press(m, "enter")
	if m.dispatchForm == nil {
		t.Fatal("palette 'dispatch' command did not open the form")
	}
	if m.paletteOpen {
		t.Fatal("palette should close when the dispatch command runs")
	}
}
