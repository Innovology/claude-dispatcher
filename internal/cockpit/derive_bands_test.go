package cockpit

// derive_bands_test.go pins the small classification helpers the lenses lean
// on: the DORA bands, what a running dispatcher is "doing", and when a feature
// counts as live. They are one-line switches, which is exactly why they drift
// unnoticed — a band boundary moving by an hour silently re-grades a whole
// portfolio.

import (
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

func TestVelFreqBandBoundaries(t *testing.T) {
	cases := []struct {
		perDay float64
		want   string
	}{
		{2.0, "elite"}, {1.0, "elite"},
		{0.99, "high"}, {0.3, "high"},
		{0.29, "medium"}, {0.1, "medium"},
		{0.09, "low"}, {0, "low"},
	}
	for _, c := range cases {
		if got := velFreqBand(c.perDay); got != c.want {
			t.Errorf("velFreqBand(%v) = %q, want %q", c.perDay, got, c.want)
		}
	}
}

func TestVelLeadBandBoundaries(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{time.Hour, "elite"},
		{23 * time.Hour, "elite"},
		{24 * time.Hour, "high"},  // a day exactly is no longer elite
		{167 * time.Hour, "high"}, // just under a week
		{168 * time.Hour, "medium"},
		{719 * time.Hour, "medium"},
		{720 * time.Hour, "low"}, // a month or worse
	}
	for _, c := range cases {
		if got := velLeadBand(c.d); got != c.want {
			t.Errorf("velLeadBand(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// "Live" is deploy time when there is one, merge time when the record is done,
// and nothing at all otherwise — a feature still in flight has no live time and
// must not be counted as though it shipped.
func TestVelLiveTime(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-2 * time.Hour)

	if got, ok := velLiveTime(&state.Dispatch{DeployedAt: &now}); !ok || !got.Equal(now) {
		t.Errorf("a deployed record should be live at its deploy time")
	}
	if got, ok := velLiveTime(&state.Dispatch{Status: state.StatusDone, PRMergedAt: &earlier}); !ok || !got.Equal(earlier) {
		t.Errorf("a done record with a merge should be live at the merge")
	}
	if got, ok := velLiveTime(&state.Dispatch{Status: state.StatusDone, UpdatedAt: earlier}); !ok || !got.Equal(earlier) {
		t.Errorf("a done record with no merge falls back to its last update")
	}
	if _, ok := velLiveTime(&state.Dispatch{Status: state.StatusWorking}); ok {
		t.Error("a working record is not live")
	}
}

// The chain segment is inferred from the last tool in the transcript. A tool we
// cannot place lights nothing rather than being filed under the nearest guess,
// and a session we have read nothing from lights nothing either.
func TestCQPhaseFromTranscript(t *testing.T) {
	cases := []struct {
		tail []string
		want string
	}{
		{[]string{"⚙ Read", "⚙ Edit"}, "act"},
		{[]string{"⚙ Bash"}, "observe"},
		{[]string{"⚙ Grep"}, "plan"},
		{[]string{"⚙ Task"}, "plan"},
		{[]string{"⚙ TodoWrite"}, "plan"},
		{[]string{"⚙ SomeNewTool"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := cqPhase(c.tail, &state.Dispatch{}); got != c.want {
			t.Errorf("cqPhase(%v) = %q, want %q", c.tail, got, c.want)
		}
	}
	// An open PR is not an inference: the work has left the dispatcher and is
	// waiting on the forge, whatever tool it last touched.
	shipped := &state.Dispatch{PRNumber: 7, PRState: "OPEN"}
	if got := cqPhase([]string{"⚙ Edit"}, shipped); got != "ship" {
		t.Errorf("an open pr should read as ship, got %q", got)
	}
}

func TestFanOutIsBounded(t *testing.T) {
	if n := fanOut(); n < 4 || n > 12 {
		t.Errorf("fanOut = %d, want it clamped to [4,12] so a big portfolio cannot spawn hundreds of processes", n)
	}
}
