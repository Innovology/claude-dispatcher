package cockpit

// collect_products.go builds the product lens (lens 3) from real data: the
// portfolio roll-up (one product per config mapping plus a synthetic
// "unassigned"), the per-product repo grid, the review queue (from gh open PRs),
// the shipped history (done records grouped by ship day) and the per-product
// velocity tiles. Team stats have no cheap real source, so they are an honest
// empty state.
//
// Every field is filled non-nil so the lens shows a truthful empty state rather
// than stale seed data. gh is best-effort: when it is unavailable the review
// queue is left nil (seed kept) and CI badges fall back to "—".

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
)

func collectProducts(ctx *collectCtx, s *snapshot) {
	cfg := ctx.cfg
	ghUp := gh.Available()
	now := time.Now()
	weekAgo := now.Add(-7 * 24 * time.Hour)

	// Discovered repos, indexed by directory name for path/forge lookups.
	discByName := map[string]repos.Repo{}
	var unassignedRepos []repos.Repo
	for _, r := range ctx.repos {
		discByName[r.Name] = r
		if r.Product == "" {
			unassignedRepos = append(unassignedRepos, r)
		}
	}

	// Known product keys from config.
	known := map[string]bool{}
	for p := range cfg.Products {
		known[p] = true
	}

	// prodOf maps a record onto a product key, folding anything unmapped into
	// "unassigned".
	prodOf := func(rec *state.Dispatch) string {
		p := rec.Product
		if p == "" {
			p = cfg.ProductFor(rec.RepoName)
		}
		if p == "" || !known[p] {
			return "unassigned"
		}
		return p
	}

	recsByProduct := map[string][]*state.Dispatch{}
	openByRepo := map[string]int{}
	recBranch := map[string]bool{}
	recPR := map[string]bool{}
	for _, rec := range ctx.records {
		recsByProduct[prodOf(rec)] = append(recsByProduct[prodOf(rec)], rec)
		if rec.Status != state.StatusDone && rec.Status != state.StatusExited {
			openByRepo[rec.RepoName]++
		}
		if rec.Branch != "" {
			recBranch[rec.Branch] = true
		}
		if rec.PRNumber > 0 {
			recPR[rec.RepoName+"#"+itoa(rec.PRNumber)] = true
		}
	}

	// Stable product order: sorted config keys, then unassigned if it has any
	// repos or records.
	order := make([]string, 0, len(cfg.Products)+1)
	for p := range cfg.Products {
		order = append(order, p)
	}
	sort.Strings(order)
	if len(unassignedRepos) > 0 || len(recsByProduct["unassigned"]) > 0 {
		order = append(order, "unassigned")
	}

	// repoNamesFor lists a product's repo directory names (sorted, deterministic).
	repoNamesFor := func(p string) []string {
		if p == "unassigned" {
			names := make([]string, 0, len(unassignedRepos))
			for _, r := range unassignedRepos {
				names = append(names, r.Name)
			}
			return names // already sorted: ctx.repos is name-sorted
		}
		names := append([]string(nil), cfg.Products[p]...)
		sort.Strings(names)
		return names
	}

	// repoGH caches per-repo gh work: open PRs, their checks, and the derived
	// CI badge, so reviews and the repo grid share one fetch.
	type repoGHData struct {
		prs     []gh.OpenPR
		checks  map[int]gh.Checks
		ci      string
		ciColor string
	}
	// Open PRs for every repo come from one search rather than one list call
	// per repo; openPRs is empty when the search fails, which reads as "no
	// open PRs" only after gh has actually answered.
	openPRs, _ := gh.OpenPRs()

	ghCache := map[string]repoGHData{}
	repoGHFor := func(name string) repoGHData {
		if v, ok := ghCache[name]; ok {
			return v
		}
		v := repoGHData{ci: "—", ciColor: cDim, checks: map[int]gh.Checks{}}
		r, ok := discByName[name]
		if ghUp && ok && r.Path != "" {
			v.prs = openPRs[name]
			// Check runs are per PR and cannot be batched, so fetch them
			// concurrently and cap the fan-out: a repo with forty open PRs
			// must not cost forty serial round-trips on every refresh.
			for num, c := range prChecksFor(r.Path, v.prs, maxCheckedPRs) {
				v.checks[num] = c
			}
			anyFail, anyRun, anyPass := false, false, false
			for _, c := range v.checks {
				switch {
				case c.Failing > 0:
					anyFail = true
				case c.Running > 0:
					anyRun = true
				case c.Passed > 0:
					anyPass = true
				}
			}
			switch {
			case anyFail:
				v.ci, v.ciColor = "✗ failing", cRed
			case anyRun:
				v.ci, v.ciColor = "● deploying", cBlue
			case anyPass:
				v.ci, v.ciColor = "✓ green", cGreen
			}
		}
		ghCache[name] = v
		return v
	}

	// Allocate every field non-nil.
	s.products = make([]product, 0, len(order))
	s.productOrder = append([]string(nil), order...)
	s.reposByProduct = map[string][]repoRef{}
	s.productNote = map[string]string{}
	s.productStats = map[string]productStat{}
	s.shipped = map[string][]shippedDay{}
	s.productVelocity = map[string][]velTile{}
	s.team = map[string][]teamRow{}
	s.teamVerdict = map[string]string{}
	if ghUp {
		s.reviews = map[string][]reviewItem{}
	}

	for _, p := range order {
		recs := recsByProduct[p]
		names := repoNamesFor(p)

		// ---- counts + velocity series over this product's records ----------
		inflight, needs, review, live := 0, 0, 0, 0
		dispatched7d, closed7d, rejected7d := 0, 0, 0
		var leads []time.Duration
		var waits []time.Duration
		dailyLive := make([]int, 7) // index 6 == today
		type shipEntry struct {
			it shippedItem
			t  time.Time
		}
		shipGroups := map[time.Time][]shipEntry{}

		for _, rec := range recs {
			if rec.Status != state.StatusDone && rec.Status != state.StatusExited {
				inflight++
			}
			if rec.Status == state.StatusBlocked || rec.Status == state.StatusNeedsInput {
				needs++
				waits = append(waits, now.Sub(rec.UpdatedAt))
			}
			if rec.PRNumber > 0 && rec.PRState == "OPEN" {
				review++
			}
			if rec.CreatedAt.After(weekAgo) {
				dispatched7d++
			}
			if (rec.Status == state.StatusExited || rec.PRState == "CLOSED") && rec.UpdatedAt.After(weekAgo) {
				rejected7d++
			}

			la, ok := prodLiveAt(rec)
			if !ok || rec.Status != state.StatusDone {
				continue
			}
			if prodSameDay(la, now) {
				live++
			}
			if la.After(weekAgo) {
				closed7d++
				if d := int(prodDay(now).Sub(prodDay(la)) / (24 * time.Hour)); d >= 0 && d < 7 {
					dailyLive[6-d]++
				}
			}
			if d := la.Sub(rec.CreatedAt); d > 0 {
				leads = append(leads, d)
			}
			key := prodDay(la)
			pr := ""
			if rec.PRNumber > 0 {
				pr = "#" + itoa(rec.PRNumber)
			}
			shipGroups[key] = append(shipGroups[key], shipEntry{
				it: shippedItem{
					feature:  rec.Feature,
					repo:     rec.RepoName,
					pr:       pr,
					at:       prodAge(la) + " ago",
					session:  rec.SessionID,
					closedBy: rec.StatusReason,
					prompt:   rec.Prompt,
				},
				t: la,
			})
		}

		// ---- product roll-up row -------------------------------------------
		leadStr := "—"
		if med, ok := prodMedian(leads); ok {
			leadStr = prodDur(med)
		}
		s.products = append(s.products, product{
			name:     p,
			repos:    prodRepoCount(len(names)),
			forge:    prodForge(names, discByName, ctx),
			inflight: inflight,
			needs:    needs,
			review:   review,
			live:     live,
			spark:    prodSpark(dailyLive),
			lead:     leadStr,
		})

		// ---- note + stats --------------------------------------------------
		note := prodNote(inflight, needs, review, live)
		s.productNote[p] = note
		s.productStats[p] = productStat{
			dispatched7d: dispatched7d,
			closed7d:     closed7d,
			rejected7d:   rejected7d,
			note:         note,
		}

		// ---- repo grid -----------------------------------------------------
		grid := make([]repoRef, 0, len(names))
		for _, name := range names {
			forge := "gh"
			if r, ok := discByName[name]; ok {
				forge = ctx.forge(r.Path)
			}
			g := repoGHFor(name)
			grid = append(grid, repoRef{
				name:    name,
				forge:   forge,
				out:     openByRepo[name],
				ci:      g.ci,
				ciColor: g.ciColor,
			})
		}
		s.reposByProduct[p] = grid

		// ---- reviews (gh open PRs across the product's repos) --------------
		if ghUp {
			items := []reviewItem{}
			for _, name := range names {
				g := repoGHFor(name)
				for _, pr := range g.prs {
					mine := strings.HasPrefix(pr.HeadRefName, "feature/") &&
						(recBranch[pr.HeadRefName] || recPR[name+"#"+itoa(pr.Number)])
					items = append(items, reviewItem{
						pr:      "#" + itoa(pr.Number),
						title:   pr.Title,
						repo:    name,
						author:  pr.Author,
						waiting: prodWaiting(pr.ReviewDecision, mine),
						age:     prodAge(pr.CreatedAt),
						checks:  prodChecks(g.checks[pr.Number]),
						size:    prodSize(pr.Additions, pr.Deletions),
						mine:    mine,
					})
				}
			}
			s.reviews[p] = items
		}

		// ---- shipped history -----------------------------------------------
		var days []time.Time
		for k := range shipGroups {
			days = append(days, k)
		}
		sort.Slice(days, func(i, j int) bool { return days[i].After(days[j]) })
		shippedDays := []shippedDay{}
		for _, k := range days {
			entries := shipGroups[k]
			sort.Slice(entries, func(i, j int) bool { return entries[i].t.After(entries[j].t) })
			items := make([]shippedItem, 0, len(entries))
			for _, e := range entries {
				items = append(items, e.it)
			}
			shippedDays = append(shippedDays, shippedDay{day: prodDayLabel(k, now), items: items})
		}
		s.shipped[p] = shippedDays

		// ---- velocity tiles ------------------------------------------------
		deploys7 := 0
		for _, n := range dailyLive {
			deploys7 += n
		}
		perDay := float64(deploys7) / 7
		leadTile := velTile{k: "lead time", v: "—", band: "medium"}
		if med, ok := prodMedian(leads); ok {
			leadTile.v, leadTile.band = prodDur(med), prodLeadBand(med)
		}
		waitTile := velTile{k: "waiting on you", v: "—", band: "medium"}
		if med, ok := prodMedian(waits); ok {
			waitTile.v, waitTile.band = prodDur(med), prodWaitBand(med)
		}
		s.productVelocity[p] = []velTile{
			{k: "deploys/day", v: fmt.Sprintf("%.1f", perDay), band: prodDeployBand(perDay), spark: prodSpark(dailyLive)},
			leadTile,
			waitTile,
		}

		// ---- team (honest empty state) -------------------------------------
		s.team[p] = []teamRow{}
		s.teamVerdict[p] = ""
	}
}

// ---- helpers (prod-prefixed to avoid collisions) ---------------------------

// prodLiveAt returns when a record went live: its deploy time, else the PR
// merge time.
func prodLiveAt(rec *state.Dispatch) (time.Time, bool) {
	if rec.DeployedAt != nil {
		return *rec.DeployedAt, true
	}
	if rec.PRMergedAt != nil {
		return *rec.PRMergedAt, true
	}
	return time.Time{}, false
}

// prodDay truncates t to local midnight.
func prodDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func prodSameDay(a, b time.Time) bool { return prodDay(a).Equal(prodDay(b)) }

// prodDayLabel names a ship day: "today", "yesterday", else "mon 3 aug".
func prodDayLabel(day, now time.Time) string {
	switch {
	case prodDay(day).Equal(prodDay(now)):
		return "today"
	case prodDay(day).Equal(prodDay(now).AddDate(0, 0, -1)):
		return "yesterday"
	default:
		return strings.ToLower(day.Format("Mon 2 Jan"))
	}
}

// prodAge renders a short age like "4m", "2h", "3d".
func prodAge(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h"
	default:
		return itoa(int(d/(24*time.Hour))) + "d"
	}
}

// prodDur renders a duration like "48m", "4h 10m", "2d 3h".
func prodDur(d time.Duration) string {
	if d < time.Minute {
		return "0m"
	}
	days := d / (24 * time.Hour)
	hours := (d % (24 * time.Hour)) / time.Hour
	mins := (d % time.Hour) / time.Minute
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// prodMedian returns the median of a duration set.
func prodMedian(ds []time.Duration) (time.Duration, bool) {
	if len(ds) == 0 {
		return 0, false
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2], true
}

var prodBars = []rune("▁▂▃▄▅▆▇█")

// prodSpark renders a small bar sparkline from a count series.
func prodSpark(counts []int) string {
	if len(counts) == 0 {
		return ""
	}
	max := 0
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	var b strings.Builder
	for _, c := range counts {
		idx := 0
		if max > 0 {
			idx = c * (len(prodBars) - 1) / max
		}
		b.WriteRune(prodBars[idx])
	}
	return b.String()
}

// prodRepoCount renders "1 repo" / "N repos".
func prodRepoCount(n int) string {
	if n == 1 {
		return "1 repo"
	}
	return itoa(n) + " repos"
}

// prodForge is the majority forge of a product's discovered repos, rendered as
// "github"/"ado", or "—" when none are discovered.
func prodForge(names []string, disc map[string]repos.Repo, ctx *collectCtx) string {
	nGH, nADO := 0, 0
	for _, name := range names {
		r, ok := disc[name]
		if !ok || r.Path == "" {
			continue
		}
		if ctx.forge(r.Path) == "ado" {
			nADO++
		} else {
			nGH++
		}
	}
	if nGH == 0 && nADO == 0 {
		return "—"
	}
	if nADO > nGH {
		return "ado"
	}
	return "github"
}

// prodNote is a short derived sentence, or "".
func prodNote(inflight, needs, review, live int) string {
	var parts []string
	if inflight > 0 {
		parts = append(parts, itoa(inflight)+" in flight")
	}
	if needs > 0 {
		parts = append(parts, itoa(needs)+" want you")
	}
	if review > 0 {
		parts = append(parts, itoa(review)+" in review")
	}
	if live > 0 {
		parts = append(parts, itoa(live)+" live today")
	}
	return strings.Join(parts, " · ")
}

// prodWaiting names who a PR is waiting on.
func prodWaiting(decision string, mine bool) string {
	switch decision {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes"
	case "REVIEW_REQUIRED":
		if mine {
			return "you"
		}
		return "review"
	default:
		if mine {
			return "you"
		}
		return ""
	}
}

// prodChecks renders a CI checks summary like "✓ 4/4", "● 2/5", "✗ 1/4".
func prodChecks(c gh.Checks) string {
	if c.Total == 0 {
		return "—"
	}
	switch {
	case c.Failing > 0:
		return fmt.Sprintf("✗ %d/%d", c.Failing, c.Total)
	case c.Running > 0:
		return fmt.Sprintf("● %d/%d", c.Passed, c.Total)
	default:
		return fmt.Sprintf("✓ %d/%d", c.Passed, c.Total)
	}
}

// prodSize renders a diff size like "+268 −41" (unicode minus).
func prodSize(add, del int) string {
	return fmt.Sprintf("+%d −%d", add, del)
}

func prodDeployBand(v float64) string {
	switch {
	case v >= 3:
		return "elite"
	case v >= 1:
		return "high"
	case v >= 0.3:
		return "medium"
	default:
		return "low"
	}
}

func prodLeadBand(d time.Duration) string {
	switch {
	case d < 2*time.Hour:
		return "elite"
	case d < 8*time.Hour:
		return "high"
	case d < 24*time.Hour:
		return "medium"
	default:
		return "low"
	}
}

func prodWaitBand(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "elite"
	case d < 4*time.Hour:
		return "high"
	case d < 12*time.Hour:
		return "medium"
	default:
		return "low"
	}
}
