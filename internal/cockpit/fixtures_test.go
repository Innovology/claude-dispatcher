package cockpit

import "testing"

// fixtures_test.go gives the interaction tests their own data.
//
// The cockpit's data vars now start empty (data.go) so nothing fabricated can
// reach a real screen. Tests that need something to act on install a fixture
// here and restore the empty state afterwards, rather than leaning on ambient
// package state — which is what let the design's mock portfolio survive as
// "test coverage" for so long. Names are deliberately generic so a fixture can
// never be mistaken for real output.

// installFixture fills the data vars a lens test needs and restores the
// previous values when the test ends.
func installFixture(t *testing.T) {
	t.Helper()

	prevProducts, prevOrder := products, productOrder
	prevTickets := backlogTickets
	prevReviews, prevTeam, prevVerdict := reviews, team, teamVerdict
	prevShipped, prevVelocity := shipped, productVelocity
	prevDispatches := dispatches
	prevStats, prevRepos := productStats, reposByProduct
	t.Cleanup(func() {
		products, productOrder = prevProducts, prevOrder
		backlogTickets = prevTickets
		reviews, team, teamVerdict = prevReviews, prevTeam, prevVerdict
		shipped, productVelocity = prevShipped, prevVelocity
		dispatches = prevDispatches
		productStats, reposByProduct = prevStats, prevRepos
	})

	products = []product{
		{name: "alpha", repos: "2 repos", forge: "github", inflight: 3, needs: 1, review: 1, live: 1, spark: "▁▂▃", lead: "2h"},
		{name: "beta", repos: "1 repo", forge: "github", inflight: 1, needs: 0, review: 1, live: 0, spark: "▁▁▂", lead: "5h"},
	}
	productOrder = []string{"alpha", "beta"}
	reposByProduct = map[string][]repoRef{
		"alpha": {{name: "alpha-api"}, {name: "alpha-web"}},
		"beta":  {{name: "beta-svc"}},
	}
	productStats = map[string]productStat{
		"alpha": {dispatched7d: 6, closed7d: 4, rejected7d: 1, budget: 6, pace: 1.0, note: "note"},
		"beta":  {dispatched7d: 2, closed7d: 1, rejected7d: 0, budget: 2, pace: 0.5, note: "note"},
	}

	dispatches = []dispatch{
		{feature: "one", repo: "alpha-api", product: "alpha", state: "needs", age: "4m", urgent: true},
		{feature: "two", repo: "alpha-web", product: "alpha", state: "review", age: "1h"},
		{feature: "three", repo: "beta-svc", product: "beta", state: "live", age: "2h"},
	}

	// Index 3 is deliberately already taken — the backlog test asserts that
	// dispatching a taken ticket is refused.
	backlogTickets = []ticket{
		{id: "T-1", src: "gh", title: "first", repo: "alpha-api", product: "alpha", pri: "high", age: "1d"},
		{id: "T-2", src: "gh", title: "second", repo: "alpha-api", product: "alpha", pri: "med", age: "2d"},
		{id: "T-3", src: "gh", title: "third", repo: "alpha-web", product: "alpha", pri: "low", age: "3d"},
		{id: "T-4", src: "gh", title: "fourth", repo: "beta-svc", product: "beta", pri: "med", age: "4d", taken: "one"},
	}

	// cursor 0 and 1 are the user's own PRs (not self-approvable); index 2 is
	// someone else's, so the approve/request-changes keys apply.
	reviews = map[string][]reviewItem{
		"alpha": {
			{pr: "#1", title: "mine one", repo: "alpha-api", author: "you", waiting: "them", age: "1h", checks: "✓ passed", size: "+10 −2", mine: true},
			{pr: "#2", title: "mine two", repo: "alpha-api", author: "you", waiting: "them", age: "2h", checks: "✓ passed", size: "+4 −1", mine: true},
			{pr: "#3", title: "theirs", repo: "alpha-web", author: "sam", waiting: "you", age: "3h", checks: "✓ passed", size: "+20 −5"},
		},
	}
	team = map[string][]teamRow{
		"alpha": {
			{who: "you", live7: 4, opened: 3, reviews: 2, debt: 1, latency: "20m", me: true},
			{who: "sam", live7: 2, opened: 1, reviews: 4, debt: 0, latency: "1h"},
		},
	}
	teamVerdict = map[string]string{"alpha": "verdict"}
	shipped = map[string][]shippedDay{
		"alpha": {{day: "today", items: []shippedItem{
			{feature: "one", repo: "alpha-api", pr: "#1", at: "10:00", session: "disp-one", closedBy: "merge", prompt: "do the thing"},
			{feature: "two", repo: "alpha-web", pr: "#2", at: "11:00", session: "disp-two", closedBy: "merge", prompt: "do the other thing"},
		}}},
	}
	productVelocity = map[string][]velTile{
		"alpha": {{k: "lead time", v: "2h", band: "high", spark: "▁▂▃"}},
	}
}
