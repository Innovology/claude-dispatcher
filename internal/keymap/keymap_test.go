package keymap

import (
	"strings"
	"testing"
)

func mustNew(t *testing.T, overrides map[string]string) *Map {
	t.Helper()
	m, err := New(overrides)
	if err != nil {
		t.Fatalf("New(%v): %v", overrides, err)
	}
	return m
}

// The defaults have to be self-consistent, or the first thing a user meets is
// an error about a file they have not written yet.
func TestDefaultsHaveNoCollisions(t *testing.T) {
	if _, err := New(nil); err != nil {
		t.Fatalf("the shipped defaults collide: %v", err)
	}
}

// Every action needs an id, a scope and a key; and an id must not be reused for
// two different keys, since `[keys]` names actions and would rebind both.
func TestDefaultsAreWellFormed(t *testing.T) {
	keyOf := map[string]string{}
	for _, b := range Defaults {
		if b.Action == "" || b.Scope == "" || b.Key == "" {
			t.Errorf("incomplete binding: %+v", b)
		}
		if prev, seen := keyOf[b.Action]; seen && prev != b.Key {
			t.Errorf("action %q ships as both %q and %q — one id is one key", b.Action, prev, b.Key)
		}
		keyOf[b.Action] = b.Key
	}
}

func TestResolvePassesUnboundKeysThrough(t *testing.T) {
	m := mustNew(t, nil)
	// A key this package does not own reaches the lens unchanged — text entry
	// and everything else depends on it.
	if got := m.Resolve(Editor, "Z"); got != "Z" {
		t.Errorf("got %q, want the key itself", got)
	}
	if got := m.Resolve(Editor, "p"); got != "p" {
		t.Errorf("a default binding should resolve to itself, got %q", got)
	}
}

// Rebinding moves the action to the new key AND takes the old one away. Leaving
// the default working beside the override is not what rebinding means.
func TestRebindMovesTheActionAndFreesTheOldKey(t *testing.T) {
	m := mustNew(t, map[string]string{"products.new": "N"})

	if got := m.Resolve(Products, "N"); got != "n" {
		t.Errorf("the new key should resolve to the action's canonical key, got %q", got)
	}
	if got := m.Resolve(Products, "n"); got != "" {
		t.Errorf("the old key should be swallowed, got %q", got)
	}
	if got := m.KeyFor("products.new"); got != "N" {
		t.Errorf("KeyFor should report the live binding, got %q", got)
	}
}

// Scope is what lets one key mean two things on one screen without either being
// ambiguous: `n` makes a product, and steps to the next match while a search is
// up, because only one of those scopes has the keyboard at a time.
func TestScopesAreIndependent(t *testing.T) {
	m := mustNew(t, nil)
	if got := m.Resolve(Products, "n"); got != "n" {
		t.Errorf("products n should be the new-product key, got %q", got)
	}
	if got := m.Resolve(Search, "n"); got != "n" {
		t.Errorf("search n should resolve, got %q", got)
	}
	// And a rebind in one scope leaves the other alone.
	m2 := mustNew(t, map[string]string{"search.next": "]"})
	if got := m2.Resolve(Products, "n"); got != "n" {
		t.Errorf("rebinding the search jump must not disturb new-product, got %q", got)
	}
	if got := m2.Resolve(Search, "]"); got != "n" {
		t.Errorf("got %q, want the search-next canonical key", got)
	}
}

// A key bound twice in one scope is refused, with both actions named. The last
// one quietly winning would present as a key that stopped working.
func TestCollisionIsRefusedAndNamesBothActions(t *testing.T) {
	_, err := New(map[string]string{"products.assign": "n"})
	if err == nil {
		t.Fatal("binding a second action to n on the products lens should be refused")
	}
	for _, want := range []string{"products.assign", "products.new", "\"n\""} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %s, got: %v", want, err)
		}
	}
}

func TestUnknownActionAndFixedKeyAreRefused(t *testing.T) {
	if _, err := New(map[string]string{"fleet.teleport": "t"}); err == nil {
		t.Error("an unknown action should be refused, not dropped")
	} else if !strings.Contains(err.Error(), "fleet.teleport") {
		t.Errorf("the error should name it, got: %v", err)
	}

	// Nothing may trap the process.
	if _, err := New(map[string]string{"quit.now": "Q"}); err == nil {
		t.Error("ctrl+c must not be rebindable")
	}

	if _, err := New(map[string]string{"products.new": "  "}); err == nil {
		t.Error("binding an action to nothing should be refused")
	}
}

// Two scopes share the search.open id on purpose — the same action, offered on
// the products table and in its editor. Rebinding it moves both.
func TestOneActionInTwoScopesMovesTogether(t *testing.T) {
	// "z" only has to be a key free in both scopes; it is not about z. It was
	// "s" until the editor bound that to a repo's tmux server, and the refusal
	// that produced is the collision check doing its job.
	m := mustNew(t, map[string]string{"search.open": "z"})
	for _, scope := range []Scope{Products, Editor} {
		if got := m.Resolve(scope, "z"); got != "/" {
			t.Errorf("%s: got %q, want the search canonical key", scope, got)
		}
		if got := m.Resolve(scope, "/"); got != "" {
			t.Errorf("%s: the old key should be gone, got %q", scope, got)
		}
	}
}

// A nil map is the unconfigured cockpit and must behave as the defaults, so no
// caller has to guard for it.
func TestNilMapIsTheDefaults(t *testing.T) {
	var m *Map
	if got := m.Resolve(Products, "n"); got != "n" {
		t.Errorf("got %q", got)
	}
	if got := m.KeyFor("products.new"); got != "n" {
		t.Errorf("got %q", got)
	}
	if len(m.Bindings()) != len(Defaults) {
		t.Error("a nil map should report the defaults")
	}
}
