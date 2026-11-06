package dispatch

// branching.go is the dispatch form's BRANCH choice: whether a dispatch cuts
// a branch of its own.
//
// Every dispatch used to, because every dispatch was assumed to end in a pull
// request. Plenty of work is not that. Read off the reporting store, three
// dispatches in a row went out asking for an answer rather than a change:
// "review-163" ("quickly review it first and only provide the succinct
// report"), "byga-integration-sitrep" ("what's the status of byga
// integration?") and "find-mannequin" ("find it"). Each was handed a branch
// named after it and a contract telling it to "commit as you go, open the
// PR". The branches are litter in repos that already carry 170-odd, and the
// contract asked for a pull request the brief did not want. The only other way
// to a session with no branch of its own was to start one by hand in a shared
// checkout and adopt it — every adopted record on that store sits in a `main`
// or `dev` checkout.
//
// BranchNone is that work, and it is wider than one repo: a conversation about
// a product, or about one repo in it, that can follow the conversation to any
// of the product's other repos. So it is not a checkout at all. The session
// runs in a folder of its own under the state dir, whose CLAUDE.md names every
// repo in the product and where it lives, and it is given each repo's
// canonical checkout with --add-dir (see branchless.go). It cuts nothing and
// writes nothing to those checkouts: they are the human's own, and its
// contract says to answer in the conversation (contract.go). A change the
// conversation turns up is a dispatch of its own, with a branch.

import "strings"

// Branching is whether a dispatch cuts a branch of its own.
type Branching string

const (
	// BranchNew cuts <prefix><slug> from the root in a worktree of its own:
	// work that ends in a pull request. It is the default, and what every
	// dispatch did before this.
	BranchNew Branching = "new"
	// BranchNone cuts nothing: a session over the product, reading its repos'
	// own checkouts, for work whose answer is in the conversation.
	BranchNone Branching = "none"
)

// DefaultBranching is what a dispatch does when nothing chose.
const DefaultBranching = BranchNew

// Branchings is the offer order, the default first.
func Branchings() []Branching { return []Branching{BranchNew, BranchNone} }

// Normalize folds anything but BranchNone onto the default, so a caller that
// chose nothing, and a record that predates the choice, cut a branch as they
// always did.
func (b Branching) Normalize() Branching {
	if Branching(strings.TrimSpace(string(b))) == BranchNone {
		return BranchNone
	}
	return BranchNew
}

// None reports whether b cuts no branch.
func (b Branching) None() bool { return b.Normalize() == BranchNone }

// Hint is the one line a form shows beside the choice. Like Mode's it names
// no keys, because the two forms bind different ones.
func (b Branching) Hint() string {
	if b.None() {
		return "no branch — a conversation across the product's repos, for a review, a question or a look"
	}
	return "cuts its own branch in a checkout of its own, for work that ends in a pull request"
}
