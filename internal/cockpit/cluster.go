package cockpit

// cluster.go is the products lens's assignment editor: the two-pane screen that
// finally lets you say which repos make up a product, without leaving the
// cockpit for config.toml.
//
// A product is the only grouping the whole cockpit uses — triage orders by it,
// velocity rolls up by it — and until now it existed solely as a `[products]`
// table the user had to hand-edit. Assignments made here are written straight
// back to that table, so the file stays the source of truth and anything
// already in it survives a round trip.

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
)

// clUnassigned is the product key collectProducts folds unmapped repos into.
// It is a display bucket, never a real product, so it is never written to the
// config and never offered as an assignment target.
const clUnassigned = "unassigned"

// clRepoRow is one row of the editor's left pane.
type clRepoRow struct {
	name, forge, product, last string
	out                        int
}

// clRepos lists every discovered repo, mapped or not, in a stable order: the
// unassigned first — they are why the screen is open — then the rest by name.
func (m model) clRepos() []clRepoRow {
	var out []clRepoRow
	for prod, refs := range reposByProduct {
		for _, r := range refs {
			p := prod
			if p == clUnassigned {
				p = ""
			}
			// The working copy wins: it holds edits not yet reflected in a
			// reloaded snapshot.
			if v, ok := m.clMap[r.name]; ok {
				p = v
			}
			last := r.last
			if last == "" {
				last = "—"
			}
			out = append(out, clRepoRow{name: r.name, forge: r.forge, product: p, out: r.out, last: last})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].product == "", out[j].product == ""
		if ai != aj {
			return ai
		}
		return out[i].name < out[j].name
	})
	return out
}

// clProducts lists the assignment targets: every product in the config plus any
// created in this session, sorted. "unassigned" is not one of them — `u` is how
// you take a repo out of a product.
func (m model) clProducts() []string {
	seen := map[string]bool{}
	for _, r := range m.clRepos() {
		if r.product != "" && r.product != clUnassigned {
			seen[r.product] = true
		}
	}
	if m.cfg != nil {
		for p := range m.cfg.Products {
			if p != clUnassigned {
				seen[p] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// clTargets is what an assignment applies to: everything marked, or the row
// under the cursor when nothing is marked.
func (m model) clTargets() []string {
	var marked []string
	for name, on := range m.clMarked {
		if on {
			marked = append(marked, name)
		}
	}
	if len(marked) > 0 {
		sort.Strings(marked)
		return marked
	}
	rows := m.clRepos()
	if len(rows) == 0 {
		return nil
	}
	return []string{rows[clampCursor(m.clRepo, len(rows))].name}
}

// clAssign moves repos into product (or out of any product when it is empty),
// then persists. The returned command carries the save's outcome — a failed
// write must not look like a successful assignment.
func (m model) clAssign(repos []string, product string) (model, tea.Cmd) {
	if len(repos) == 0 {
		return m, nil
	}
	if m.clMap == nil {
		m.clMap = map[string]string{}
	}
	for _, r := range repos {
		m.clMap[r] = product
	}
	m.clMarked = map[string]bool{}

	word := "repo"
	if len(repos) != 1 {
		word = "repos"
	}
	if product == "" {
		m.notice = itoa(len(repos)) + " " + word + " moved out of their product"
	} else {
		m.notice = itoa(len(repos)) + " " + word + " → " + product
	}
	return m, m.clPersist()
}

// clPersist rewrites [products] from the working copy. Products are rebuilt
// wholesale rather than patched: a repo belongs to exactly one product, so the
// map is the truth and a merge could leave a repo in two.
func (m model) clPersist() tea.Cmd {
	if m.cfg == nil {
		return func() tea.Msg { return actionMsg{notice: "no config — nothing saved"} }
	}
	next := map[string][]string{}
	for _, r := range m.clRepos() {
		if r.product == "" || r.product == clUnassigned {
			continue
		}
		next[r.product] = append(next[r.product], r.name)
	}
	for p := range next {
		sort.Strings(next[p])
	}
	cfg := *m.cfg
	cfg.Products = next
	return func() tea.Msg {
		if err := config.Save(&cfg); err != nil {
			return actionMsg{notice: "could not save products: " + err.Error()}
		}
		return actionMsg{notice: ""} // reloads the snapshot so every lens regroups
	}
}

// ---- keys -------------------------------------------------------------------

// updateCluster handles the editor's keys. handled is false only for the keys
// allowed to leave it (1–8 and ':'), which handleKey routes as usual.
func (m model) updateCluster(k string) (model, tea.Cmd, bool) {
	rows := m.clRepos()
	prods := m.clProducts()

	// Naming a new product: the buffer owns the keyboard.
	if m.clNaming {
		switch k {
		case "esc":
			m.clNaming, m.clNewName = false, ""
			return m, nil, true
		case "enter":
			name := strings.TrimSpace(m.clNewName)
			m.clNaming, m.clNewName = false, ""
			if name == "" || name == clUnassigned {
				return m, nil, true
			}
			targets := m.clTargets()
			mm, cmd := m.clAssign(targets, name)
			word := "repo"
			if len(targets) != 1 {
				word = "repos"
			}
			mm.notice = "created \"" + name + "\" · " + itoa(len(targets)) + " " + word + " moved in"
			return mm, cmd, true
		case "backspace":
			r := []rune(m.clNewName)
			if len(r) > 0 {
				m.clNewName = string(r[:len(r)-1])
			}
			return m, nil, true
		}
		// Text comes from the key message, never the key's name — a burst or a
		// paste arrives as one multi-rune message.
		if s, ok := typedTextFor(m.key, k); ok {
			m.clNewName += s
		}
		return m, nil, true
	}

	switch k {
	case "esc", "a":
		m.clOpen, m.clMarked = false, map[string]bool{}
		return m, nil, true
	case "tab":
		if m.clPane == "products" {
			m.clPane = "repos"
		} else {
			m.clPane = "products"
		}
		return m, nil, true
	case "n":
		m.clNaming, m.clNewName = true, ""
		return m, nil, true
	case "j", "down":
		if m.clPane == "products" {
			m.clProd = mini(m.clProd+1, maxi(len(prods)-1, 0))
		} else {
			m.clRepo = mini(m.clRepo+1, maxi(len(rows)-1, 0))
		}
		return m, nil, true
	case "k", "up":
		if m.clPane == "products" {
			m.clProd = maxi(m.clProd-1, 0)
		} else {
			m.clRepo = maxi(m.clRepo-1, 0)
		}
		return m, nil, true
	case " ", "space":
		if len(rows) == 0 {
			return m, nil, true
		}
		name := rows[clampCursor(m.clRepo, len(rows))].name
		if m.clMarked == nil {
			m.clMarked = map[string]bool{}
		}
		if m.clMarked[name] {
			delete(m.clMarked, name)
		} else {
			m.clMarked[name] = true
		}
		m.clRepo = mini(m.clRepo+1, maxi(len(rows)-1, 0))
		return m, nil, true
	case "u":
		mm, cmd := m.clAssign(m.clTargets(), "")
		return mm, cmd, true
	case "U":
		// Start over: every repo out of every product. Destructive enough to
		// deserve its own capital key rather than sharing `u`.
		var all []string
		for _, r := range rows {
			all = append(all, r.name)
		}
		mm, cmd := m.clAssign(all, "")
		mm.notice = "every repo unassigned · no products left"
		return mm, cmd, true
	case "enter":
		if len(prods) == 0 {
			m.notice = "no product to assign to — n names one"
			return m, nil, true
		}
		target := prods[clampCursor(m.clProd, len(prods))]
		mm, cmd := m.clAssign(m.clTargets(), target)
		return mm, cmd, true
	}

	if isLensDigit(k) || k == ":" {
		return m, nil, false
	}
	return m, nil, true
}
