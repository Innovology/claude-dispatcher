package cockpit

// lenses_v4_test.go covers the figures the backlog, usage and velocity lenses
// stopped inventing: the dwell split measured from the lifecycle event log, the
// dispatch prompt composed from a ticket, and the "where" line's missing
// clauses. Each test asserts the honest-empty case as hard as the populated
// one — an empty portfolio rendering a plausible number is the failure mode
// these replaced.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/usage"
)

// writeEvents points the state dir at a temp directory and writes an
// events.jsonl from the given lines.
func writeEvents(t *testing.T, lines ...string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ev renders one event log line at t0+offset.
func ev(t0 time.Time, offset time.Duration, event, id string) string {
	e := state.Event{Time: t0.Add(offset), Event: event, DispatcherID: id}
	return `{"time":"` + e.Time.Format(time.RFC3339Nano) + `","event":"` + e.Event + `","dispatcher_id":"` + e.DispatcherID + `"}`
}

func TestVelDwellSinceSplitsByHolder(t *testing.T) {
	t0 := time.Now().Add(-2 * time.Hour)
	writeEvents(t,
		ev(t0, 0, "SessionStart", "a"),                                // working for 30m
		ev(t0, 30*time.Minute, "Stop", "a"),                           // waiting for 20m
		ev(t0, 50*time.Minute, "UserPromptSubmit", "a"),               // working for 10m
		ev(t0, 60*time.Minute, "Notification:permission_prompt", "a"), // blocked 5m
		ev(t0, 65*time.Minute, "UserPromptSubmit", "a"),               // working 5m
		ev(t0, 70*time.Minute, "SessionEnd", "a"),                     // nothing after this
	)

	d := velDwellSince(t0.Add(-time.Hour))
	if got, want := d.working, 45*time.Minute; got != want {
		t.Errorf("working = %v, want %v", got, want)
	}
	if got, want := d.waiting, 20*time.Minute; got != want {
		t.Errorf("waiting = %v, want %v", got, want)
	}
	if got, want := d.blocked, 5*time.Minute; got != want {
		t.Errorf("blocked = %v, want %v", got, want)
	}
	if len(d.waits) != 1 {
		t.Fatalf("waits = %v, want one hand-back", d.waits)
	}
}

// Two dispatchers interleave in one log. The gap between A's event and B's is
// not an interval, and billing it to A is how a two-session portfolio would
// come to look like it spends all its time waiting.
func TestVelDwellSinceKeepsDispatchersApart(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)
	writeEvents(t,
		ev(t0, 0, "SessionStart", "a"),
		ev(t0, 1*time.Minute, "SessionStart", "b"),
		ev(t0, 10*time.Minute, "Stop", "a"),
		ev(t0, 11*time.Minute, "Stop", "b"),
	)

	d := velDwellSince(t0.Add(-time.Hour))
	if got, want := d.working, 20*time.Minute; got != want {
		t.Errorf("working = %v, want %v (10m each, not the interleaved gaps)", got, want)
	}
	if d.waiting != 0 {
		t.Errorf("waiting = %v, want 0 — nothing follows either Stop", d.waiting)
	}
}

func TestVelDwellSinceIgnoresEventsBeforeTheCut(t *testing.T) {
	t0 := time.Now().Add(-90 * time.Minute)
	writeEvents(t,
		ev(t0, 0, "SessionStart", "a"),
		ev(t0, 10*time.Minute, "Stop", "a"),
	)
	if d := velDwellSince(time.Now()); d.working != 0 || d.waiting != 0 || d.blocked != 0 {
		t.Errorf("dwell = %+v, want zero — every event predates the cut", d)
	}
}

func TestVelDwellSinceNoLog(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := velDwellSince(time.Now().Add(-time.Hour))
	if d.working != 0 || d.waiting != 0 || d.blocked != 0 || d.waits != nil {
		t.Errorf("dwell = %+v, want zero on a portfolio with no event log", d)
	}
	if got := velSplitFrom(d); got == nil || len(got) != 0 {
		t.Errorf("split = %#v, want empty-but-non-nil so the lens omits the section", got)
	}
}

func TestVelSplitFromDropsEmptyStatesAndTotalsRoughly100(t *testing.T) {
	parts := velSplitFrom(velDwell{working: 60 * time.Minute, waiting: 40 * time.Minute})
	if len(parts) != 2 {
		t.Fatalf("parts = %#v, want the two states with time in them", parts)
	}
	if parts[0].label != "dispatcher working" || parts[0].pct != 60 {
		t.Errorf("parts[0] = %+v, want dispatcher working at 60%%", parts[0])
	}
	if parts[1].label != "waiting on you" || parts[1].pct != 40 {
		t.Errorf("parts[1] = %+v, want waiting on you at 40%%", parts[1])
	}
	for _, p := range parts {
		if p.color == "" {
			t.Errorf("%q has no colour", p.label)
		}
	}
}

// The split is the source for the amber prose line; it must read its own data
// rather than restate the design's third.
func TestVelSplitPct(t *testing.T) {
	old := doraSplit
	t.Cleanup(func() { doraSplit = old })

	doraSplit = nil
	if got := velSplitPct("waiting on you"); got != 0 {
		t.Errorf("velSplitPct on an empty split = %d, want 0", got)
	}
	doraSplit = []splitPart{{label: "waiting on you", pct: 71, color: cAmber}}
	if got := velSplitPct("waiting on you"); got != 71 {
		t.Errorf("velSplitPct = %d, want 71", got)
	}
	if got := velSplitPct("blocked on approval"); got != 0 {
		t.Errorf("velSplitPct for an absent state = %d, want 0", got)
	}
}

func TestBlkPromptCarriesTheTicket(t *testing.T) {
	got := blkPrompt("GitHub issue", "shop-api#41", "Cart totals drift", "Steps to reproduce…")
	if !strings.Contains(got, "shop-api#41") || !strings.Contains(got, "Cart totals drift") {
		t.Errorf("prompt = %q, want the id and title", got)
	}
	if !strings.Contains(got, "Steps to reproduce") {
		t.Errorf("prompt = %q, want the body", got)
	}

	// A ticket with no body still briefs the dispatcher.
	bare := blkPrompt("Azure Boards work item", "AB#7", "Rotate the signing key", "")
	if bare == "" || strings.HasSuffix(bare, "\n\n") {
		t.Errorf("bodyless prompt = %q, want just the headline", bare)
	}
}

func TestBlkPromptTruncatesOnARuneBoundary(t *testing.T) {
	body := strings.Repeat("é", 2000) // two bytes a rune, so 1200 lands mid-rune
	got := blkPrompt("GitHub issue", "r#1", "t", body)
	if strings.ContainsRune(got, '�') {
		t.Error("prompt carries a replacement rune — the cut split a rune in half")
	}
	if !utf8.ValidString(got) {
		t.Error("prompt is not valid UTF-8")
	}
	if !strings.Contains(got, "truncated") {
		t.Error("prompt was cut without admitting it")
	}
}

func TestBacklogWhereOmitsWhatTheTicketDoesNotHave(t *testing.T) {
	cases := []struct {
		name string
		in   ticket
		want string
	}{
		{"full", ticket{product: "shop", repo: "shop-api", labels: "bug"}, "shop / shop-api · bug"},
		{"no product", ticket{repo: "shop-api", labels: "bug"}, "shop-api · bug"},
		{"linear", ticket{labels: "In Progress"}, "In Progress"},
		{"bare", ticket{}, ""},
	}
	for _, c := range cases {
		if got := backlogWhere(c.in); got != c.want {
			t.Errorf("%s: backlogWhere = %q, want %q", c.name, got, c.want)
		}
	}
}

// The design's fallback is PLUGINS[3]; the collector never ships four.
func TestPluginForRepoFallbackDoesNotIndexPastTheEnd(t *testing.T) {
	old := plugins
	t.Cleanup(func() { plugins = old })

	plugins = []plugin{
		{id: "adr-tools", name: "adr-tools", repos: []string{"shop-api"}},
		{id: "builtin", name: "cockpit records", repos: []string{"shop-web"}},
	}
	if got := pluginForRepo("shop-api").id; got != "adr-tools" {
		t.Errorf("wired repo resolved to %q, want adr-tools", got)
	}
	if got := pluginForRepo("never-seen").id; got != "builtin" {
		t.Errorf("unwired repo resolved to %q, want the builtin fallback", got)
	}

	plugins = nil
	if got := pluginForRepo("anything"); got.id != "" {
		t.Errorf("empty plugin list resolved to %+v, want the zero plugin", got)
	}
}

// usgWindow used to pin pace at 1.0, which the header read as "exactly on
// budget" on every install. Both windows are trailing; there is no pace.
func TestUsgWindowLeavesPaceUnmeasured(t *testing.T) {
	w := usgWindow("this week", usage.Stat{
		Total: 500_000, ByModel: map[string]int{"sonnet": 500_000},
		Sessions: 3, Cap: 1_000_000, CapSource: "limit",
	})
	if w.pace != 0 {
		t.Errorf("pace = %v, want 0 (not measured) for a trailing window", w.pace)
	}
	if w.used <= 0 {
		t.Errorf("used = %d, want the real percentage", w.used)
	}
	if !strings.Contains(w.note, "tok") {
		t.Errorf("note = %q, want the token count it was measured from", w.note)
	}
}
