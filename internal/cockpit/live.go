package cockpit

// live.go is the real-data layer. The lenses render package-level data vars
// (dispatches, products, reviews, backlogTickets, …) that start as the design's
// demo seed. The refresh loop (refresh.go) rebuilds a snapshot from the real
// backends — dispatch records, git, gh, Linear, Azure, transcripts — and
// applySnapshot swaps it into those vars on the UI goroutine, so no lens needs
// to change. A feature→record map rides along for the real actions (actions.go).
//
// Each collector (collect_*.go) fills its slice of the shared snapshot. A
// collector that cannot reach its source leaves its field nil; applySnapshot
// treats nil as "no fresh data — keep what's showing", and an empty-but-non-nil
// slice as "collected, genuinely nothing", so a lens shows an honest empty state.

import (
	"os/exec"
	"strings"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// snapshot is a full rebuild of every reassignable data var, plus the record
// map the actions need. Field types mirror the package vars exactly.
type snapshot struct {
	dispatches []dispatch
	saidBy     map[string]string
	tailLines  map[string][]string
	diffsBy    map[string]struct {
		files []diffFile
		hunk  []hunkLine
	}

	fleet        []fleetRow
	cqLastOutput string

	products       []product
	reposByProduct map[string][]repoRef
	productOrder   []string
	productNote    map[string]string
	staleRepos     []staleRepo
	working        []workingItem
	productStats   map[string]productStat

	backlogTickets []ticket

	// discovered is the scanned repo list, carried so the view layer can reach a
	// repo's path without re-walking the disk.
	discovered []repos.Repo

	reviews         map[string][]reviewItem
	team            map[string][]teamRow
	teamVerdict     map[string]string
	shipped         map[string][]shippedDay
	productVelocity map[string][]velTile

	decisions         map[string][]decision
	decisionRepoOrder []string
	plugins           []plugin

	usageWindows    []usageWindow
	usageModels     []usageModel
	usageProjection string
	usageAdvice     []usageAdviceItem

	doraOrg     []doraMetric
	doraFactory []doraMetric
	doraSplit   []splitPart
	doraWeeks   []doraWeek
	outputWeeks []outWeek
	outputHead  string
	outputUnit  string
	outputDelta string
	outputSpark string
	notVelocity []notVelocityRow

	// records maps a view dispatch's feature to its live record, so an action
	// on the selected row reaches the real tmux session / branch / PR.
	records map[string]*state.Dispatch
	// dataMode is "live" once a real load has run, "" while still on seed.
	dataMode string
}

// collectCtx is the shared input every collector reads from.
type collectCtx struct {
	cfg     *config.Config
	records []*state.Dispatch
	repos   []repos.Repo
}

// productFor maps a record onto the same product key collectProducts uses,
// folding anything unmapped into "unassigned".
//
// It reads the config rather than the package's productOrder/reposByProduct
// vars on purpose. Collectors run in order and applySnapshot only publishes
// their results at the end, so on the first load those vars are still empty:
// every dispatch resolved to "—", matched no product group, and the floor came
// up empty while work was in flight. A collector must derive from its ctx, not
// from the state a later collector will produce.
func (c *collectCtx) productFor(rec *state.Dispatch) string {
	p := rec.Product
	if p == "" && c.cfg != nil {
		p = c.cfg.ProductFor(rec.RepoName)
	}
	if p == "" {
		return "unassigned"
	}
	if c.cfg != nil {
		if _, ok := c.cfg.Products[p]; !ok {
			return "unassigned"
		}
	}
	return p
}

// forge reports the forge a repo path belongs to: "ado" for Azure DevOps
// remotes, else "gh". Detection is by the origin remote URL.
func (c *collectCtx) forge(repoPath string) string {
	out, err := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin").Output()
	if err != nil {
		return "gh"
	}
	url := strings.ToLower(strings.TrimSpace(string(out)))
	if strings.Contains(url, "dev.azure.com") || strings.Contains(url, "visualstudio.com") {
		return "ado"
	}
	return "gh"
}

// loadSnapshot assembles a full snapshot from the real backends. Each collector
// fills its own fields; collectors live in collect_*.go.
func loadSnapshot(cfg *config.Config) snapshot { return loadSnapshotReporting(cfg, nil) }

// loadSnapshotReporting is loadSnapshot with the opening screen watching. Every
// stage announces what it is about to do and then what it found, so the boot
// sequence is a description of this function rather than a decoration over it.
// A nil report is the ordinary refresh: no screen, no reporting, same work.
func loadSnapshotReporting(cfg *config.Config, r bootReport) snapshot {
	r.begin(bootSupervisor, "looking for "+supervisor.Backend()+"…")
	if supervisor.Available() {
		r.done(bootSupervisor, supervisor.Backend(), false)
	} else {
		// Without the supervisor there is nothing to attach to or kill; the
		// cockpit still opens, so this is a warning and not an exit.
		r.done(bootSupervisor, supervisor.Backend()+" not found", true)
	}

	r.begin(bootRecords, "reading "+state.DispatchesDir()+"…")
	records := state.LoadAll()
	r.done(bootRecords, countOf(len(records), "dispatch", "dispatches"), false)

	r.begin(bootSessions, "asking "+supervisor.Backend()+" what is still running…")
	live := liveSessions(records)
	r.done(bootSessions, countOf(live, "session", "sessions")+" live of "+itoa(len(records)), false)

	roots := cfg.ExpandedRoots()
	r.begin(bootRepos, "scanning "+countOf(len(roots), "root", "roots")+"…")
	found := repos.Discover(cfg)
	// No repos with roots configured means the roots point somewhere without
	// any: an empty cockpit whose cause is worth naming here rather than
	// leaving the human to infer it from six empty lenses.
	r.done(bootRepos, countOf(len(found), "repo", "repos"), len(found) == 0)

	r.begin(bootForge, "checking the github cli…")
	if gh.Available() {
		r.done(bootForge, "github cli", false)
	} else {
		r.done(bootForge, "gh not found — no pr or check signals", true)
	}

	ctx := &collectCtx{cfg: cfg, records: records, repos: found}
	var s snapshot
	s.dataMode = "live"
	s.discovered = ctx.repos

	r.begin(bootDispatchers, "reading transcripts, diffs and prs…")
	collectFloor(ctx, &s)
	// collectFleet must follow collectFloor: it reads the forge, diff and
	// transcript work that load already did rather than paying for it twice.
	collectFleet(ctx, &s)
	r.done(bootDispatchers, countOf(len(s.dispatches), "dispatcher", "dispatchers")+" in flight", false)

	r.begin(bootProducts, "grouping repos by product…")
	collectProducts(ctx, &s)
	r.done(bootProducts, countOf(namedProducts(s.products), "product", "products"), false)

	r.begin(bootBacklog, "fetching assigned issues…")
	collectBacklog(ctx, &s)
	r.done(bootBacklog, countOf(len(s.backlogTickets), "ticket", "tickets"), false)

	r.begin(bootDecisions, "scanning repos for adrs and decision logs…")
	collectDecisions(ctx, &s)
	r.done(bootDecisions, countOf(countDecisions(s.decisions), "record", "records")+
		" in "+countOf(len(s.decisionRepoOrder), "repo", "repos"), false)

	r.begin(bootUsage, "totalling this week's tokens…")
	collectUsage(ctx, &s)
	r.done(bootUsage, countOf(len(s.usageModels), "model", "models")+" this week", false)

	r.begin(bootVelocity, "measuring what reached production…")
	collectVelocity(ctx, &s)
	// Deploys, not len(doraWeeks): the week series is a fixed six-week window
	// the collector always emits, so its length would report "6 weeks of
	// history" on a portfolio that has never shipped anything.
	r.done(bootVelocity, countOf(countDeploys(s.doraWeeks), "deploy", "deploys")+" in 6 weeks", false)

	r.begin(bootStale, "looking for repos nothing is working on…")
	collectStale(ctx, &s)
	r.done(bootStale, countOf(len(s.staleRepos), "repo", "repos")+" gone quiet", false)

	return s
}

// liveSessions counts the recorded dispatches whose supervisor session is still
// running, from one listing rather than a probe per record.
//
// It is read for the opening screen alone. The cockpit's own notion of what is
// live comes from the records the lifecycle hook writes, and replacing that
// with a session probe would be a different change with different consequences
// — a session can outlive the work in it, and the work can outlive a session
// the human killed by hand.
func liveSessions(records []*state.Dispatch) int {
	alive := map[string]bool{}
	for _, name := range supervisor.Sessions() {
		alive[name] = true
	}
	n := 0
	for _, rec := range records {
		if rec.TmuxSession != "" && alive[rec.TmuxSession] {
			n++
		}
	}
	return n
}

// namedProducts counts real products, excluding the bucket every unmapped repo
// is folded into — the same distinction the header's portfolio line makes.
func namedProducts(ps []product) int {
	n := 0
	for _, p := range ps {
		if p.name != clUnassigned {
			n++
		}
	}
	return n
}

// countDecisions totals the records found across every repo that has any.
func countDecisions(byRepo map[string][]decision) int {
	n := 0
	for _, ds := range byRepo {
		n += len(ds)
	}
	return n
}

// countDeploys totals what reached production across the measured window.
func countDeploys(weeks []doraWeek) int {
	n := 0
	for _, w := range weeks {
		n += w.deploys
	}
	return n
}

// countOf renders "0 repos" / "1 repo" / "57 repos" — the boot sequence's only
// figure format, so a zero reads as a counted zero and not as a blank.
func countOf(n int, one, many string) string {
	return itoa(n) + " " + plural(n, one, many)
}

// applySnapshot swaps freshly collected data into the package vars. It runs on
// the UI goroutine (from Update), so the reassignments race with nothing. A nil
// field means the collector had no fresh data, so the current value is kept.
func applySnapshot(s snapshot) {
	if s.dispatches != nil {
		dispatches = s.dispatches
	}
	if s.saidBy != nil {
		saidBy = s.saidBy
	}
	if s.tailLines != nil {
		tailLines = s.tailLines
	}
	if s.diffsBy != nil {
		diffsBy = s.diffsBy
	}
	if s.fleet != nil {
		fleet = s.fleet
	}
	// Unconditionally, unlike every other field: "" means no running session has
	// a readable transcript, which is an observation the view must show, and a
	// stale age left in place would be a lie about liveness.
	cqLastOutput = s.cqLastOutput
	if s.products != nil {
		products = s.products
	}
	if s.reposByProduct != nil {
		reposByProduct = s.reposByProduct
	}
	if s.productOrder != nil {
		productOrder = s.productOrder
	}
	if s.productNote != nil {
		productNote = s.productNote
	}
	if s.staleRepos != nil {
		staleRepos = s.staleRepos
	}
	if s.working != nil {
		working = s.working
	}
	if s.productStats != nil {
		productStats = s.productStats
	}
	if s.backlogTickets != nil {
		backlogTickets = s.backlogTickets
	}
	if s.discovered != nil {
		lastDiscovered = s.discovered
	}
	if s.reviews != nil {
		reviews = s.reviews
	}
	if s.team != nil {
		team = s.team
	}
	if s.teamVerdict != nil {
		teamVerdict = s.teamVerdict
	}
	if s.shipped != nil {
		shipped = s.shipped
	}
	if s.productVelocity != nil {
		productVelocity = s.productVelocity
	}
	if s.decisions != nil {
		decisions = s.decisions
	}
	if s.decisionRepoOrder != nil {
		decisionRepoOrder = s.decisionRepoOrder
	}
	if s.plugins != nil {
		plugins = s.plugins
	}
	if s.usageWindows != nil {
		usageWindows = s.usageWindows
	}
	if s.usageModels != nil {
		usageModels = s.usageModels
	}
	if s.usageProjection != "" {
		usageProjection = s.usageProjection
	}
	if s.usageAdvice != nil {
		usageAdvice = s.usageAdvice
	}
	if s.doraOrg != nil {
		doraOrg = s.doraOrg
	}
	if s.doraFactory != nil {
		doraFactory = s.doraFactory
	}
	if s.doraSplit != nil {
		doraSplit = s.doraSplit
	}
	if s.doraWeeks != nil {
		doraWeeks = s.doraWeeks
	}
	if s.outputWeeks != nil {
		outputWeeks = s.outputWeeks
	}
	if s.outputHead != "" {
		outputHeadline = s.outputHead
	}
	if s.outputUnit != "" {
		outputUnit = s.outputUnit
	}
	if s.outputDelta != "" {
		outputDelta = s.outputDelta
	}
	if s.outputSpark != "" {
		outputSpark = s.outputSpark
	}
	if s.notVelocity != nil {
		notVelocity = s.notVelocity
	}
	if s.records != nil {
		liveRecords = s.records
	}
}

// liveRecords maps the selected view dispatch's feature to its real record. It
// is empty until the first live load; actions fall back gracefully when a
// feature has no record (e.g. while still showing seed data).
var liveRecords = map[string]*state.Dispatch{}

// recordFor returns the live record backing feature, or nil.
func recordFor(feature string) *state.Dispatch { return liveRecords[feature] }
