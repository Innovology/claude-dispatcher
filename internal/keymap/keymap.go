// Package keymap is the one place that decides what a key means.
//
// Every action the cockpit offers has a stable id and a default key, and
// `[keys]` in config.toml rebinds any of them. Two rules make that safe to have
// at all:
//
//   - The help sheet is BUILT from this table, so `?` cannot describe a binding
//     the cockpit does not have. A hand-written key sheet beside a remappable
//     keymap is a lie waiting for its first rebind.
//   - A key bound to two actions in one scope is refused at load, with both
//     actions named. Silently letting the last one win would make a typo in a
//     config file present as a key that stopped working.
//
// Scope is what lets `n` be "new product" on the products lens and "next match"
// while a search is up: keys are resolved against the screen that has the
// keyboard, not globally. Resolution translates a pressed key into the action's
// DEFAULT key, so the cockpit's own switches go on reading `case "n":` — the
// defaults are the canonical spelling of an action, and rebinding changes which
// physical key arrives at it, not what the code is about.
package keymap

import (
	"fmt"
	"sort"
	"strings"
)

// Scope is the screen a binding belongs to. A key means whatever the scope with
// the keyboard says it means.
type Scope string

const (
	// Global is resolved before any lens: these keys work everywhere.
	Global Scope = "global"
	// Fleet is the triage lens (1).
	Fleet Scope = "fleet"
	// Products is the portfolio table (2).
	Products Scope = "products"
	// Editor is the assignment editor, `a` on the products lens.
	Editor Scope = "editor"
	// Fold is an unfolded repo row inside the editor, while it has the keyboard.
	Fold Scope = "fold"
	// Search is live only while a query is up. It is resolved before the lens
	// beneath it, which is how `n` can step to the next match on a screen where
	// `n` otherwise makes a product — two meanings that never apply at once.
	Search Scope = "search"
	// List is the remaining lenses: backlog, usage, decisions, velocity.
	List Scope = "list"
)

// Binding is one action: what it is called, where it applies, the key it ships
// with, and the sentence `?` prints for it.
type Binding struct {
	Action string
	Scope  Scope
	Key    string
	Help   string
	// Fixed marks a binding that may not be rebound. Only ctrl+c is: nothing may
	// trap the process, so the key that always quits cannot be configured away.
	Fixed bool
	// Section groups bindings under a heading on the help sheet. Bindings with
	// no section are still bound, just not printed (motion keys the sheet
	// describes once).
	Section string
}

// Defaults is every action the cockpit has. It is the single source for what a
// key does, what `?` says, and what `[keys]` may name.
var Defaults = []Binding{
	// ---- global -------------------------------------------------------------
	{Action: "quit.now", Scope: Global, Key: "ctrl+c", Fixed: true,
		Help: "quit · never rebindable, nothing may trap the process", Section: "anywhere"},
	{Action: "quit", Scope: Global, Key: "q", Help: "quit", Section: "anywhere"},
	{Action: "help", Scope: Global, Key: "?", Help: "this help", Section: "anywhere"},
	{Action: "palette", Scope: Global, Key: ":", Help: "command palette", Section: "anywhere"},
	{Action: "settings", Scope: Global, Key: ",", Help: "settings", Section: "anywhere"},
	{Action: "dispatch.new", Scope: Global, Key: "+", Help: "new dispatch, repo first", Section: "anywhere"},
	{Action: "upgrade", Scope: Global, Key: "U",
		Help: "upgrade to the published build — behind the cockpit, in place", Section: "anywhere"},
	{Action: "undo", Scope: Global, Key: "ctrl+z", Help: "put back the last thing you cleared", Section: "anywhere"},
	{Action: "redraw", Scope: Global, Key: "ctrl+l", Help: "redraw a garbled screen", Section: "anywhere"},

	// ---- the fleet ----------------------------------------------------------
	{Action: "fleet.down", Scope: Fleet, Key: "j", Help: "move the cursor — the panel and the key hints follow it", Section: "work the fleet"},
	{Action: "fleet.up", Scope: Fleet, Key: "k"},
	{Action: "fleet.first", Scope: Fleet, Key: "g", Help: "first row · last row is G", Section: "work the fleet"},
	{Action: "fleet.last", Scope: Fleet, Key: "G"},
	{Action: "fleet.filter", Scope: Fleet, Key: "f", Help: "filter: all → wants you → running → history", Section: "work the fleet"},
	{Action: "fleet.attach", Scope: Fleet, Key: "enter", Help: "attach the session", Section: "work the fleet"},
	{Action: "fleet.approve", Scope: Fleet, Key: "y", Help: "approve the merge, or mark it shipped once it has commits", Section: "work the fleet"},
	{Action: "fleet.kill", Scope: Fleet, Key: "x", Help: "kill it · the branch and any dirty worktree survive", Section: "work the fleet"},
	{Action: "fleet.skip", Scope: Fleet, Key: "s", Help: "skip · send it to the back, it comes round again", Section: "work the fleet"},
	{Action: "fleet.park", Scope: Fleet, Key: "p", Help: "park it · say why, it waits below the fleet · again unparks", Section: "work the fleet"},
	{Action: "fleet.dispatch", Scope: Fleet, Key: "d", Help: "dispatch · pick a repo, say what it does", Section: "work the fleet"},
	{Action: "fleet.history", Scope: Fleet, Key: "h", Help: "history · every dispatcher whose session is over, and back again", Section: "what has finished"},
	{Action: "fleet.openpr", Scope: Fleet, Key: "o", Help: "open its pull request", Section: "what has finished"},

	// ---- products lens ------------------------------------------------------
	{Action: "products.down", Scope: Products, Key: "j"},
	{Action: "products.up", Scope: Products, Key: "k"},
	{Action: "products.open", Scope: Products, Key: "enter", Help: "open the product panel beside the table", Section: "products · lens 2"},
	{Action: "products.assign", Scope: Products, Key: "a", Help: "assign repos to products", Section: "products · lens 2"},
	{Action: "products.new", Scope: Products, Key: "n", Help: "new product — names it and moves the marked repos in", Section: "products · lens 2"},

	// ---- assignment editor --------------------------------------------------
	{Action: "editor.down", Scope: Editor, Key: "j"},
	{Action: "editor.up", Scope: Editor, Key: "k"},
	{Action: "editor.close", Scope: Editor, Key: "esc"},
	{Action: "editor.pane", Scope: Editor, Key: "tab", Help: "between the repo list and the products", Section: "products · lens 2"},
	{Action: "editor.mark", Scope: Editor, Key: "space", Help: "mark a repo · enter moves every marked one", Section: "products · lens 2"},
	{Action: "editor.move", Scope: Editor, Key: "enter"},
	// The same action as on the lens behind it, deliberately sharing its id:
	// "new product" is one thing to the person pressing it, and two ids would
	// mean rebinding it moved half of it — measured, `n` went on making products
	// in the editor after being rebound on the products lens.
	{Action: "products.new", Scope: Editor, Key: "n"},
	{Action: "editor.linear", Scope: Editor, Key: "l", Help: "the Linear token this product's backlog is read with", Section: "products · lens 2"},
	{Action: "editor.unassign", Scope: Editor, Key: "u", Help: "take repos back out of a product · ctrl+u starts over", Section: "products · lens 2"},
	{Action: "editor.reset", Scope: Editor, Key: "ctrl+u"},
	{Action: "editor.where", Scope: Editor, Key: "p", Help: "unfold a repo · where it lives, and its every checkout", Section: "products · lens 2"},

	// ---- inside an unfolded row ---------------------------------------------
	{Action: "fold.down", Scope: Fold, Key: "j", Help: "in the fold: pick the checkout this repo works in", Section: "products · lens 2"},
	{Action: "fold.up", Scope: Fold, Key: "k"},
	{Action: "fold.pick", Scope: Fold, Key: "enter", Help: "in the fold: set it · again on the pinned one clears it", Section: "products · lens 2"},
	{Action: "fold.leave", Scope: Fold, Key: "esc"},

	// ---- search -------------------------------------------------------------
	{Action: "search.open", Scope: Products, Key: "/", Help: "search · fzf matching, live, in place", Section: "search"},
	{Action: "search.open", Scope: Editor, Key: "/"},
	{Action: "search.next", Scope: Search, Key: "n", Help: "jump to the next match", Section: "search"},
	{Action: "search.prev", Scope: Search, Key: "ctrl+n", Help: "jump to the previous match", Section: "search"},
	{Action: "search.clear", Scope: Search, Key: "esc", Help: "clear the search", Section: "search"},

	// ---- the other lenses ---------------------------------------------------
	{Action: "list.down", Scope: List, Key: "j", Help: "up and down the list", Section: "the other lenses"},
	{Action: "list.up", Scope: List, Key: "k"},
	{Action: "list.open", Scope: List, Key: "enter", Help: "open what is selected", Section: "the other lenses"},
	{Action: "list.pick", Scope: List, Key: "space", Help: "pick a backlog ticket", Section: "the other lenses"},
	{Action: "list.dispatch", Scope: List, Key: "ctrl+d", Help: "dispatch every picked ticket", Section: "the other lenses"},
}

// Map is a resolved keymap: the defaults with the user's overrides applied.
type Map struct {
	bindings []Binding
	// byKey is (scope, pressed key) → the action's DEFAULT key, which is what
	// the cockpit's switches are written against.
	byKey map[Scope]map[string]string
	// canonical is (scope, default key) → true, so a key whose action has been
	// rebound away can be swallowed rather than firing the action it used to.
	canonical map[Scope]map[string]bool
}

// New resolves Defaults against the overrides in `[keys]` (action id → key).
//
// An override naming an unknown action, or rebinding a fixed one, is an error
// rather than a shrug: the human wrote it expecting it to do something, and the
// only feedback a silent drop gives is a key that does nothing.
func New(overrides map[string]string) (*Map, error) {
	m := &Map{
		byKey:     map[Scope]map[string]string{},
		canonical: map[Scope]map[string]bool{},
	}
	m.bindings = append(m.bindings, Defaults...)

	known := map[string]bool{}
	for _, b := range m.bindings {
		known[b.Action] = true
	}
	var bad []string
	for action, key := range overrides {
		if !known[action] {
			bad = append(bad, fmt.Sprintf("%q is not an action this cockpit has", action))
			continue
		}
		if key = strings.TrimSpace(key); key == "" {
			bad = append(bad, fmt.Sprintf("%q is bound to nothing — remove the line to keep the default", action))
			continue
		}
		for i := range m.bindings {
			if m.bindings[i].Action != action {
				continue
			}
			if m.bindings[i].Fixed {
				bad = append(bad, fmt.Sprintf("%q cannot be rebound", action))
				break
			}
			m.bindings[i].Key = key
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(bad, "; "))
	}

	// One key, one action, per scope. Checked after every override is applied,
	// because a collision is usually between a rebind and a default that was
	// fine until it arrived.
	taken := map[Scope]map[string]string{}
	var clashes []string
	for _, b := range m.bindings {
		if taken[b.Scope] == nil {
			taken[b.Scope] = map[string]string{}
		}
		if prev, dup := taken[b.Scope][b.Key]; dup && prev != b.Action {
			clashes = append(clashes, fmt.Sprintf("%q is bound to both %q and %q in %s", b.Key, prev, b.Action, b.Scope))
			continue
		}
		taken[b.Scope][b.Key] = b.Action
	}
	// A lens is asked before the global keys are, so a lens binding that reuses
	// a global key does not share it — it takes it, everywhere that lens is up.
	// Bind `U` to a fleet action and the upgrade key is gone from the triage
	// lens and nowhere says why.
	for _, b := range m.bindings {
		if b.Scope == Global {
			continue
		}
		if owner, taken := taken[Global][b.Key]; taken {
			clashes = append(clashes, fmt.Sprintf("%q is %q everywhere, so %q in %s could never run", b.Key, owner, b.Action, b.Scope))
		}
	}
	if len(clashes) > 0 {
		sort.Strings(clashes)
		return nil, fmt.Errorf("%s", strings.Join(clashes, "; "))
	}

	for _, b := range m.bindings {
		if m.byKey[b.Scope] == nil {
			m.byKey[b.Scope] = map[string]string{}
			m.canonical[b.Scope] = map[string]bool{}
		}
		m.byKey[b.Scope][b.Key] = defaultKeyOf(b.Action)
		m.canonical[b.Scope][defaultKeyOf(b.Action)] = true
	}
	return m, nil
}

func defaultKeyOf(action string) string {
	for _, d := range Defaults {
		if d.Action == action {
			return d.Key
		}
	}
	return ""
}

// Resolve translates a pressed key into the canonical key the cockpit's own
// switch for that scope is written against.
//
// Three outcomes, and the third is the one that makes rebinding correct rather
// than merely possible:
//
//   - the key is bound in this scope → the bound action's default key;
//   - the key is bound to nothing here → itself, so text entry and the keys
//     this package does not own still arrive;
//   - the key is the DEFAULT of an action that has since been rebound away →
//     "", swallowed. Passing it through would leave the old key working
//     alongside the new one, which is not what rebinding means.
func (m *Map) Resolve(scope Scope, pressed string) string {
	if m == nil {
		return pressed
	}
	if canon, ok := m.byKey[scope][pressed]; ok {
		return canon
	}
	if m.canonical[scope][pressed] {
		return ""
	}
	return pressed
}

// KeyFor is the key an action currently answers to, for the screens that print
// their own hints.
func (m *Map) KeyFor(action string) string {
	if m == nil {
		return defaultKeyOf(action)
	}
	for _, b := range m.bindings {
		if b.Action == action {
			return b.Key
		}
	}
	return ""
}

// Bindings returns every binding, in declaration order, for the help sheet.
func (m *Map) Bindings() []Binding {
	if m == nil {
		return Defaults
	}
	return m.bindings
}
