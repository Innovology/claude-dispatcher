package cockpit

// collect_usage.go fills the USAGE lens. The bars are Claude Code's own figures
// wherever a session has reported them (internal/account: the status line's
// rate_limits, the percentages claude.ai's usage page shows), per account and
// with each window's real reset. Only where no session has do they fall back to
// the internal/usage learner — this machine's transcript tokens against a limit
// learned from 429s — which is a model, and says so wherever it shows. The user
// is on a subscription, so this speaks only in tokens and effort, never dollars.

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
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
	now := time.Now()
	sum := usage.Compute(state.Dir(), now)

	// ---- windows: the two real subscription windows -------------------------
	measured := usgMeasured(ctx.cfg, sum, now)
	s.usageWindows = measured
	if len(measured) == 0 {
		s.usageWindows = []usageWindow{
			usgWindow("5-hour session", sum.FiveHour),
			usgWindow("this week", sum.Weekly),
		}
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
	if p := usgMeasuredProjection(measured, now); p != "" {
		s.usageProjection = p
	} else if sum.Weekly.Cap > 0 {
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
		if len(measured) > 0 {
			advice = append(advice, usageAdviceItem{
				text: "the bars are Claude Code's own limit figures, as claude.ai shows them; token counts are this machine's transcripts", color: cMid,
			})
		} else {
			advice = append(advice, usageAdviceItem{
				text: "these bars are an estimate: limits are learned from real usage, filling toward the last cap you hit and lifting if you sail past it. Claude Code's own figures replace them once a session draws the status line `claude-dispatcher init` installs", color: cMid,
			})
		}
	}
	if !sum.LastLimit.IsZero() {
		advice = append(advice, usageAdviceItem{
			text:  "last limit hit " + usgAgo(sum.LastLimit) + " ago",
			color: cMid,
		})
	}
	s.usageAdvice = advice
}

// usgMeasured is every window Claude Code reported for each account, default
// first — empty when no session of any account has drawn the status line, and
// the lens then falls back to the learned estimate. The default account's rows
// carry this machine's transcript tokens as their detail, because the
// transcripts read are the default login's own.
func usgMeasured(cfg *config.Config, sum usage.Summary, now time.Time) []usageWindow {
	var out []usageWindow
	for _, a := range account.List(cfg) {
		l, ok := account.ReadLimits(state.Dir(), a.ConfigDir())
		if !ok {
			continue
		}
		var five, week usage.Stat
		if a.IsDefault() {
			five, week = sum.FiveHour, sum.Weekly
		}
		if l.FiveHour != nil {
			out = append(out, usgMeasuredWindow("5-hour session", a.Name, *l.FiveHour, 5*time.Hour, l.At, five, now))
		}
		if l.SevenDay != nil {
			out = append(out, usgMeasuredWindow("this week", a.Name, *l.SevenDay, 7*24*time.Hour, l.At, week, now))
		}
	}
	return out
}

// usgMeasuredWindow is one reported window as a lens row. A window that knows
// its reset has a pace, which a trailing estimate never did: the share used
// against the share of the window gone. It is left unset for the window's
// first tenth, where a few minutes' work would read as a runaway.
func usgMeasuredWindow(label, acct string, w account.Window, length time.Duration, at time.Time, local usage.Stat, now time.Time) usageWindow {
	left, reset := w.Left(now)
	win := usageWindow{label: label, account: acct, used: 100 - left, measured: true}
	if w.ResetsAt > 0 {
		win.resets = time.Unix(w.ResetsAt, 0)
	}
	switch {
	case reset:
		win.note = "reset " + usgWhen(win.resets, now) + " · no session has reported since"
	case win.resets.IsZero():
		win.note = "read " + usgAge(at, now)
	default:
		win.note = "resets " + usgWhen(win.resets, now) + " · read " + usgAge(at, now)
		if gone := float64(length-win.resets.Sub(now)) / float64(length); gone >= 0.1 && win.used > 0 {
			win.pace = math.Round(float64(win.used)/100/gone*10) / 10
		}
	}
	if local.Total > 0 {
		win.note += fmt.Sprintf(" · %s tok here in %d %s", usgTokens(local.Total), local.Sessions, plural(local.Sessions, "session", "sessions"))
	}
	return win
}

// usgMeasuredProjection sets the week against how much of it has gone, for the
// first account that reported one: both facts, and no forecast drawn from
// them. "" when no week was reported.
func usgMeasuredProjection(measured []usageWindow, now time.Time) string {
	for _, w := range measured {
		if w.label != "this week" || w.resets.IsZero() {
			continue
		}
		if !now.Before(w.resets) {
			return "This week reset " + usgWhen(w.resets, now) + " — no session has reported since"
		}
		gone := 100 - int(100*w.resets.Sub(now)/(7*24*time.Hour))
		return fmt.Sprintf("This week: %d%% used, with %d%% of the week gone · resets %s", w.used, gone, usgWhen(w.resets, now))
	}
	return ""
}

// usgWhen names a moment the way a reset is read: the time alone today, the
// weekday and time within the week, the date beyond it.
func usgWhen(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	switch {
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return t.Format("15:04")
	case t.Sub(now).Abs() < 6*24*time.Hour:
		return strings.ToLower(t.Format("Mon 15:04"))
	default:
		return t.Format("2 Jan")
	}
}

// usgAge is how old a reading is, as a note says it.
func usgAge(at, now time.Time) string {
	if now.Sub(at) < time.Minute {
		return "just now"
	}
	return usgAgoFrom(at, now) + " ago"
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
func usgAgo(t time.Time) string { return usgAgoFrom(t, time.Now()) }

// usgAgoFrom is usgAgo measured from now.
func usgAgoFrom(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
