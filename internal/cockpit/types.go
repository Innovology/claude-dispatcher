package cockpit

// seed.go holds the shared view-model types and the cross-lens seed data. Data
// used by a single lens lives in that lens's seed_*.go file; anything two or
// more lenses read lives here so there is exactly one owner.
//
// The data is the design's own representative content. Field names track the
// mock so the mapping stays legible.

import "time"

// ---- shared view-model types ------------------------------------------------

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
	agents                                            []agent
	prs                                               []prRef
	runs                                              []runRef
	chain                                             []chainStep
	ask                                               *askBlock
}

type product struct {
	name, repos, forge      string
	inflight, needs, review int
	live                    int
	spark, lead             string
	// leadDur is what lead formats. The velocity lens ranks products by lead
	// time, and ranking on the formatted string silently puts "48m" above
	// "2d 4h"; the duration is the sortable truth, the string is for the eye.
	leadDur time.Duration
	// commits7d and merged7d are what the repositories say, not what the
	// dispatcher started: commits on each repo's branch and pull requests merged
	// in the last week, whoever did them. A product the human works in directly
	// has no dispatch records and would otherwise read as idle.
	commits7d int
	merged7d  int
	// deploys7d counts successful runs of each repo's deploy workflow — the
	// deployments that actually happened.
	deploys7d int
}

type repoRef struct {
	name, forge string
	out         int
	ci, ciColor string
	// last is how long since the repo's last commit ("3d", "—" when git could
	// not say). The assignment editor shows it so a repo nobody has touched in
	// months is obvious while you are deciding where it belongs.
	last string
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

// commands is the : palette. Hints describe what a command does — they never
// quote a count, because a static count is a claim about the user's portfolio
// that nothing keeps true.
var commands = []command{
	{name: "backlog", hint: "open tickets · github, linear, azure boards"},
	{name: "usage", hint: "subscription budget by window, model and product"},
	{name: "velocity", hint: "dora + what actually shipped, tracked over time"},
	{name: "decisions", hint: "adrs and decision trees found in your repos"},
	{name: "plugins", hint: "deciduous, adr-tools, structurizr · enable per repo"},
	{name: "dispatch", hint: "repo → feature → prompt, or paste a batch"},
	{name: "new dispatch", hint: "open the repo → feature → prompt form"},
	{name: "product", hint: "open the product under the cursor"},
	{name: "reply", hint: "answer the selected dispatcher without attaching"},
	{name: "attach", hint: "tmux attach at full fidelity · ctrl+\\ to come back"},
	{name: "merge", hint: "gh pr merge --squash --auto on the selected feature"},
	{name: "pipelines", hint: "runs across github actions + azure pipelines"},
	{name: "ship", hint: "mark shipped manually — done means live"},
	{name: "kill", hint: "end the tmux session and mark exited"},
	{name: "roots", hint: "edit the directories scanned for repos"},
	{name: "settings", hint: "scan roots · Linear key · Azure org · weekly token budget"},
}

// ---- small shared metadata --------------------------------------------------

var sourceMeta = map[string]struct{ label, color string }{
	"lin": {"linear", cViolet},
	"gh":  {"github", cMid},
	"ado": {"boards", cBoards},
}
