package cockpit

// types_product.go holds the data owned by the product panel (inside lens 2,
// the products area): the review
// queue, the team scoreboard, the shipped history, per-PR diffs and findings,
// and the per-product velocity tiles. It is a faithful transcription of the
// design's REVIEWS / TEAM / TEAM_VERDICT / SHIPPED / DIFFS_BY_PR / FINDINGS /
// PRODUCT_VELOCITY tables. Names are lens-prefixed where they could collide.

// ---- reviews ----------------------------------------------------------------

// reviewerInfo is the "a reviewer dispatcher looked at this" annotation on a PR.
type reviewerInfo struct{ label, color string }

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

// id is the dispatch record's own id, and is what the resume overlay acts
// through — a feature name can belong to several records, its id cannot.
type shippedItem struct {
	id, feature, repo, pr, at, session, closedBy, prompt string
}

type shippedDay struct {
	day   string
	items []shippedItem
}

// ---- history ----------------------------------------------------------------

// historyItem is one finished dispatcher on the product's HISTORY tab: every
// session that is over, whether it shipped or not.
//
// SHIPPED cannot answer for these. It is a ship log — grouped by the day a
// feature went live, and built only from records with a merge or a deploy
// behind them — so a dispatcher that was killed, that ended without a PR, or
// that was marked shipped by hand appears on it nowhere, and used to be
// unreachable from every screen in the product. ended says how it finished and
// session is the transcript to resume; an empty session means there is nothing
// to resume and the overlay says so rather than pretending.
type historyItem struct {
	id, feature, repo, pr, at, ended, session, prompt string
}

// ---- diffs + findings -------------------------------------------------------

// ---- velocity tiles ---------------------------------------------------------

type velTile struct{ k, v, band, spark string }
