package cockpit

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/usage"
)

// The lens read 77% and 88% from its learned estimate while claude.ai said
// 45% and 33% for the same login, and its header named a reset day that was
// only ever today's weekday. Where a session has reported Claude Code's own
// figures, those are the bars, with the window's real reset and a pace.
func TestUsageShowsClaudeCodesOwnFigures(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	// Tue 13 Oct 2026 13:26 UTC: 43m left of the 5-hour window, the week
	// resetting Thu 15 Oct 08:00 UTC.
	now := time.Unix(1791898000, 0)
	payload := `{"rate_limits":{"five_hour":{"used_percentage":45,"resets_at":1791900600},` +
		`"seven_day":{"used_percentage":33,"resets_at":1792051200}}}`
	if err := account.RecordStatusLine(state.Dir(), account.DefaultConfigDir(), []byte(payload), now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	local := usage.Summary{
		FiveHour: usage.Stat{Total: 27_700_000, Sessions: 7, HighWater: 35_800_000},
		Weekly:   usage.Stat{Total: 253_700_000, Sessions: 20, HighWater: 288_000_000},
	}
	got := usgMeasured(&config.Config{}, local, now)
	if len(got) != 2 {
		t.Fatalf("windows = %+v, want the two reported", got)
	}
	five, week := got[0], got[1]
	if !five.measured || five.used != 45 || week.used != 33 {
		t.Errorf("used = %d/%d measured=%v, want Claude Code's 45/33", five.used, week.used, five.measured)
	}
	if !week.resets.Equal(time.Unix(1792051200, 0)) {
		t.Errorf("week resets %v", week.resets)
	}
	if !strings.Contains(five.note, "read 2m ago") || !strings.Contains(five.note, "27.7M tok here in 7 sessions") {
		t.Errorf("5-hour note = %q", five.note)
	}
	// 45% used with 86% of the window gone; 33% with 75% gone.
	if five.pace != 0.5 || week.pace != 0.4 {
		t.Errorf("pace = %v/%v, want 0.5/0.4", five.pace, week.pace)
	}
	if p := usgMeasuredProjection(got, now); !strings.Contains(p, "33% used, with 75% of the week gone") {
		t.Errorf("projection = %q", p)
	}

	// Once the window has passed its reset it reads empty, and says why.
	after := usgMeasured(&config.Config{}, usage.Summary{}, time.Unix(1791900600, 0))
	if after[0].used != 0 || !strings.Contains(after[0].note, "no session has reported since") || after[0].pace != 0 {
		t.Errorf("past its reset: %+v", after[0])
	}
}

// With no reading the lens is the estimate, marked as one: "≈" on the figure
// and no reset day, which a trailing window does not have.
func TestTheEstimateSaysItIsOne(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if got := usgMeasured(&config.Config{}, usage.Summary{}, time.Now()); len(got) != 0 {
		t.Fatalf("measured windows with no reading: %+v", got)
	}
	saved := usageWindows
	defer func() { usageWindows = saved }()

	usageWindows = []usageWindow{usgWindow("this week", usage.Stat{Total: 10, HighWater: 40})}
	m := newModel()
	m.cfg = &config.Config{Roots: []string{t.TempDir()}}
	m.width = 200
	header := stripAnsi(m.headerView())
	if !strings.Contains(header, "week ≈25%") || strings.Contains(header, "resets") {
		t.Errorf("header = %q, want an estimate with no reset day", header)
	}

	usageWindows = []usageWindow{{label: "this week", used: 33, measured: true, resets: time.Unix(1792051200, 0)}}
	header = stripAnsi(m.headerView())
	if !strings.Contains(header, "week 33% · resets thu") {
		t.Errorf("header = %q, want the reported week and its real reset", header)
	}
}
