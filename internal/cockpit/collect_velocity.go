package cockpit

// collect_velocity.go fills the VELOCITY lens (velocity.go) from the dispatch
// record history and the lifecycle event log — no external service. "Output" is
// what actually reached production: a feature is live when it has a DeployedAt
// (or, lacking a deploy workflow, a merge that flipped it to done). Everything
// is bucketed by ISO week. DORA metrics we can honestly derive (deploy
// frequency, lead time, work in progress) are computed; the rest render as "—"
// with a neutral band rather than a fabricated number.
//
// "Where the time goes" used to be four hardcoded percentages — the design's
// mock, shipped as if measured. It is now summed from events.jsonl, which is
// the only record of who was holding a session and for how long, and it renders
// nothing at all on a portfolio that has not logged an event yet.

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

// velDwellState buckets a lifecycle event by the state it leaves a dispatcher
// in, so the interval that follows can be billed to someone. "" means the event
// says nothing about who is holding the work (SessionEnd, or anything the hook
// grows later), and the interval after it is billed to nobody.
func velDwellState(event string) string {
	switch event {
	case "SessionStart", "UserPromptSubmit":
		return "working"
	case "Stop", "Notification:idle_prompt":
		return "waiting"
	case "Notification:permission_prompt":
		return "blocked"
	}
	return ""
}

// velDwell is measured time, per state, across every dispatcher in the window.
// waits keeps each individual hand-back to the human so the factory metric can
// quote a median rather than only the total.
type velDwell struct {
	working, waiting, blocked time.Duration
	waits                     []time.Duration
}

// velDwellSince walks the lifecycle log and sums how long dispatchers spent
// working, waiting on the human, and stopped on a permission prompt.
//
// The gap between two consecutive events belongs to the state the earlier one
// left the dispatcher in — that is the only thing the log can tell us, and it
// is enough for the split the lens draws. Events are grouped per dispatcher (by
// session id when the dispatcher id is missing, which is how a session started
// outside the cockpit is logged) because two dispatchers running side by side
// interleave in one file and the gaps between their events are not intervals.
//
// Nothing is billed after the last event of a dispatcher: a session that has
// gone quiet is not thereby "waiting on you" forever.
func velDwellSince(cut time.Time) velDwell {
	byKey := map[string][]state.Event{}
	for _, ev := range state.LoadEvents() {
		key := ev.DispatcherID
		if key == "" {
			key = ev.SessionID
		}
		if key == "" || ev.Time.Before(cut) {
			continue
		}
		byKey[key] = append(byKey[key], ev)
	}

	var d velDwell
	for _, list := range byKey {
		// The log is append-only and therefore already chronological, but it is
		// written by a process per hook invocation — sort rather than trust the
		// interleaving of two hooks that fired in the same instant.
		sort.Slice(list, func(i, j int) bool { return list[i].Time.Before(list[j].Time) })
		for i := 0; i+1 < len(list); i++ {
			span := list[i+1].Time.Sub(list[i].Time)
			if span <= 0 {
				continue
			}
			switch velDwellState(list[i].Event) {
			case "working":
				d.working += span
			case "waiting":
				d.waiting += span
				d.waits = append(d.waits, span)
			case "blocked":
				d.blocked += span
			}
		}
	}
	return d
}

// velSplitFrom turns measured dwell into the lens's percentage split. A state
// with no time in it is dropped rather than drawn as a 0% bar, and a window
// with no events at all returns an empty (non-nil) split so the lens omits the
// section instead of showing a shape with nothing in it.
func velSplitFrom(d velDwell) []splitPart {
	total := d.working + d.waiting + d.blocked
	out := []splitPart{}
	if total <= 0 {
		return out
	}
	for _, p := range []struct {
		label string
		d     time.Duration
		color string
	}{
		{"dispatcher working", d.working, cGreen},
		{"waiting on you", d.waiting, cAmber},
		{"blocked on approval", d.blocked, cRed},
	} {
		if p.d <= 0 {
			continue
		}
		// Seconds, not raw nanoseconds: a busy portfolio's totals overflow an
		// int64 once multiplied by 100.
		pct := int(p.d.Seconds()/total.Seconds()*100 + 0.5)
		out = append(out, splitPart{label: p.label, pct: pct, color: p.color})
	}
	return out
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

	// ---- dwell, from the lifecycle log --------------------------------------
	// The one place the cockpit can see inside a session: who was holding the
	// work, and for how long. Same window as everything else on the lens.
	dwell := velDwellSince(now.AddDate(0, 0, -7*nWeeks))
	s.doraSplit = velSplitFrom(dwell)

	// ---- factory metrics ----------------------------------------------------
	waitV, waitNote := "—", "median hand-back to you · no lifecycle events yet"
	if med := velMedianDur(dwell.waits); med > 0 {
		waitV = velHumanDur(med)
		waitNote = fmt.Sprintf("median of %d hand-backs to you", len(dwell.waits))
	}
	// What the repositories say, summed across products — these count work done
	// outside the dispatcher too, which every other figure on this lens misses.
	deploys, merged, commits := 0, 0, 0
	for _, p := range s.products {
		deploys += p.deploys7d
		merged += p.merged7d
		commits += p.commits7d
	}
	deployV, deployNote := "—", "no deploy workflow found"
	if deploys > 0 {
		deployV, deployNote = itoa(deploys), "successful deploy runs, last 7 days"
	}
	mergedV, mergedNote := "—", "nothing merged in the last 7 days"
	if merged > 0 {
		mergedNote = "merged in the last 7 days"
		mergedV = itoa(merged)
		if commits > 0 {
			mergedNote += " · " + itoa(commits) + " " + plural(commits, "commit", "commits")
		}
	}

	s.doraFactory = []doraMetric{
		{key: "deployments", v: deployV, unit: "", band: "medium", note: deployNote},
		{key: "prs merged", v: mergedV, unit: "", band: "medium", note: mergedNote},
		{key: "waiting on you", v: waitV, unit: "", band: "medium", note: waitNote},
		{key: "work in progress", v: itoa(inFlight), unit: "", band: "medium", spark: velSpark(sparkVals), note: "dispatchers not yet live"},
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
	// The design's fourth row ("tools · — · motion") is dropped: nothing counts
	// tool calls, so it was a label with a dash where a figure should be.
	s.notVelocity = []notVelocityRow{
		{"dispatchers out", itoa(inFlight), "a queue, not an output"},
		{"commits", itoa(weekCommits) + " this week", "a dispatcher can write a thousand and ship none"},
		{"tokens", "see usage", "a cost, and it is on the usage lens"},
	}
}
