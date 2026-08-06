package ui

import (
	"sort"

	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

// otherGroup labels dispatches and repos that no product claims.
const otherGroup = "other"

// groupByProduct reorders urgency-sorted dispatches into product sections.
// Members keep their relative (urgency) order, groups are ordered by their
// most urgent member, and the unnamed bucket sorts after named products on
// ties. The cockpit list renders a section header wherever the product
// changes, so grouping is purely an ordering concern.
func groupByProduct(ds []*state.Dispatch) []*state.Dispatch {
	type group struct {
		name    string
		best    int
		members []*state.Dispatch
	}
	byName := map[string]*group{}
	var groups []*group
	for _, d := range ds {
		g, ok := byName[d.Product]
		if !ok {
			g = &group{name: d.Product, best: d.Status.Priority()}
			byName[d.Product] = g
			groups = append(groups, g)
		}
		if p := d.Status.Priority(); p < g.best {
			g.best = p
		}
		g.members = append(g.members, d)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].best != groups[j].best {
			return groups[i].best < groups[j].best
		}
		if (groups[i].name == "") != (groups[j].name == "") {
			return groups[j].name == ""
		}
		return groups[i].name < groups[j].name
	})
	out := make([]*state.Dispatch, 0, len(ds))
	for _, g := range groups {
		out = append(out, g.members...)
	}
	return out
}

// groupRepos orders repos for the picker: named products alphabetically, the
// unnamed bucket last, repos by name within each.
func groupRepos(rs []repos.Repo) []repos.Repo {
	out := append([]repos.Repo(nil), rs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Product != out[j].Product {
			if (out[i].Product == "") != (out[j].Product == "") {
				return out[j].Product == ""
			}
			return out[i].Product < out[j].Product
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// anyProduct reports whether grouping headers are worth rendering at all — a
// portfolio with no products configured stays a flat list.
func anyProduct[T any](items []T, product func(T) string) bool {
	for _, it := range items {
		if product(it) != "" {
			return true
		}
	}
	return false
}

func groupLabel(product string) string {
	if product == "" {
		return otherGroup
	}
	return product
}
