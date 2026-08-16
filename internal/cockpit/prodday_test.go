package cockpit

// prodday_test.go covers the day arithmetic the products lens counts by. Every
// case here is run in a zone that is NOT UTC, because that is the whole bug: on
// a UTC machine the broken and the fixed version agree.

import (
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
)

// bst is far enough from UTC that a local midnight and a UTC midnight are
// unmistakably different instants, in the direction a European user sees.
var bst = time.FixedZone("test+01", 60*60)

// A merge time comes off the forge in UTC and `now` off the clock in local, so
// "did this go live today" compared two midnights an offset apart and answered
// no every time. The portfolio table's LIVE TODAY column is that comparison.
func TestProdSameDayAcrossZones(t *testing.T) {
	local := time.Date(2026, 8, 16, 14, 30, 0, 0, bst)
	forge := local.UTC() // the same instant, as gh would report it

	if !prodSameDay(forge, local) {
		t.Error("the same instant in two zones is not the same day")
	}
	// Early afternoon local is still the same UTC date here, so the two must
	// agree — and an hour after local midnight is the case that used to slip a
	// day, since it is still yesterday in UTC.
	justAfterMidnight := time.Date(2026, 8, 16, 0, 30, 0, 0, bst)
	if !prodSameDay(justAfterMidnight.UTC(), justAfterMidnight) {
		t.Error("00:30 local is not its own day once converted to UTC")
	}
	// And genuinely different days must still read as different.
	if prodSameDay(local.AddDate(0, 0, -1), local) {
		t.Error("yesterday reads as today")
	}
}

// prodDayLabel groups the shipped tab. Two features shipped the same local
// afternoon must land under one heading, whichever zone their timestamps came
// from.
func TestProdDayLabelIsStableAcrossZones(t *testing.T) {
	now := time.Date(2026, 8, 16, 14, 0, 0, 0, bst)
	fromForge := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	fromClock := time.Date(2026, 8, 16, 11, 0, 0, 0, bst)

	a := prodDayLabel(prodDay(fromForge), now)
	b := prodDayLabel(prodDay(fromClock), now)
	if a != b {
		t.Errorf("one afternoon produced two headings: %q and %q", a, b)
	}
	if a != "today" {
		t.Errorf("today's ships are headed %q", a)
	}
	if got := prodDayLabel(prodDay(now.AddDate(0, 0, -1)), now); got != "yesterday" {
		t.Errorf("yesterday is headed %q", got)
	}
}

// The end of it, through the collector: a feature whose deploy time is a UTC
// timestamp from earlier the same local day has to count towards LIVE TODAY.
// The column read zero for every non-UTC user however much had shipped.
func TestLiveTodayCountsAForgeTimestamp(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	// Mid-morning local, so "today" is unambiguous in both zones and the test
	// cannot pass or fail on the hour it happens to run at.
	now := time.Now()
	if h := now.Hour(); h < 2 || h > 21 {
		t.Skip("too close to midnight for the local/UTC dates to be comparable")
	}
	deployed := now.Add(-time.Hour).UTC()

	cfg := &config.Config{Products: map[string][]string{"acme": {"acme-hq"}}}
	var s snapshot
	collectProducts(&collectCtx{cfg: cfg, records: []*state.Dispatch{{
		ID: "rec", Feature: "csv export", RepoName: "acme-hq", Product: "acme",
		Status: state.StatusDone, StatusReason: "deployed — live",
		PRNumber: 4, PRState: "MERGED", PRMergedAt: &deployed, DeployedAt: &deployed,
		CreatedAt: now.Add(-4 * time.Hour), UpdatedAt: deployed,
	}}}, &s)

	for _, p := range s.products {
		if p.name != "acme" {
			continue
		}
		if p.live != 1 {
			t.Errorf("LIVE TODAY = %d, want 1 — a deploy an hour ago did not count", p.live)
		}
		// The sparkline's last bar is today; a deploy today must move it off the
		// floor rather than land a day out.
		if p.spark == "" || strings.HasSuffix(p.spark, "▁") {
			t.Errorf("7d sparkline %q does not show today's deploy in its last bar", p.spark)
		}
		return
	}
	t.Fatal("no acme product collected")
}
