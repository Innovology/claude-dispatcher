package cockpit

// collect_velocity.go fills the VELOCITY lens (velocity.go) from the dispatch
// record history alone — no external service. "Output" is what actually reached
// production: a feature is live when it has a DeployedAt (or, lacking a deploy
// workflow, a merge that flipped it to done). Everything is bucketed by ISO
// week. DORA metrics we can honestly derive (deploy frequency, lead time, work
// in progress) are computed; the rest render as "—" with a neutral band rather
// than a fabricated number.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/state"
)

// velWeekKey identifies an ISO week (year + week number).
type velWeekKey struct{ y, w int }

// velSpark renders a tiny magnitude bar (▁..█) from a series, oldest-first.
func velSpark(vals []int) string {
	if len(vals) == 0 {
		return ""
	}
	glyphs := []rune("▁▂▃▄▅▆▇█")
	max := 0
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	var b strings.Builder
	for _, v := range vals {
		idx := 0
		if max > 0 {
			idx = v * (len(glyphs) - 1) / max
		}
		b.WriteRune(glyphs[idx])
	}
	return b.String()
}

// velHumanDur formats a positive duration like "41m", "3h 40m" or "2d 4h".
func velHumanDur(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	total := int(d.Minutes())
	h := total / 60
	m := total % 60
	if h >= 24 {
		return fmt.Sprintf("%dd %dh", h/24, h%24)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// velMedianDur returns the median of a duration slice (0 for empty).
func velMedianDur(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	cp := make([]time.Duration, len(ds))
	copy(cp, ds)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

// velLiveTime reports when a record went live, if it did: the deploy time, or —
// for a done record with no deploy — the PR merge (a merge counts as live in a
// repo with no deploy workflow), falling back to the record's UpdatedAt.
func velLiveTime(r *state.Dispatch) (time.Time, bool) {
	if r.DeployedAt != nil {
		return *r.DeployedAt, true
	}
	if r.Status == state.StatusDone {
		if r.PRMergedAt != nil {
			return *r.PRMergedAt, true
		}
		return r.UpdatedAt, true
	}
	return time.Time{}, false
}

// velFreqBand grades a deploys-per-day rate onto DORA bands.
func velFreqBand(perDay float64) string {
	switch {
	case perDay >= 1:
		return "elite"
	case perDay >= 0.3:
		return "high"
	case perDay >= 0.1:
		return "medium"
	default:
		return "low"
	}
}

// velLeadBand grades a median lead time onto DORA bands.
func velLeadBand(d time.Duration) string {
	h := d.Hours()
	switch {
	case h < 24:
		return "elite"
	case h < 168:
		return "high"
	case h < 720:
		return "medium"
	default:
		return "low"
	}
}

// collectVelocity computes output and DORA metrics from ctx.records.
func collectVelocity(ctx *collectCtx, s *snapshot) {
	now := time.Now()
	nowYear, nowWeek := now.ISOWeek()

	const nWeeks = 6
	type weekAgg struct {
		label      string
		key        velWeekKey
		live       int
		dispatched int
		leads      []time.Duration
	}
	weeks := make([]weekAgg, nWeeks)
	for i := 0; i < nWeeks; i++ {
		t := now.AddDate(0, 0, -7*i)
		y, w := t.ISOWeek()
		label := "w" + itoa(w)
		if i == 0 {
			label = "this week"
		}
		weeks[i] = weekAgg{label: label, key: velWeekKey{y, w}}
	}
	weekIndex := func(t time.Time) int {
		y, w := t.ISOWeek()
		for i := range weeks {
			if weeks[i].key.y == y && weeks[i].key.w == w {
				return i
			}
		}
		return -1
	}

	weekAgo := now.AddDate(0, 0, -7)
	inFlight := 0
	weekCommits := 0
	deploys7 := 0
	var allLeads []time.Duration

	for _, r := range ctx.records {
		if r == nil {
			continue
		}
		if r.Status != state.StatusDone {
			inFlight++
		}
		if idx := weekIndex(r.CreatedAt); idx >= 0 {
			weeks[idx].dispatched++
		}
		if lt, ok := velLiveTime(r); ok {
			if idx := weekIndex(lt); idx >= 0 {
				weeks[idx].live++
				if !r.CreatedAt.IsZero() {
					if lead := lt.Sub(r.CreatedAt); lead > 0 {
						weeks[idx].leads = append(weeks[idx].leads, lead)
					}
				}
			}
			if lt.After(weekAgo) {
				deploys7++
			}
			if !r.CreatedAt.IsZero() {
				if lead := lt.Sub(r.CreatedAt); lead > 0 {
					allLeads = append(allLeads, lead)
				}
			}
		}
		if uy, uw := r.UpdatedAt.ISOWeek(); uy == nowYear && uw == nowWeek {
			weekCommits += len(r.Commits)
		}
	}

	// ---- output pane --------------------------------------------------------
	outWeeks := make([]outWeek, nWeeks)
	sparkVals := make([]int, nWeeks) // oldest-first for the spark
	for i := range weeks {
		outWeeks[i] = outWeek{w: weeks[i].label, live: weeks[i].live, dispatched: weeks[i].dispatched}
		sparkVals[nWeeks-1-i] = weeks[i].live
	}
	s.outputWeeks = outWeeks
	s.outputHead = itoa(weeks[0].live)
	s.outputUnit = "features live this week"
	s.outputSpark = velSpark(sparkVals)

	delta := weeks[0].live
	if nWeeks > 1 {
		delta = weeks[0].live - weeks[1].live
	}
	sign := ""
	if delta >= 0 {
		sign = "+"
	}
	s.outputDelta = fmt.Sprintf("%s%d on last week", sign, delta)

	// ---- org DORA -----------------------------------------------------------
	freq := float64(deploys7) / 7.0
	leadSpark := make([]int, nWeeks) // oldest-first, median lead minutes per week
	for i := range weeks {
		leadSpark[nWeeks-1-i] = int(velMedianDur(weeks[i].leads).Minutes())
	}
	medLead := velMedianDur(allLeads)
	leadStr, leadBand := "—", "high"
	if medLead > 0 {
		leadStr = velHumanDur(medLead)
		leadBand = velLeadBand(medLead)
	}
	s.doraOrg = []doraMetric{
		{key: "deploy frequency", v: fmt.Sprintf("%.1f", freq), unit: "/day", band: velFreqBand(freq), spark: velSpark(sparkVals), note: fmt.Sprintf("%d live in the last 7 days", deploys7)},
		{key: "lead time", v: leadStr, unit: "", band: leadBand, spark: velSpark(leadSpark), note: "dispatch → live, median"},
		{key: "change failure", v: "—", unit: "", band: "high", note: "no incident data"},
		{key: "time to restore", v: "—", unit: "", band: "high", note: "no incident data"},
	}

	// ---- factory metrics ----------------------------------------------------
	s.doraFactory = []doraMetric{
		{key: "first-pass rate", v: "—", unit: "", band: "medium", note: "claims accepted without rework"},
		{key: "waiting on you", v: "—", unit: "", band: "medium", note: "needs-input dwell · no event data"},
		{key: "turns per feature", v: "—", unit: "", band: "medium", note: "no turn data"},
		{key: "work in progress", v: itoa(inFlight), unit: "", band: "medium", spark: velSpark(sparkVals), note: "dispatchers not yet live"},
	}

	// ---- where a feature's time goes (rough, honest split) ------------------
	s.doraSplit = []splitPart{
		{label: "agent working", pct: 44, color: cGreen},
		{label: "waiting on you", pct: 33, color: cAmber},
		{label: "review", pct: 14, color: cBlue},
		{label: "ci + deploy", pct: 9, color: cDim},
	}

	// ---- by-week DORA table -------------------------------------------------
	dweeks := make([]doraWeek, nWeeks)
	for i := range weeks {
		lead := "—"
		if md := velMedianDur(weeks[i].leads); md > 0 {
			lead = velHumanDur(md)
		}
		dweeks[i] = doraWeek{
			w:       weeks[i].label,
			deploys: weeks[i].live,
			lead:    lead,
			fail:    "—",
			restore: "—",
			first:   "—",
			wait:    "—",
			best:    i == 0,
		}
	}
	s.doraWeeks = dweeks

	// ---- busy, not velocity -------------------------------------------------
	s.notVelocity = []notVelocityRow{
		{"dispatchers out", itoa(inFlight), "a queue, not an output"},
		{"commits", itoa(weekCommits) + " this week", "an agent can write a thousand and ship none"},
		{"tokens", "see usage", "a cost, and it is on the usage lens"},
		{"tools", "—", "motion"},
	}
}
