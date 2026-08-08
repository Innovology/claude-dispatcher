package cockpit

// seed_product.go holds the data owned by the product lens (lens 3): the review
// queue, the team scoreboard, the shipped history, per-PR diffs and findings,
// and the per-product velocity tiles. It is a faithful transcription of the
// design's REVIEWS / TEAM / TEAM_VERDICT / SHIPPED / DIFFS_BY_PR / FINDINGS /
// PRODUCT_VELOCITY tables. Names are lens-prefixed where they could collide.

// ---- reviews ----------------------------------------------------------------

// reviewerInfo is the "a reviewer dispatcher looked at this" annotation on a PR.
type reviewerInfo struct{ state, label, color string }

type reviewItem struct {
	pr, title, repo, author, waiting, age, checks, size, summary string
	mine                                                         bool
	reviewer                                                     *reviewerInfo
}

// ---- team -------------------------------------------------------------------

type teamRow struct {
	who                          string
	live7, opened, reviews, debt int
	latency                      string
	me                           bool
}

// ---- shipped ----------------------------------------------------------------

type shippedItem struct {
	feature, repo, pr, at, session, closedBy, prompt string
}

type shippedDay struct {
	day   string
	items []shippedItem
}

// ---- diffs + findings -------------------------------------------------------

// ---- velocity tiles ---------------------------------------------------------

type velTile struct{ k, v, band, spark string }
