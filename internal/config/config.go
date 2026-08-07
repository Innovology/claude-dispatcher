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
