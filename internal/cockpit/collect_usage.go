package cockpit

// collect_usage.go fills the USAGE lens from the internal/usage learner: the
// 5-hour rolling window and the weekly quota, measured from real consumption
// (Claude Code's stats cache + recent transcripts) and gauged against limits
// LEARNED empirically (there is no API for them — see internal/usage). The user
// is on a subscription, so this speaks only in tokens and effort, never dollars.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/usage"
)

// usgTokens renders a token count compactly: 2_500_000 → "2.5M", 40_000 → "40K".
func usgTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// usgWindow maps one learned window Stat to the lens's usageWindow row.
func usgWindow(label string, s usage.Stat) usageWindow {
	denom := s.Denom()
	used := 0
	if denom > 0 {
		used = 100 * s.Total / denom
		if used > 100 {
			used = 100
		}
	}
	// The note states what we are measuring against and how sure we are.
	var gauge string
	switch {
	case s.Cap > 0 && s.CapSource == "limit":
		gauge = "of ~" + usgTokens(s.Cap) + " learned limit"
	case s.Cap > 0:
		gauge = "of ~" + usgTokens(s.Cap) + " seen so far · learning"
	case denom > 0:
		gauge = usgTokens(s.Total) + " so far · no limit hit yet · learning"
	default:
		gauge = "no usage yet"
	}
	note := usgTokens(s.Total) + " tok · " + gauge
	if s.Sessions > 0 {
		note = fmt.Sprintf("%d sessions · %s", s.Sessions, note)
	}
	// pace is left at 0 — "not measured". Both windows are trailing, so there
	// is no elapsed share of a window to weigh the spent share against. It used
	// to be pinned at 1.0, which is a claim ("exactly on budget") and rendered
	// as one, on every window, on every install.
	return usageWindow{label: label, used: used, note: note}
}

func collectUsage(ctx *collectCtx, s *snapshot) {
	sum := usage.Compute(state.Dir(), time.Now())

	// ---- windows: the two real subscription windows -------------------------
	s.usageWindows = []usageWindow{
		usgWindow("5-hour session", sum.FiveHour),
		usgWindow("this week", sum.Weekly),
	}

	// ---- by model (this week) -----------------------------------------------
	notes := map[string]string{
		"fable":  "the top tier — hardest and longest-horizon work",
		"mythos": "the top tier — hardest and longest-horizon work",
		"opus":   "blocked, urgent and long-context work",
		"sonnet": "the default for feature work",
		"haiku":  "pr writing, changelogs, quick passes",
	}
	wk := sum.Weekly
	models := []usageModel{}
	for _, name := range usgModelOrder(wk.ByModel) {
		tok := wk.ByModel[name]
		if tok == 0 {
			continue
		}
		share := 0
		if wk.Total > 0 {
			share = 100 * tok / wk.Total
		}
		models = append(models, usageModel{
			name:  name,
			share: share,
			avg:   usgTokens(tok) + " this week",
			note:  notes[name],
		})
	}
	s.usageModels = models

	// ---- projection ---------------------------------------------------------
	if sum.Weekly.Cap > 0 {
		s.usageProjection = fmt.Sprintf("This week: %s of ~%s (%d%%) · 5-hour window: %s",
			usgTokens(sum.Weekly.Total), usgTokens(sum.Weekly.Cap), sum.Weekly.Pct(), usgTokens(sum.FiveHour.Total))
	} else {
		s.usageProjection = fmt.Sprintf("This week: %s tokens · no weekly limit learned yet — hit one once and the cockpit remembers it",
			usgTokens(sum.Weekly.Total))
	}

	// ---- advice / what would change it --------------------------------------
	advice := []usageAdviceItem{}
	if wk.Total > 0 {
		// The premium tiers together, not opus alone — fable/mythos price above
		// opus, so counting only opus understates what the week actually cost.
		premium := 0
		for _, name := range usgPremium {
			premium += wk.ByModel[name]
		}
		if premium > 0 {
			advice = append(advice, usageAdviceItem{
				text: fmt.Sprintf("%s is %d%% of the week's tokens — the expensive share",
					usgJoin(usgPresent(wk.ByModel, usgPremium)), 100*premium/wk.Total), color: cAmber,
			})
		}
		advice = append(advice, usageAdviceItem{
			text: "limits are learned from real usage: the bar fills toward the last cap you hit, and lifts if you sail past it", color: cMid,
		})
	}
	if !sum.LastLimit.IsZero() {
		advice = append(advice, usageAdviceItem{
			text:  "last limit hit " + usgAgo(sum.LastLimit) + " ago",
			color: cMid,
		})
	}
	s.usageAdvice = advice
}

// usgPremium are the tiers that price above the rest — the "expensive share".
var usgPremium = []string{"fable", "mythos", "opus"}

// usgModelOrder lists the models to render: the known families in tier order
// first, then anything unrecognised, heaviest first. An unknown model shows
// under its own id rather than being swept into a nameless "other" row.
func usgModelOrder(byModel map[string]int) []string {
	known := map[string]bool{}
	for _, f := range usage.Families {
		known[f] = true
	}
	order := append([]string{}, usage.Families...)

	rest := []string{}
	for name := range byModel {
		if !known[name] {
			rest = append(rest, name)
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		if byModel[rest[i]] != byModel[rest[j]] {
			return byModel[rest[i]] > byModel[rest[j]]
		}
		return rest[i] < rest[j]
	})
	return append(order, rest...)
}

// usgPresent keeps only the names that drew tokens this week.
func usgPresent(byModel map[string]int, names []string) []string {
	out := []string{}
	for _, n := range names {
		if byModel[n] > 0 {
			out = append(out, n)
		}
	}
	return out
}

// usgJoin renders a name list as prose: "fable", "fable and opus",
// "fable, mythos and opus".
func usgJoin(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// usgAgo formats a rough age like "3h", "2d".
func usgAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
