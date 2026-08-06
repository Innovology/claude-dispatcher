// Package config loads the dispatcher configuration: which directory roots to
// scan for repositories, and how repos map onto products for the roll-up view.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	// Roots are directories scanned (a few levels deep) for git repositories.
	Roots []string `toml:"roots"`
	// Products maps a product name to the repo directory names that belong to
	// it, powering the portfolio roll-up lens.
	Products map[string][]string `toml:"products"`
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

const defaultConfig = `# Claude Dispatcher configuration.

# Directories scanned (up to 3 levels deep) for git repositories.
roots = [%q]

# Map product names to repo directory names for the portfolio roll-up.
# [products]
# acme-shop = ["shop-api", "shop-web"]
# side-thing = ["side-thing"]
[products]
`

// WriteDefault creates the config file if absent, defaulting the scan root to
// the parent directory of the current working directory (or ~).
func WriteDefault() (string, bool, error) {
	path := Path()
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	}
	root := "~"
	if wd, err := os.Getwd(); err == nil {
		root = filepath.Dir(wd)
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return path, false, err
	}
	content := fmt.Sprintf(defaultConfig, root)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return path, false, err
	}
	return path, true, nil
}
