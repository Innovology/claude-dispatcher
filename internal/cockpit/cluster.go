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
	"maps"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/keymap"
)

// clUnassigned is the product key collectProducts folds unmapped repos into.
// It is a display bucket, never a real product, so it is never written to the
// config and never offered as an assignment target.
const clUnassigned = "unassigned"

// clRepoRow is one row of the editor's left pane. One row is one repository,
// which may be many checkouts on disk — path and worktrees are what `p` unfolds
// to say which.
type clRepoRow struct {
	name, forge, product, last string
	out                        int
	path                       string
	pinned                     bool
	worktrees                  []repoWorktree
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
			out = append(out, clRepoRow{
				name: r.name, forge: r.forge, product: p, out: r.out, last: last,
				path: r.path, pinned: r.pinned, worktrees: r.worktrees,
			})
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
//
// A save that works is also published into the cockpit's own config, because
// that object — loaded once at startup and never re-read — is what every
// collector groups by. This used to write a *copy* and leave m.cfg alone, so the
// file on disk was right and the running cockpit went on grouping by the mapping
// the human had just replaced: the assignment appeared in this editor's working
// copy and nowhere else, and triage went on calling the repo unassigned until
// the cockpit was restarted.
//
// The write is synchronous, like the settings editor's, so the config in memory
// and the config on disk are never two different things: a failed write leaves
// both as they were and says so.
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
	if err := config.Save(&cfg); err != nil {
		return func() tea.Msg { return actionMsg{notice: "could not save products: " + err.Error()} }
	}
	m.cfg.Products = next
	return func() tea.Msg { return actionMsg{notice: ""} } // reloads the snapshot so every lens regroups
}

// clPinCheckout writes which working tree a repo is read and dispatched from
// into `[checkouts]`, and persists.
//
// The automatic choice knows three spellings of a trunk — the main worktree,
// `main`, `master` — and a repository whose trunk is none of those gets a row
// pointed at a branch nobody ships from: its staleness log, its decisions scan
// and the tree a dispatch starts in all come from this directory. The human
// picks instead, from the checkouts git actually lists, which is why this is a
// selection in the fold rather than a path typed anywhere.
//
// Choosing the one already pinned CLEARS the entry rather than rewriting it.
// Otherwise a pin would be a door with no handle on the inside: nothing else on
// this screen removes one, and "back to choosing automatically" is a state the
// human has to be able to get back to.
//
// Same write discipline as clPersist and clSetLinearKey — a copy saved first,
// and only a save that worked published into the cockpit's own config, so the
// file on disk and the config every collector reads are never two different
// things.
func (m model) clPinCheckout(r clRepoRow, path string) (model, tea.Cmd) {
	if m.cfg == nil {
		return m, func() tea.Msg { return actionMsg{notice: "no config — nothing saved"} }
	}
	next := map[string]string{}
	maps.Copy(next, m.cfg.Checkouts)
	unpin := r.pinned && r.path == path
	if unpin {
		delete(next, r.name)
	} else {
		next[r.name] = path
	}
	cfg := *m.cfg
	cfg.Checkouts = next
	if err := config.Save(&cfg); err != nil {
		return m, func() tea.Msg { return actionMsg{notice: "could not save the checkout: " + err.Error()} }
	}
	m.cfg.Checkouts = next
	notice := r.name + " works in " + filepath.Base(path)
	if unpin {
		notice = r.name + " chooses its checkout again"
	}
	// The notice rides the message because the reload it queues sets one of its
	// own: Repo.Path is what the next snapshot reads every repo through.
	return m, func() tea.Msg { return actionMsg{notice: notice} }
}

// clLinearKey is the Linear token a product's backlog is read with, or "" when
// it names none and reads with the unscoped key.
func (m model) clLinearKey(product string) string {
	if m.cfg == nil || product == "" {
		return ""
	}
	return m.cfg.Linear[product]
}

// clSetLinearKey writes product's Linear token into `[linear]` and persists.
//
// The token belongs on this screen because `[linear]` is keyed by product NAME,
// and product names are minted here — `n` is the only place in the product that
// creates one. Asked to hand-write the table, the human has to retype a string
// that must match a name in another file exactly, and a mismatch fails silently
// in the worst possible way: the token authenticates, the read succeeds, and
// its tickets are filed under a product that groups nowhere. Typed against the
// row that holds the name, it cannot be mistyped at all.
//
// An empty token removes the entry rather than storing a blank, because those
// are two different states: no entry is "read this product with the unscoped
// key", and `product = ""` in the file would say the same thing in a way that
// makes the product look configured on this screen when it is not.
//
// Same write discipline as clPersist, for the same reason: a copy is saved
// first and only a save that worked is published into the cockpit's own config,
// so the file on disk and the config every collector reads are never two
// different things.
func (m model) clSetLinearKey(product, key string) (model, tea.Cmd) {
	if product == "" {
		return m, nil
	}
	if m.cfg == nil {
		return m, func() tea.Msg { return actionMsg{notice: "no config — nothing saved"} }
	}
	next := map[string]string{}
	for p, k := range m.cfg.Linear {
		next[p] = k
	}
	key = strings.TrimSpace(key)
	if key == "" {
		delete(next, product)
	} else {
		next[product] = key
	}
	cfg := *m.cfg
	cfg.Linear = next
	if err := config.Save(&cfg); err != nil {
		return m, func() tea.Msg { return actionMsg{notice: "could not save the token: " + err.Error()} }
	}
	m.cfg.Linear = next
	notice := product + " reads Linear with its own token"
	if key == "" {
		notice = product + " reads with the unscoped Linear key again"
	}
	// The notice rides the message rather than the model because the reload it
	// queues sets one of its own — collectBacklog reads per token, so the next
	// snapshot is already the one this changed.
	return m, func() tea.Msg { return actionMsg{notice: notice} }
}

// clCanonicalIdx is where in a row's checkouts the one it works in sits, so the
// fold opens on it rather than at the top: the list can be sixty-nine long and
// the answer to "which one is it now" should not have to be scrolled for.
func clCanonicalIdx(r clRepoRow) int {
	for i, w := range r.worktrees {
		if w.path == r.path {
			return i
		}
	}
	return 0
}

// clFoldTarget is the row the fold has the keyboard for, and whether it is still
// on screen. A reload can retire a repo while its fold is open — the roots
// changed, or it was moved — and the fold must then let go rather than act on a
// row that is not there.
func clFoldTarget(rows []clRepoRow, name string) (clRepoRow, bool) {
	for _, r := range rows {
		if r.name == name {
			return r, true
		}
	}
	return clRepoRow{}, false
}

// updateClFold handles the keys an unfolded row owns while it is focused. done
// is false for everything it does not claim, which goes on to the editor's own
// keys — `a`, `n`, `tab` and the rest still work with a fold open.
func (m model) updateClFold(k string, rows []clRepoRow) (model, tea.Cmd, bool) {
	row, ok := clFoldTarget(rows, m.clFoldRow)
	if !ok || len(row.worktrees) == 0 {
		m.clFoldRow = ""
		return m, nil, false
	}
	switch k {
	case "j", "down":
		m.clFoldIdx = mini(m.clFoldIdx+1, len(row.worktrees)-1)
		return m, nil, true
	case "k", "up":
		m.clFoldIdx = maxi(m.clFoldIdx-1, 0)
		return m, nil, true
	case "esc":
		// Out of the fold, not out of the editor, and the fold stays open: esc
		// here means "stop steering this list", and closing what the human is
		// reading as well would make the key ambiguous with `p`.
		m.clFoldRow = ""
		return m, nil, true
	case "tab":
		// tab moves to the products pane, which the fold's cursor has nothing to
		// do with — it lets go rather than leaving two cursors lit at once.
		m.clFoldRow = ""
		return m, nil, false
	case "enter":
		w := row.worktrees[clampCursor(m.clFoldIdx, len(row.worktrees))]
		mm, cmd := m.clPinCheckout(row, w.path)
		return mm, cmd, true
	}
	return m, nil, false
}

// ---- keys -------------------------------------------------------------------

// updateCluster handles the editor's keys. handled is false only for the keys
// allowed to leave it (1–6 and ':'), which handleKey routes as usual.
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

	// Typing a product's Linear token, likewise. A token is pasted far more
	// often than it is typed, which is the whole reason text comes from the key
	// message: a paste is one multi-rune message, and reading the key's *name*
	// would turn "lin_api_xxx" into nothing at all.
	if m.clKeying {
		switch k {
		case "esc":
			m.clKeying, m.clKeyFor, m.clKeyText = false, "", ""
			return m, nil, true
		case "enter":
			product, key := m.clKeyFor, m.clKeyText
			m.clKeying, m.clKeyFor, m.clKeyText = false, "", ""
			mm, cmd := m.clSetLinearKey(product, key)
			return mm, cmd, true
		case "backspace":
			r := []rune(m.clKeyText)
			if len(r) > 0 {
				m.clKeyText = string(r[:len(r)-1])
			}
			return m, nil, true
		}
		if s, ok := typedTextFor(m.key, k); ok {
			m.clKeyText += s
		}
		return m, nil, true
	}

	// An unfolded row can take the keyboard, and while it has it j/k/enter are
	// about its checkouts rather than about the repo list. Resolved before the
	// editor's own keys for the same reason the naming prompt is resolved before
	// them: the alternative is one key meaning two things at once.
	if m.clFoldRow != "" {
		if mm, cmd, done := m.updateClFold(m.keys.Resolve(keymap.Fold, k), rows); done {
			return mm, cmd, true
		}
	}

	k = m.keys.Resolve(keymap.Editor, k)

	switch k {
	case "/":
		return m.searchStart(), nil, true
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
	case "l":
		// The token is a fact about a product, so it is entered against a
		// product row and nowhere else — a repo does not have one. Starting from
		// empty rather than from the stored token: it is masked on screen, and
		// pre-filling a buffer with a secret the human cannot read is how you
		// get a half-deleted key saved over a working one.
		if len(prods) == 0 {
			m.notice = "no product to hold a token — " + m.keys.KeyFor("products.new") + " names one"
			return m, nil, true
		}
		m.clPane = "products"
		m.clKeying, m.clKeyFor, m.clKeyText = true, prods[clampCursor(m.clProd, len(prods))], ""
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
	case "p":
		// Where a repo actually is, and which of its checkouts it works in. A row
		// is a repository now, not a folder — one row can stand for sixty-nine
		// checkouts and be named for none of them — so both questions have to be
		// answerable without leaving the screen you are assigning from.
		//
		// Unfolding also hands the fold the keyboard, because the second question
		// is answered by moving through the list and picking one. The fold is
		// remembered by name, so leaving it with esc keeps it open and the cursor
		// can move on with it still showing.
		if len(rows) == 0 {
			return m, nil, true
		}
		row := rows[clampCursor(m.clRepo, len(rows))]
		if m.clExpanded == nil {
			m.clExpanded = map[string]bool{}
		}
		if m.clFoldRow == row.name {
			m.clFoldRow = ""
			delete(m.clExpanded, row.name)
			return m, nil, true
		}
		m.clExpanded[row.name] = true
		m.clFoldRow, m.clFoldIdx = row.name, clCanonicalIdx(row)
		return m, nil, true
	case "u":
		mm, cmd := m.clAssign(m.clTargets(), "")
		return mm, cmd, true
	case "ctrl+u":
		// Start over: every repo out of every product. Destructive enough to
		// deserve its own key rather than sharing `u`, and a chord rather than
		// the capital it used to be — `U` never arrived here at all, because
		// handleKey resolves it as the upgrade key before any lens is asked.
		var all []string
		for _, r := range rows {
			all = append(all, r.name)
		}
		mm, cmd := m.clAssign(all, "")
		mm.notice = "every repo unassigned · no products left"
		return mm, cmd, true
	case "enter":
		if len(prods) == 0 {
			m.notice = "no product to assign to — " + m.keys.KeyFor("products.new") + " names one"
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
