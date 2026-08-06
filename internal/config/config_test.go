package config

import (
	"os"
	"path/filepath"
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
