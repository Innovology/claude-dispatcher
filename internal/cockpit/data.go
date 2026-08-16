package cockpit

import (
	"time"

	"claude-dispatcher/internal/repos"
)

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

// ---- the fleet (the triage lens's own view model) ----------------------------
//
// fleet is every dispatcher in flight, ranked: the ones that want you first.
//
// cqLastOutput is when a running session last wrote anything, or the zero time
// when none of them has a transcript we could read — the view drops the clause
// rather than printing a zero, so an unreadable transcript never reads as
// "silent for 0s".
//
// It is the instant and not the rendered age ("6s") it used to be. A string
// formatted in the collector is only as fresh as the snapshot that carried it,
// so it stood still between polls while the clock did not — the same standing
// still the AGE column had, in a line that claims to report liveness. The view
// formats it, once a second, from the clock (see ageEvery).

var (
	fleet        []fleetRow
	cqLastOutput time.Time
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
)

// ---- product detail ---------------------------------------------------------

var (
	reviews         = map[string][]reviewItem{}
	team            = map[string][]teamRow{}
	teamVerdict     = map[string]string{}
	shipped         = map[string][]shippedDay{}
	productHistory  = map[string][]historyItem{}
	historyOlder    = map[string]int{}
	productVelocity = map[string][]velTile{}
)

// diffsByPR and findings back the product lens's review overlay. No collector
// fills them yet, so the overlay says so rather than inventing a hunk and a
// reviewer's verdict — which is what it used to do.
var (
	diffsByPR = map[string][]hunkLine{}
	findings  = map[string][]struct{ sev, text, color string }{}
)

// ---- backlog ----------------------------------------------------------------

// lastDiscovered is the repo list from the most recent scan.
var lastDiscovered []repos.Repo

var backlogTickets []ticket

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
