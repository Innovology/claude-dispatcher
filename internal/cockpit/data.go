package cockpit

// data.go declares every data var the lenses render. They start empty and are
// only ever filled by applySnapshot (live.go) from the collectors, so nothing
// reaches the screen that did not come from the user's own repos, dispatch
// records and forges.
//
// This file deliberately contains no values. The cockpit previously shipped the
// design's mock portfolio here as the initial state, which meant a fabricated
// factory — features, PR numbers, DORA figures — was indistinguishable from
// real data until the first snapshot landed, and stayed on screen for good if a
// collector failed. An empty var renders an honest empty state instead; see
// dataMode and the loading/empty copy in each lens.

// ---- floor (triage) ---------------------------------------------------------

var (
	dispatches []dispatch
	saidBy     = map[string]string{}
	tailLines  = map[string][]string{}
	diffsBy    = map[string]struct {
		files []diffFile
		hunk  []hunkLine
	}{}
)

// ---- command queue (the triage lens's own view model) -----------------------
//
// cqLastOutput is a bare age ("6s"), or "" when no running session has a
// transcript we could read — the view drops the clause rather than printing a
// zero, so an unreadable transcript never reads as "silent for 0s".

var (
	cqItems      []cqItem
	cqWorking    []cqGroup
	cqLastOutput string
)

// ---- products ---------------------------------------------------------------

var (
	products       []product
	reposByProduct = map[string][]repoRef{}
	productOrder   []string
	productNote    = map[string]string{}
	staleRepos     []staleRepo
	working        []workingItem
	productStats   = map[string]productStat{}
	// repoInventory is every discovered repo, product or not. The assign
	// overlay writes config.toml and then updates it in place, so the flow
	// stays responsive while the next full snapshot is still loading.
	repoInventory []repoRow
)

// ---- product detail ---------------------------------------------------------

var (
	reviews         = map[string][]reviewItem{}
	team            = map[string][]teamRow{}
	teamVerdict     = map[string]string{}
	shipped         = map[string][]shippedDay{}
	productVelocity = map[string][]velTile{}
)

// diffsByPR and findings back the product lens's review overlay. No collector
// fills them yet, so the overlay says so rather than inventing a hunk and a
// reviewer's verdict — which is what it used to do.
var (
	diffsByPR = map[string][]hunkLine{}
	findings  = map[string][]struct{ sev, text, color string }{}
)

// ---- backlog / queue --------------------------------------------------------

var (
	backlogTickets []ticket
	queueItems     []queueItem
)

// ---- decisions --------------------------------------------------------------

var (
	decisions         = map[string][]decision{}
	decisionRepoOrder []string
	plugins           []plugin
)

// ---- usage ------------------------------------------------------------------

var (
	usageWindows    []usageWindow
	usageModels     []usageModel
	usageProjection string
	usageAdvice     []usageAdviceItem
)

// ---- velocity ---------------------------------------------------------------

var (
	doraOrg     []doraMetric
	doraFactory []doraMetric
	doraSplit   []splitPart
	doraWeeks   []doraWeek
	outputWeeks []outWeek
	notVelocity []notVelocityRow
)
