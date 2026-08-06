// Package repos discovers the git repositories the cockpit operates across.
// Repos are the organising primitive — not worktrees.
package repos

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"claude-dispatcher/internal/config"
)

type Repo struct {
	Name    string
	Path    string
	Product string
}

const maxDepth = 3

var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
}

// Discover scans each configured root up to maxDepth for directories
// containing a .git entry. It does not descend into discovered repos.
func Discover(cfg *config.Config) []Repo {
	seen := map[string]bool{}
	var out []Repo
	for _, root := range cfg.ExpandedRoots() {
		root := filepath.Clean(root)
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || skipDirs[name]) {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(path, ".git")); statErr == nil {
				if !seen[path] {
					seen[path] = true
					out = append(out, Repo{
						Name:    filepath.Base(path),
						Path:    path,
						Product: cfg.ProductFor(filepath.Base(path)),
					})
				}
				return filepath.SkipDir
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil && rel != "." && strings.Count(rel, string(filepath.Separator)) >= maxDepth-1 {
				return filepath.SkipDir
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
