package cockpit

// collect_usage.go fills the USAGE lens from the internal/usage learner: the
// 5-hour rolling window and the weekly quota, measured from real consumption
// (Claude Code's stats cache + recent transcripts) and gauged against limits
// LEARNED empirically (there is no API for them — see internal/usage). The user
// is on a subscription, so this speaks only in tokens and effort, never dollars.

import (
	"fmt"
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
	return usageWindow{label: label, used: used, note: note, pace: 1.0}
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
		"opus":   "blocked, urgent and long-context work",
		"sonnet": "the default for feature work",
		"haiku":  "pr writing, changelogs, quick passes",
		"other":  "other models",
	}
	wk := sum.Weekly
	models := []usageModel{}
	for _, name := range []string{"opus", "sonnet", "haiku", "other"} {
		tok := wk.ByModel[name]
		if tok == 0 {
			continue
		}
		share := 0
		if wk.Total > 0 {
			share = 100 * tok / wk.Total
		}
		models = append(models, usageModel{
			name:     name,
			share:    share,
			sessions: 0,
			avg:      usgTokens(tok) + " this week",
			note:     notes[name],
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
		if op := wk.ByModel["opus"]; op > 0 {
			advice = append(advice, usageAdviceItem{
				text: fmt.Sprintf("opus is %d%% of the week's tokens — the expensive share", 100*op/wk.Total), color: cAmber,
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
