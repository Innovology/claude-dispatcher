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
