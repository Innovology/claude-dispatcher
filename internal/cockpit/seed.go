package cockpit

// seed.go holds the shared view-model types and the cross-lens seed data. Data
// used by a single lens lives in that lens's seed_*.go file; anything two or
// more lenses read lives here so there is exactly one owner.
//
// The data is the design's own representative content. Field names track the
// mock so the mapping stays legible.

// ---- shared view-model types ------------------------------------------------

type activity struct {
	tool, arg, result, resultColor string
}

// agent is the main session plus any subagents it spawned.
// state is one of: now | ok | bad | idle (see agentStyle in seed_floor.go).
type agent struct {
	branch, name, model, doing, state, meta string
}

type prRef struct {
	id, title, meta, color, forge string
}

type runRef struct {
	name, state, color, age string
}

// chainStep is one node in the commits→pr→checks→merge→deploy chain.
// state: ok | now | bad | idle (see chainStyle in seed_floor.go).
type chainStep struct {
	label, meta, state string
}

type action struct{ key, label string }

type askBlock struct {
	kicker, headline, evidence string
	actions                    []action
}

// dispatch is one in-flight unit of work on the floor. Mirrors the mock's d().
type dispatch struct {
	feature, repo, product, forge, state, age, branch string
	why, signal                                       string
	urgent                                            bool
	plus, minus, files, commits                       int
	prompt                                            string
	activity                                          []activity
	agents                                            []agent
	prs                                               []prRef
	runs                                              []runRef
	chain                                             []chainStep
	ask                                               *askBlock
	mode                                              string
}

// branchOf derives feature/<slug> the way the mock's d() does.
func branchOf(feature string) string {
	out := make([]rune, 0, len(feature))
	for _, r := range feature {
		if r == ' ' {
			r = '-'
		}
		out = append(out, r)
	}
	return "feature/" + string(out)
}

type product struct {
	name, repos, forge      string
	inflight, needs, review int
	live                    int
	spark, lead             string
}

type repoRef struct {
	name, forge string
	out         int
	ci, ciColor string
}

type staleRepo struct {
	repo, product string
	days          int
	note          string
}

type workingItem struct {
	feature, repo, product, age string
}

type productStat struct {
	dispatched7d, closed7d, rejected7d, budget int
	pace                                       float64
	note                                       string
}

type command struct{ name, hint string }

// ticket is a backlog item. Owned (as data) by the backlog lens via
// backlogTickets, but the type lives here because the floor lens also renders
// tickets under a selected group header.
type ticket struct {
	id, src, title, product, repo, pri, age, labels, taken, body, prompt string
}

// diffFile and hunkLine are the shared shape for the diff and review overlays.
type diffFile struct {
	path        string
	plus, minus int
}

type hunkLine struct{ sign, text string }

// ---- cross-lens seed data ---------------------------------------------------

var products = []product{
	{name: "cortiva", repos: "4 repos", forge: "github", inflight: 9, needs: 2, review: 3, live: 2, spark: "▂▃▅▇▆▃▅", lead: "4h 10m"},
	{name: "altsports", repos: "2 repos", forge: "github", inflight: 5, needs: 2, review: 1, live: 1, spark: "▁▂▂▃▁▂▃", lead: "6h 25m"},
	{name: "dispatch", repos: "1 repo", forge: "github", inflight: 4, needs: 1, review: 1, live: 1, spark: "▃▅▇▅▇▆▇", lead: "1h 55m"},
	{name: "northwind", repos: "2 repos", forge: "ado", inflight: 3, needs: 2, review: 1, live: 0, spark: "▂▁▁▂▁▁▁", lead: "2d 3h"},
	{name: "kalish", repos: "2 repos", forge: "github", inflight: 3, needs: 1, review: 0, live: 0, spark: "▁▁▁▂▁▁▁", lead: "3d 8h"},
	{name: "unassigned", repos: "46 repos", forge: "—", inflight: 0, needs: 0, review: 0, live: 0, spark: "▁▁▁▁▁▁▁", lead: "—"},
}

var reposByProduct = map[string][]repoRef{
	"cortiva": {
		{name: "cortiva-hq", forge: "gh", out: 3, ci: "✓ green", ciColor: cGreen},
		{name: "cortiva-api", forge: "gh", out: 3, ci: "● deploying", ciColor: cBlue},
		{name: "cortiva-mobile", forge: "gh", out: 2, ci: "✓ green", ciColor: cGreen},
		{name: "cortiva-infra", forge: "gh", out: 1, ci: "✓ green", ciColor: cGreen},
	},
	"altsports": {
		{name: "altsports_1", forge: "gh", out: 3, ci: "✓ green", ciColor: cGreen},
		{name: "altsports-web", forge: "gh", out: 2, ci: "● ci running", ciColor: cBlue},
	},
	"dispatch": {{name: "claude-dispatcher", forge: "gh", out: 4, ci: "✓ green", ciColor: cGreen}},
	"northwind": {
		{name: "nw-billing", forge: "ado", out: 2, ci: "✗ Release-Prod", ciColor: cRed},
		{name: "nw-portal", forge: "ado", out: 1, ci: "✓ green", ciColor: cGreen},
	},
	"kalish": {
		{name: "kalish-core", forge: "gh", out: 1, ci: "✓ green", ciColor: cGreen},
		{name: "kalish-ops", forge: "gh", out: 2, ci: "✓ green", ciColor: cGreen},
	},
	"unassigned": {},
}

// productOrder preserves the display order of products (Go maps are unordered).
var productOrder = []string{"cortiva", "altsports", "dispatch", "northwind", "kalish", "unassigned"}

var productNote = map[string]string{
	"cortiva":    "Three green PRs nobody has reviewed. Merge them and two features go live before lunch.",
	"altsports":  "Healthy, but one dispatcher has been idle 22 minutes with three red tests.",
	"dispatch":   "Your fastest loop — 1h 55m from dispatch to live.",
	"northwind":  "Azure DevOps. Release-Prod has failed twice today; nothing has gone live.",
	"kalish":     "Stalled. Nothing merged in three days despite three dispatchers working.",
	"unassigned": "Repos the scanner found with no product mapping in config.toml.",
}

var staleRepos = []staleRepo{
	{repo: "cortiva-legacy-web", product: "cortiva", days: 41, note: "last dispatch failed, never retried"},
	{repo: "kalish-scoring", product: "kalish", days: 34, note: "2 open prs, both stale"},
	{repo: "nw-reporting", product: "northwind", days: 28, note: "ado pipeline disabled"},
	{repo: "altsports-scraper", product: "altsports", days: 22, note: "no ci configured"},
	{repo: "kalish-mobile", product: "kalish", days: 19, note: "never dispatched to"},
	{repo: "cortiva-docs", product: "cortiva", days: 17, note: "docs drift behind #144"},
}

var working = []workingItem{
	{feature: "terraform split", repo: "cortiva-infra", age: "4h"},
	{feature: "office", repo: "cortiva-hq", age: "3h"},
	{feature: "core telemetry", repo: "kalish-core", age: "5h"},
	{feature: "sso handoff", repo: "nw-portal", age: "3h"},
	{feature: "push tokens", repo: "cortiva-mobile", age: "2h"},
	{feature: "billing retries", repo: "nw-billing", age: "2h"},
	{feature: "state management", repo: "claude-dispatcher", age: "1h"},
	{feature: "kalish", repo: "altsports_1", age: "1h"},
	{feature: "league table", repo: "altsports-web", age: "1h"},
	{feature: "rate limiter", repo: "cortiva-api", age: "48m"},
	{feature: "roster csv", repo: "altsports_1", age: "35m"},
	{feature: "oncall digest", repo: "kalish-ops", age: "25m"},
	{feature: "deploy detect", repo: "claude-dispatcher", age: "20m"},
}

var productStats = map[string]productStat{
	"cortiva":    {dispatched7d: 31, closed7d: 24, rejected7d: 3, budget: 31, pace: 1.4, note: "Three green PRs nobody has reviewed. Merge them and two features go live before lunch."},
	"altsports":  {dispatched7d: 14, closed7d: 9, rejected7d: 4, budget: 12, pace: 0.9, note: "One dispatcher idle 22m with three red tests, and a claims-done with no PR behind it."},
	"dispatch":   {dispatched7d: 19, closed7d: 17, rejected7d: 1, budget: 9, pace: 0.8, note: "Your fastest loop — 1h 55m from dispatch to live, and the lowest reject rate you have."},
	"northwind":  {dispatched7d: 8, closed7d: 2, rejected7d: 2, budget: 8, pace: 1.1, note: "Release-Prod has failed twice today. Nothing has reached production since Tuesday."},
	"kalish":     {dispatched7d: 6, closed7d: 1, rejected7d: 2, budget: 5, pace: 0.6, note: "Nothing merged in three days despite three dispatchers working. Two repos untouched for a month."},
	"unassigned": {dispatched7d: 0, closed7d: 0, rejected7d: 0, budget: 0, pace: 0, note: "Repos the scanner found with no product mapping in config.toml."},
}

// repoProduct maps a repo name back to its product (or "—").
func repoProduct(repo string) string {
	for _, p := range productOrder {
		for _, r := range reposByProduct[p] {
			if r.name == repo {
				return p
			}
		}
	}
	return "—"
}

// productByName returns the product record, or a zero value if not found.
func productByName(name string) product {
	for _, p := range products {
		if p.name == name {
			return p
		}
	}
	return product{name: name}
}

var commands = []command{
	{name: "backlog", hint: "12 open tickets · github, linear, azure boards"},
	{name: "usage", hint: "subscription budget by window, model and product"},
	{name: "velocity", hint: "dora + what actually shipped, tracked over time"},
	{name: "decisions", hint: "adrs and decision trees · 4 proposed by claude, unreviewed"},
	{name: "plugins", hint: "deciduous, adr-tools, structurizr · enable per repo"},
	{name: "dispatch", hint: "repo → feature → prompt, or paste a batch"},
	{name: "product cortiva", hint: "open the product — 4 repos, 9 in flight"},
	{name: "reply", hint: "answer the selected dispatcher without attaching"},
	{name: "attach", hint: "tmux attach at full fidelity · ctrl+\\ to come back"},
	{name: "merge", hint: "gh pr merge --squash --auto on the selected feature"},
	{name: "pipelines", hint: "runs across github actions + azure pipelines"},
	{name: "ship", hint: "mark shipped manually — done means live"},
	{name: "kill", hint: "end the tmux session and mark exited"},
	{name: "roots", hint: "edit the scan roots (57 repos found)"},
	{name: "settings", hint: "scan roots · Linear key · Azure org · weekly token budget"},
}

// ---- small shared metadata --------------------------------------------------

var sourceMeta = map[string]struct{ label, color string }{
	"lin": {"linear", "#9a8ce0"},
	"gh":  {"github", "#a3a3a3"},
	"ado": {"boards", "#b46cc9"},
}

var groupLabel = map[string]string{
	"band":    "by what it wants",
	"product": "by product",
	"repo":    "by repo",
	"forge":   "by forge",
}

var bandOrder = []string{"blocked", "claimed", "needs", "review"}

var bandRule = map[string]string{
	"blocked": "you are the dependency",
	"claimed": "claude says the goal is met · accept to close",
	"needs":   "a turn ended and nobody replied",
	"review":  "code exists, it is not live yet",
}

var models = []string{"opus", "sonnet", "haiku"}

type dispatchMode struct{ id, label, note string }

var modes = []dispatchMode{
	{"plan", "plan only", "proposes, writes nothing"},
	{"edits", "edits, asks to ship", "stops at merge and anything outside scope"},
	{"auto", "auto-accept edits", "writes freely, still asks to merge"},
	{"full", "ship unattended", "merges and deploys without asking"},
}

func modeByID(id string) dispatchMode {
	for _, m := range modes {
		if m.id == id {
			return m
		}
	}
	return modes[1]
}
