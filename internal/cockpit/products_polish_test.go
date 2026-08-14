package cockpit

// products_polish_test.go pins the four small things the products lens and its
// assignment editor were missing: an honest product count in the header, the
// keys advertised in the footer and the help sheet, and a repo list whose
// cursor stays on screen.

import (
	"fmt"
	"strings"
	"testing"
)

// The header counts what the user has grouped. "unassigned" is the bucket every
// unmapped repo falls into, so counting it made a fresh install — nothing
// grouped, nothing to group by — report "1 product".
func TestPortfolioLineDoesNotCountTheUnassignedBucket(t *testing.T) {
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })

	m := newModel()
	products = []product{{name: clUnassigned, repos: "4 repos"}}
	if got := m.portfolioLine(); strings.Contains(got, "product") {
		t.Errorf("nothing grouped, yet the header claims a product: %q", got)
	}

	products = []product{{name: "acme", repos: "2 repos"}, {name: clUnassigned, repos: "2 repos"}}
	if got := m.portfolioLine(); !strings.Contains(got, "1 product") {
		t.Errorf("one real product: got %q, want it to say 1 product", got)
	}

	products = []product{{name: "acme"}, {name: "orbit"}, {name: clUnassigned}}
	if got := m.portfolioLine(); !strings.Contains(got, "2 products") {
		t.Errorf("two real products: got %q", got)
	}
}

// a and n are the only way to make a product without hand-editing TOML, so the
// lens has to say so — the footer is the only thing on screen that can.
func TestProductsFooterAdvertisesTheAssignKeys(t *testing.T) {
	m := newModel()
	m.lens = "products"
	got := m.footerHelp()
	for _, want := range []string{"a assign", "n new product"} {
		if !strings.Contains(got, want) {
			t.Errorf("products footer is missing %q: %q", want, got)
		}
	}
}

// The help sheet is where a user goes when the footer has scrolled past. Every
// key the editor answers to should be findable there.
func TestHelpSheetDocumentsTheAssignKeys(t *testing.T) {
	m := newModel()
	m.width, m.height = 150, 44
	out := m.viewHelp(m.width, m.height)
	for _, want := range []string{"products", "assign repos to products", "new product", "tab"} {
		if !strings.Contains(out, want) {
			t.Errorf("help sheet is missing %q:\n%s", want, out)
		}
	}
}

// The list scrolls with the cursor. It used to render every repo and let
// clampLines cut the overflow, so on a portfolio taller than the terminal the
// selection walked off the bottom and became invisible.
func TestClLeftKeepsTheCursorOnScreen(t *testing.T) {
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })

	// Zero-padded: clRepos sorts by name, so "repo-9" would otherwise sit at
	// index 59 and the assertions below would be testing the sort, not the
	// scrolling.
	var refs []repoRef
	for i := 0; i < 60; i++ {
		refs = append(refs, repoRef{name: fmt.Sprintf("repo-%02d", i), forge: "gh", last: "1d"})
	}
	reposByProduct = map[string][]repoRef{clUnassigned: refs}

	m := newModel()
	m.clOpen, m.clPane = true, "repos"

	const h = 20
	for _, sel := range []int{0, 30, 59} {
		m.clRepo = sel
		lines := m.clLeft(70, h)
		if len(lines) > h {
			t.Errorf("cursor %d: pane is %d lines, exceeds the %d available", sel, len(lines), h)
		}
		want := fmt.Sprintf("repo-%02d", sel)
		found := false
		for _, ln := range lines {
			// The selected row is the one carrying the cursor marker.
			if strings.Contains(ln, want) && strings.Contains(ln, "▸") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cursor %d (%s) is not on screen:\n%s", sel, want, strings.Join(lines, "\n"))
		}
	}

	// A scrolling list says where you are in it; one that fits does not.
	if got := strings.Join(m.clLeft(70, h), "\n"); !strings.Contains(got, "60 of 60") {
		t.Errorf("expected a position counter while scrolling:\n%s", got)
	}
	reposByProduct = map[string][]repoRef{clUnassigned: refs[:3]}
	m.clRepo = 0
	if got := strings.Join(m.clLeft(70, h), "\n"); strings.Contains(got, " of 3") {
		t.Errorf("a list that fits should not count itself:\n%s", got)
	}
}

// A lens digit is a "take me somewhere else" key, so it must not leave the
// assignment editor open behind it. The editor lets digits through to the
// router (that is how you leave), but until v4 it stayed open, and coming back
// to lens 2 dropped you into a half-marked editor you did not ask for.
func TestALensDigitClosesTheAssignmentEditor(t *testing.T) {
	saved := captureVars()
	t.Cleanup(func() { restoreVars(saved) })
	installFixture(t)

	m := newModel()
	m.width, m.height = 190, 44
	m = press(m, "2")
	m = press(m, "a")
	if !m.clOpen {
		t.Fatal("a should open the assignment editor")
	}
	m = press(m, "space") // mark whatever is under the cursor
	m = press(m, "3")
	if m.lens != "backlog" {
		t.Errorf("3 from the editor should reach the backlog, lens = %q", m.lens)
	}
	if m.clOpen || m.clNaming || len(m.clMarked) != 0 {
		t.Errorf("the editor survived the digit: open=%v naming=%v marked=%d",
			m.clOpen, m.clNaming, len(m.clMarked))
	}
}
