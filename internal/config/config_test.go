package config

import (
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
