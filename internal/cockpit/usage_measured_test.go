package cockpit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

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
	// The reading resets Thu 08:00 UTC, which is Wednesday west of UTC-8.
	prev := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = prev })
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
	header := ansi.Strip(m.headerView())
	if !strings.Contains(header, "week ≈25%") || strings.Contains(header, "resets") {
		t.Errorf("header = %q, want an estimate with no reset day", header)
	}

	usageWindows = []usageWindow{{label: "this week", used: 33, measured: true, resets: time.Unix(1792051200, 0)}}
	header = ansi.Strip(m.headerView())
	if !strings.Contains(header, "week 33% · resets thu") {
		t.Errorf("header = %q, want the reported week and its real reset", header)
	}
}

// collectUsage as a whole, from a reading: the bars, the projection and the
// advice all speak of Claude Code's own figures rather than the learner's.
func TestCollectUsageSpeaksOfTheReading(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	now := time.Now()

	dir := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"sessionId":"s","message":{"model":"claude-opus-4","usage":{"input_tokens":1000,"output_tokens":500}}}`+"\n",
		now.Add(-time.Minute).UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	reading := fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":40,"resets_at":%d},"seven_day":{"used_percentage":30,"resets_at":%d}}}`,
		now.Add(2*time.Hour).Unix(), now.Add(3*24*time.Hour).Unix())
	if err := account.RecordStatusLine(state.Dir(), account.DefaultConfigDir(), []byte(reading), now); err != nil {
		t.Fatal(err)
	}

	var s snapshot
	collectUsage(&collectCtx{cfg: &config.Config{}}, &s)
	if len(s.usageWindows) != 2 || !s.usageWindows[0].measured || s.usageWindows[1].used != 30 {
		t.Fatalf("windows = %+v, want the reading's", s.usageWindows)
	}
	if !strings.Contains(s.usageProjection, "This week: 30% used") {
		t.Errorf("projection = %q", s.usageProjection)
	}
	var said []string
	for _, a := range s.usageAdvice {
		said = append(said, a.text)
	}
	if all := strings.Join(said, " | "); !strings.Contains(all, "Claude Code's own limit figures") || strings.Contains(all, "estimate") {
		t.Errorf("advice = %q", all)
	}
}

// The edges a reading can have, each said as what is known: a window that
// reports no reset has an age and no pace, a week that has rolled over since
// says so, and a reset is named by the time, the weekday or the date.
func TestUsageEdgesSayWhatTheyKnow(t *testing.T) {
	prev := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = prev })
	now := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC) // a Tuesday

	w := usgMeasuredWindow("this week", "default", account.Window{UsedPct: 20}, 7*24*time.Hour, now, usage.Stat{}, now)
	if w.note != "read just now" || w.pace != 0 || !w.resets.IsZero() {
		t.Errorf("a window with no reset: %+v", w)
	}
	rolled := []usageWindow{{label: "this week", measured: true, resets: now.Add(-time.Hour)}}
	if p := usgMeasuredProjection(rolled, now); !strings.Contains(p, "no session has reported since") {
		t.Errorf("a rolled-over week: %q", p)
	}
	for at, want := range map[time.Time]string{
		now.Add(2 * time.Hour):       "15:00",
		now.Add(48 * time.Hour):      "thu 13:00",
		now.Add(10 * 24 * time.Hour): "23 Oct",
	} {
		if got := usgWhen(at, now); got != want {
			t.Errorf("usgWhen(%v) = %q, want %q", at, got, want)
		}
	}
	if got := usgAge(now.Add(-90*time.Minute), now); got != "1h ago" {
		t.Errorf("usgAge = %q", got)
	}
	for names, want := range map[string]string{"": "", "fable": "fable", "fable,opus": "fable and opus", "fable,mythos,opus": "fable, mythos and opus"} {
		var in []string
		if names != "" {
			in = strings.Split(names, ",")
		}
		if got := usgJoin(in); got != want {
			t.Errorf("usgJoin(%q) = %q, want %q", names, got, want)
		}
	}
	// A model the families do not know shows under its own id, heaviest first.
	order := usgModelOrder(map[string]int{"opus": 5, "zeta": 3, "gpt-x": 3, "big-new": 9})
	if tail := strings.Join(order[len(order)-3:], ","); tail != "big-new,gpt-x,zeta" {
		t.Errorf("unknown models ordered %q", tail)
	}
}
