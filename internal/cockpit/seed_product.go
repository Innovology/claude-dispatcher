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

// reviews mirrors REVIEWS: open PRs per product, newest work first.
var reviews = map[string][]reviewItem{
	"cortiva": {
		{pr: "#148", title: "audit log date + actor filters", repo: "cortiva-hq", author: "you · claude", waiting: "priya", age: "2h", checks: "✓ 4/4", size: "+268 −41", mine: true,
			summary: "Filters serialise into the url. The filter names become public api — that is the thing worth arguing about."},
		{pr: "#150", title: "session replay behind a 30d ttl", repo: "cortiva-api", author: "you · claude", waiting: "approved", age: "6h", checks: "✓ 4/4", size: "+330 −66", mine: true,
			summary: "Two approvals already. Nothing blocks this but the merge key."},
		{pr: "#147", title: "seat limit grace window", repo: "cortiva-api", author: "priya", waiting: "you", age: "5h", checks: "✓ 4/4", size: "+96 −12",
			reviewer: &reviewerInfo{state: "done", label: "◆ reviewer found 3 things", color: cAmber},
			summary:  "Priya wrote this by hand. Grace window is 7 days, hard-coded — she flagged it for your opinion."},
		{pr: "#145", title: "mobile: offline write queue", repo: "cortiva-mobile", author: "marcus", waiting: "you", age: "1d", checks: "● 2/4", size: "+210 −30",
			reviewer: &reviewerInfo{state: "working", label: "● reviewer working · 4m", color: cGreen},
			summary:  "Builds on the cache from #48. Two checks still running after a rebase."},
		{pr: "#143", title: "infra: split staging workspace", repo: "cortiva-infra", author: "jo", waiting: "you", age: "2d", checks: "✗ plan failed", size: "+620 −410",
			summary: "Terraform plan fails on the prod workspace. Jo asked whether to split state files too."},
	},
	"altsports": {
		{pr: "#61", title: "polar radar for boat speed", repo: "altsports-web", author: "you · claude", waiting: "sam", age: "3h", checks: "● 2/5", size: "+512 −120", mine: true,
			summary: "Checks re-running after the second push."},
		{pr: "#87", title: "season fixture ocr fallback", repo: "altsports_1", author: "sam", waiting: "you", age: "1d", checks: "✓ passed", size: "+140 −22",
			summary: "Sam has been waiting a day. Small diff, clear tests."},
	},
	"dispatch": {
		{pr: "#77", title: "back off the status hook on nfs", repo: "claude-dispatcher", author: "you · claude", waiting: "merge queue", age: "1h", checks: "✓ passed", size: "+74 −30", mine: true,
			summary: "In the queue, no human needed."},
	},
	"northwind": {
		{pr: "!322", title: "per-region tenant split", repo: "nw-billing", author: "you · claude", waiting: "ana", age: "12m", checks: "● build", size: "+88 −12", mine: true,
			summary: "Draft — blocked on infra scope before it is reviewable."},
	},
	"kalish": {
		{pr: "#44", title: "ingest retry backoff", repo: "kalish-core", author: "dev", waiting: "you", age: "2d", checks: "✓ passed", size: "+61 −8",
			summary: "Two days waiting on you. Six lines of real change."},
	},
	"unassigned": {},
}

// ---- team -------------------------------------------------------------------

type teamRow struct {
	who                          string
	live7, opened, reviews, debt int
	latency                      string
	me                           bool
}

// team mirrors TEAM: who shipped, opened and reviewed in this product this week.
var team = map[string][]teamRow{
	"cortiva": {
		{who: "you", live7: 14, opened: 21, reviews: 2, latency: "6h 10m", debt: 3, me: true},
		{who: "priya", live7: 6, opened: 8, reviews: 11, latency: "38m", debt: 0},
		{who: "marcus", live7: 3, opened: 4, reviews: 9, latency: "1h 05m", debt: 1},
		{who: "jo", live7: 1, opened: 2, reviews: 6, latency: "2h 20m", debt: 0},
	},
	"altsports": {
		{who: "you", live7: 5, opened: 7, reviews: 1, latency: "9h 40m", debt: 2, me: true},
		{who: "sam", live7: 2, opened: 3, reviews: 5, latency: "1h 50m", debt: 0},
	},
	"dispatch": {
		{who: "you", live7: 9, opened: 11, reviews: 0, latency: "—", debt: 0, me: true},
	},
	"northwind": {
		{who: "you", live7: 1, opened: 4, reviews: 0, latency: "—", debt: 2, me: true},
		{who: "ana", live7: 2, opened: 2, reviews: 4, latency: "3h 10m", debt: 1},
	},
	"kalish": {
		{who: "you", live7: 0, opened: 3, reviews: 0, latency: "—", debt: 1, me: true},
		{who: "dev", live7: 1, opened: 1, reviews: 2, latency: "5h 00m", debt: 2},
	},
	"unassigned": {},
}

// teamVerdict mirrors TEAM_VERDICT: the one-line read on where the queue sits.
var teamVerdict = map[string]string{
	"cortiva":   "You opened 21 PRs and reviewed 2. Priya reviewed 11 and shipped 6. The queue in this product is waiting on you, not on the agents.",
	"altsports": "Seven PRs opened, one review given. Sam is carrying the review load for a product you dispatch into twice as often.",
	"dispatch":  "You are the only person here — nothing to balance, and nothing to catch your mistakes either.",
	"northwind": "Two PRs sat waiting on you while Release-Prod failed twice. Ana reviewed everything she was asked to.",
	"kalish":    "Three dispatched, none shipped, one PR waiting on you for two days. This product is stalled on review, not on work.",
}

// ---- shipped ----------------------------------------------------------------

type shippedItem struct {
	feature, repo, pr, at, session, closedBy, prompt string
}

type shippedDay struct {
	day   string
	items []shippedItem
}

// shipped mirrors SHIPPED: features already live, newest first, grouped by day.
var shipped = map[string][]shippedDay{
	"cortiva": {
		{day: "today", items: []shippedItem{
			{feature: "pdf export", repo: "cortiva-hq", pr: "#144", at: "3h ago", session: "a71f22c8", closedBy: "claude claimed · you closed", prompt: "pdf export of the report view, same pagination as print."},
			{feature: "seat limits", repo: "cortiva-api", pr: "#139", at: "5h ago", session: "4de90b17", closedBy: "claude claimed · you closed", prompt: "hard seat limits per plan with a grace window."},
		}},
		{day: "yesterday", items: []shippedItem{
			{feature: "bulk invite", repo: "cortiva-hq", pr: "#136", at: "19h ago", session: "bb1027fa", closedBy: "you closed", prompt: "invite up to 200 people from a pasted list."},
			{feature: "webhook signing", repo: "cortiva-api", pr: "#134", at: "22h ago", session: "90c4e5d1", closedBy: "claude claimed · you closed", prompt: "sign outbound webhooks, publish the verification snippet."},
			{feature: "offline cache", repo: "cortiva-mobile", pr: "#48", at: "1d ago", session: "5f2a8e60", closedBy: "you closed", prompt: "cache the last 7 days of records for offline read."},
		}},
		{day: "wed 5 aug", items: []shippedItem{
			{feature: "audit export", repo: "cortiva-hq", pr: "#129", at: "2d ago", session: "c3390dd2", closedBy: "claude claimed · you closed", prompt: "csv export of the audit log with the active filters applied."},
			{feature: "sso metadata", repo: "cortiva-api", pr: "#127", at: "2d ago", session: "7a1b4409", closedBy: "you closed", prompt: "serve saml metadata per tenant at a stable url."},
		}},
		{day: "tue 4 aug", items: []shippedItem{
			{feature: "rate cards", repo: "cortiva-hq", pr: "#121", at: "3d ago", session: "de55c103", closedBy: "you closed", prompt: "per-region rate cards on the pricing page."},
		}},
	},
	"altsports": {
		{day: "today", items: []shippedItem{{feature: "wind arrows", repo: "altsports-web", pr: "#59", at: "2h ago", session: "11ba7fd3", closedBy: "claude claimed · you closed", prompt: "wind direction arrows on the course map, readable at 100% zoom."}}},
		{day: "yesterday", items: []shippedItem{{feature: "result upload", repo: "altsports_1", pr: "#88", at: "20h ago", session: "6cc0a921", closedBy: "you closed", prompt: "upload results as a photo of the sheet, ocr the finish order."}}},
	},
	"dispatch": {
		{day: "today", items: []shippedItem{{feature: "hook path fix", repo: "claude-dispatcher", pr: "#74", at: "5h ago", session: "2f7e91aa", closedBy: "claude claimed · you closed", prompt: "the hook embeds a stale binary path after brew upgrade — fix it."}}},
		{day: "yesterday", items: []shippedItem{{feature: "ship stats", repo: "claude-dispatcher", pr: "#70", at: "1d ago", session: "8d0c34b5", closedBy: "you closed", prompt: "count commits by provenance so dispatch attribution is honest."}}},
	},
	"northwind": {
		{day: "wed 5 aug", items: []shippedItem{{feature: "dunning emails", repo: "nw-billing", pr: "!309", at: "2d ago", session: "ab77e210", closedBy: "you closed", prompt: "dunning email templates per region."}}},
	},
	"kalish": {
		{day: "mon 3 aug", items: []shippedItem{{feature: "ingest retries", repo: "kalish-core", pr: "#44", at: "4d ago", session: "f0912cc7", closedBy: "you closed", prompt: "retry failed ingests three times with backoff."}}},
	},
	"unassigned": {},
}

// ---- diffs + findings -------------------------------------------------------

// diffsByPR mirrors DIFFS_BY_PR: the hunk shown in the review overlay per PR.
var diffsByPR = map[string][]hunkLine{
	"#147": {
		{sign: " ", text: "func graceWindow(p Plan) time.Duration {"},
		{sign: "-", text: "\treturn 0"},
		{sign: "+", text: "\treturn 7 * 24 * time.Hour // TODO: per-plan"},
		{sign: " ", text: "}"},
		{sign: "+", text: "func (s *Seats) OverLimit(ctx context.Context, t Tenant) bool {"},
		{sign: "+", text: "\tif time.Since(t.ExceededAt) < graceWindow(t.Plan) {"},
		{sign: "+", text: "\t\treturn false"},
		{sign: "+", text: "\t}"},
		{sign: "+", text: "\treturn t.Used > t.Plan.Seats"},
		{sign: "+", text: "}"},
	},
	"#145": {
		{sign: " ", text: "class OfflineQueue {"},
		{sign: "+", text: "  Future<void> enqueue(Write w) async {"},
		{sign: "+", text: "    await _box.add(w.toJson());"},
		{sign: "+", text: "    _pending.value = _box.length;"},
		{sign: "+", text: "  }"},
		{sign: " ", text: "}"},
	},
	"#143": {
		{sign: " ", text: "terraform {"},
		{sign: "-", text: `  backend "s3" { key = "cortiva.tfstate" }`},
		{sign: "+", text: `  backend "s3" { key = "cortiva/${var.env}.tfstate" }`},
		{sign: " ", text: "}"},
	},
	"#148": {
		{sign: " ", text: "export function AuditFilters({ value, onChange }: Props) {"},
		{sign: "+", text: "  const [params, setParams] = useSearchParams();"},
		{sign: "+", text: "  // filter names are public api from here on"},
		{sign: " ", text: "  return ("},
	},
	"#87": {
		{sign: "+", text: "def ocr_fallback(image: Path) -> list[Result]:"},
		{sign: "+", text: "    text = tesseract(image, psm=6)"},
		{sign: "+", text: "    return parse_finish_order(text)"},
	},
}

// productDiffFallback mirrors DIFF_FALLBACK.hunk — shown when a PR has no diff.
var productDiffFallback = []hunkLine{
	{sign: " ", text: "export function run(opts: Options) {"},
	{sign: "-", text: "  return execute(opts);"},
	{sign: "+", text: "  const scoped = withDefaults(opts);"},
	{sign: "+", text: "  return execute(scoped);"},
	{sign: " ", text: "}"},
}

// findings mirrors FINDINGS: what a reviewer dispatcher reported on a PR.
var findings = map[string][]struct{ sev, text, color string }{
	"#147": {
		{sev: "blocking", text: "The 7 day grace window is hard-coded in three places — billing, the banner, and the cron.", color: cRed},
		{sev: "worth asking", text: "Downgrades inside the window are not covered by a test.", color: cAmber},
		{sev: "nit", text: "graceDays could come from the plan record you already load.", color: cDim},
	},
}

// ---- velocity tiles ---------------------------------------------------------

type velTile struct{ k, v, band, spark string }

// productVelocity mirrors PRODUCT_VELOCITY: four DORA-ish tiles per product.
// band is the tier word; render resolves it through bandColor.
var productVelocity = map[string][]velTile{
	"cortiva":    {{k: "deploys/day", v: "4.1", band: "elite", spark: "▃▄▄▅▆▅▇▆█"}, {k: "lead time", v: "4h 10m", band: "elite", spark: "█▇▆▆▅▅▄▃▃"}, {k: "change failure", v: "9%", band: "elite", spark: "▂▂▃▂▃▂▂▃▂"}, {k: "waiting on you", v: "1h 40m", band: "medium", spark: "▄▅▄▆▅▆▇▆▇"}},
	"altsports":  {{k: "deploys/day", v: "1.2", band: "high", spark: "▂▂▃▂▃▂▃▂▃"}, {k: "lead time", v: "6h 25m", band: "high", spark: "▇▇▆▇▆▆▅▆▅"}, {k: "change failure", v: "17%", band: "medium", spark: "▃▄▃▅▄▅▄▅▄"}, {k: "waiting on you", v: "2h 50m", band: "low", spark: "▆▇▆▇▇█▇█▇"}},
	"dispatch":   {{k: "deploys/day", v: "3.4", band: "elite", spark: "▄▅▅▆▆▇▆█▇"}, {k: "lead time", v: "1h 55m", band: "elite", spark: "▆▅▅▄▄▃▃▂▂"}, {k: "change failure", v: "4%", band: "elite", spark: "▁▂▁▂▁▁▂▁▁"}, {k: "waiting on you", v: "22m", band: "elite", spark: "▃▂▃▂▂▁▂▁▁"}},
	"northwind":  {{k: "deploys/day", v: "0.3", band: "low", spark: "▂▁▁▂▁▁▁▁▁"}, {k: "lead time", v: "2d 3h", band: "low", spark: "▆▆▇▇▇█▇██"}, {k: "change failure", v: "38%", band: "low", spark: "▄▅▅▆▆▇▆██"}, {k: "waiting on you", v: "4h 10m", band: "low", spark: "▆▇▇█▇███▇"}},
	"kalish":     {{k: "deploys/day", v: "0.1", band: "low", spark: "▁▁▁▂▁▁▁▁▁"}, {k: "lead time", v: "3d 8h", band: "low", spark: "▇▇███████"}, {k: "change failure", v: "—", band: "medium", spark: "▁▁▁▁▁▁▁▁▁"}, {k: "waiting on you", v: "9h 20m", band: "low", spark: "▇███▇████"}},
	"unassigned": {{k: "deploys/day", v: "—", band: "medium", spark: "▁▁▁▁▁▁▁▁▁"}, {k: "lead time", v: "—", band: "medium", spark: "▁▁▁▁▁▁▁▁▁"}, {k: "change failure", v: "—", band: "medium", spark: "▁▁▁▁▁▁▁▁▁"}, {k: "waiting on you", v: "—", band: "medium", spark: "▁▁▁▁▁▁▁▁▁"}},
}
