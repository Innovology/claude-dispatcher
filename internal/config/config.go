// Package config loads the dispatcher configuration: which directory roots to
// scan for repositories, and how repos map onto products for the roll-up view.
package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	// Roots are directories scanned (a few levels deep) for git repositories.
	Roots []string `toml:"roots"`
	// Products maps a product name to the repo directory names that belong to
	// it, powering the portfolio roll-up lens.
	Products map[string][]string `toml:"products"`
	// DeployWorkflows overrides deploy-workflow auto-detection per repo
	// directory name. Unlisted repos use the first workflow whose name looks
	// deploy-ish (deploy/release/publish/ship/prod).
	DeployWorkflows map[string]string `toml:"deploy_workflows"`

	// Integrations (optional). Matching env vars, when set, override these so a
	// secret can be kept out of the file. Edited in-app from the cockpit.
	LinearAPIKey string `toml:"linear_api_key,omitempty"` // Linear backlog source
	AzureOrg     string `toml:"azure_org,omitempty"`      // Azure DevOps org URL
	AzureProject string `toml:"azure_project,omitempty"`  // Azure DevOps project
	// Linear maps a product name to the Linear token its backlog is read with.
	// A token sees one workspace and only the teams Linear granted it, so a
	// portfolio spanning several workspaces needs one each — as do two products
	// in one workspace, who get a team-scoped key apiece. A product with no
	// entry reads with LinearAPIKey, which the settings editor holds. Edited on
	// the products lens's assignment editor (`l`), because this map is keyed by
	// product NAME and that screen is where names are made — hand-writing it
	// means retyping a key that has to match one exactly, in another file, with
	// silence as the only feedback when it does not.
	Linear map[string]string `toml:"linear,omitempty"`
	// WeeklyTokenLimit is the subscription's weekly token budget. There is no
	// API to read it (Claude Code exposes usage only interactively), so it is a
	// user setting; 0 means unknown and the usage lens shows raw tokens instead
	// of a percentage.
	WeeklyTokenLimit int `toml:"weekly_token_limit,omitempty"`
}

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claude-dispatcher")
}

func Path() string { return filepath.Join(Dir(), "config.toml") }

func Load() (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(Path(), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ExpandedRoots returns Roots with ~ expanded and duplicates removed.
func (c *Config) ExpandedRoots() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range c.Roots {
		p := ExpandHome(r)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// ProductFor returns the product a repo directory name belongs to, or "".
func (c *Config) ProductFor(repoName string) string {
	for product, names := range c.Products {
		for _, n := range names {
			if n == repoName {
				return product
			}
		}
	}
	return ""
}

func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// DefaultRoot suggests a scan root: the parent of the working directory
// (running from inside one repo usually means its siblings are repos too),
// falling back to ~.
func DefaultRoot() string {
	if wd, err := os.Getwd(); err == nil {
		return filepath.Dir(wd)
	}
	return "~"
}

// Save writes the config file, regenerating the commented template around the
// current values so the file stays self-documenting after edits from the
// cockpit settings view.
func Save(c *Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Claude Dispatcher configuration.\n\n")
	b.WriteString("# Directories scanned (up to 3 levels deep) for git repositories.\n")
	b.WriteString("# Editable from the cockpit: press s.\n")
	b.WriteString("roots = " + tomlStrings(c.Roots) + "\n\n")
	// Top-level integration keys must precede the first [table] in TOML.
	b.WriteString("# Integrations (optional; edit in-app with `s`). Matching env vars override.\n")
	b.WriteString("# linear_api_key is the unscoped Linear key: the whole backlog when no\n")
	b.WriteString("# product names one below, and what a product with no entry is read with.\n")
	fmt.Fprintf(&b, "linear_api_key = %q\n", c.LinearAPIKey)
	b.WriteString("# Azure Boards backlog (needs the az CLI logged in): the org URL and project.\n")
	fmt.Fprintf(&b, "azure_org = %q\n", c.AzureOrg)
	fmt.Fprintf(&b, "azure_project = %q\n", c.AzureProject)
	b.WriteString("# Your subscription's weekly token budget (no API exposes it, so set it here).\n")
	b.WriteString("# 0 = unknown → the usage lens shows raw tokens instead of a percentage.\n")
	fmt.Fprintf(&b, "weekly_token_limit = %d\n\n", c.WeeklyTokenLimit)
	b.WriteString("# The Linear token each product's backlog is read with, keyed by product\n")
	b.WriteString("# name. A token sees one workspace and only the teams Linear granted it, so\n")
	b.WriteString("# two products in one workspace get a team-scoped key each — scope it where\n")
	b.WriteString("# you create it. Products with no entry read with linear_api_key above.\n")
	b.WriteString("# acme-shop = \"lin_api_...\"\n")
	b.WriteString("[linear]\n")
	for _, k := range slices.Sorted(maps.Keys(c.Linear)) {
		fmt.Fprintf(&b, "%s = %q\n", tomlKey(k), c.Linear[k])
	}
	b.WriteString("\n")
	b.WriteString("# Map product names to repo directory names for the portfolio roll-up.\n")
	b.WriteString("# acme-shop = [\"shop-api\", \"shop-web\"]\n")
	b.WriteString("[products]\n")
	for _, k := range slices.Sorted(maps.Keys(c.Products)) {
		b.WriteString(tomlKey(k) + " = " + tomlStrings(c.Products[k]) + "\n")
	}
	b.WriteString("\n")
	b.WriteString("# Per-repo deploy workflow override (\"done means live\" watches this workflow\n")
	b.WriteString("# succeed after a feature's PR merges). Unlisted repos auto-detect the first\n")
	b.WriteString("# workflow named like deploy/release/publish/ship/prod; repos with no deploy\n")
	b.WriteString("# workflow count merge itself as live.\n")
	b.WriteString("# shop-api = \"Deploy production\"\n")
	b.WriteString("[deploy_workflows]\n")
	for _, k := range slices.Sorted(maps.Keys(c.DeployWorkflows)) {
		fmt.Fprintf(&b, "%s = %q\n", tomlKey(k), c.DeployWorkflows[k])
	}
	return os.WriteFile(Path(), []byte(b.String()), 0o644)
}

var bareKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(k string) string {
	if bareKeyRe.MatchString(k) {
		return k
	}
	return fmt.Sprintf("%q", k)
}

func tomlStrings(vals []string) string {
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// WriteDefault creates the config file if absent, defaulting the scan root to
// the parent directory of the current working directory (or ~).
func WriteDefault() (string, bool, error) {
	path := Path()
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	}
	if err := Save(&Config{Roots: []string{DefaultRoot()}}); err != nil {
		return path, false, err
	}
	return path, true, nil
}
