package steward

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A ruling is written down, has an end, and only a merge ruling grants a hand.
func TestRulings(t *testing.T) {
	withFakes(t, &fakeSup{trusted: true}, exePath(t))
	t0 := time.Now()
	guide, err := AddRuling("player", Ruling{Text: "  #931 waits for my   goldens "})
	if err != nil {
		t.Fatal(err)
	}
	if guide.Text != "#931 waits for my goldens" || guide.Until.Sub(guide.GivenAt) != DefaultRulingFor {
		t.Fatalf("ruling = %+v", guide)
	}
	merge, err := AddRuling("player", Ruling{Text: "merge #931", Allow: []string{AllowMerge}, PRs: []int{931}, Repo: "player-app", Until: t0.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if got := ActiveRulings("player", t0); len(got) != 2 {
		t.Fatalf("active = %+v", got)
	}
	if r, ok := MergeRuling("player", "player-app", 931, t0); !ok || r.ID != merge.ID {
		t.Fatal("a merge ruling must grant the merge")
	}
	if _, ok := MergeRuling("player", "player-app", 99, t0); ok {
		t.Fatal("a ruling for #931 granted #99")
	}
	if _, ok := MergeRuling("player", "design-system", 931, t0); ok {
		t.Fatal("a ruling for one repo granted another")
	}
	if _, ok := MergeRuling("player", "player-app", 931, t0.Add(2*time.Hour)); ok {
		t.Fatal("a ruling past its time granted a merge")
	}
	if _, ok := MergeRuling("tools", "player-app", 931, t0); ok {
		t.Fatal("one product's ruling granted another's")
	}
	product, ended, err := EndRuling(merge.ID)
	if err != nil || product != "player" || ended.EndedAt == nil {
		t.Fatalf("end: %v %s %+v", err, product, ended)
	}
	if _, ok := MergeRuling("player", "player-app", 931, t0); ok {
		t.Fatal("an ended ruling granted a merge")
	}
	if _, _, err := EndRuling(merge.ID); err == nil {
		t.Fatal("ending an ended ruling must say so")
	}
	if len(Rulings("player")) != 2 {
		t.Fatal("an ended ruling stays on record")
	}
}

func TestAddRulingRefuses(t *testing.T) {
	withFakes(t, &fakeSup{trusted: true}, exePath(t))
	for name, r := range map[string]Ruling{
		"empty":   {Text: "  "},
		"unknown": {Text: "deploy it", Allow: []string{"deploy"}},
		"over":    {Text: "merge", Until: time.Now().Add(-time.Minute)},
		// Found in review: a merge grant's reach was only ever its prose.
		"merge with no reach":   {Text: "merge ONLY #12", Allow: []string{AllowMerge}},
		"reach with no merge":   {Text: "#12 waits", PRs: []int{12}},
		"both any and numbered": {Text: "merge", Allow: []string{AllowMerge}, AnyPR: true, PRs: []int{12}},
	} {
		if _, err := AddRuling("player", r); err == nil {
			t.Errorf("%s: accepted %+v", name, r)
		}
	}
	if len(Rulings("player")) != 0 {
		t.Fatal("a refused ruling was written")
	}
}

func TestParseUntil(t *testing.T) {
	t0 := time.Date(2026, 11, 13, 18, 0, 0, 0, time.Local)
	for in, want := range map[string]time.Time{
		"":                 t0.Add(24 * time.Hour),
		"6h":               t0.Add(6 * time.Hour),
		"3d":               t0.Add(72 * time.Hour),
		"2026-11-16 09:00": time.Date(2026, 11, 16, 9, 0, 0, 0, time.Local),
		"2026-11-16":       time.Date(2026, 11, 16, 23, 59, 0, 0, time.Local),
	} {
		got, err := ParseUntil(in, t0)
		if err != nil || !got.Equal(want) {
			t.Errorf("ParseUntil(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseUntil("monday-ish", t0); err == nil || !strings.Contains(err.Error(), "duration") {
		t.Errorf("an unreadable time must say what it takes: %v", err)
	}
}

// A merge grant's reach is the pull requests it names, or any; one written
// before the field (neither) grants no merge at all.
func TestAMergeGrantReachesWhatItNames(t *testing.T) {
	t0 := time.Now()
	until := t0.Add(time.Hour)
	for _, c := range []struct {
		r     Ruling
		pr    int
		ok    bool
		reach string
	}{
		{Ruling{Allow: []string{AllowMerge}, PRs: []int{12, 14}, Until: until}, 14, true, "#12, #14"},
		{Ruling{Allow: []string{AllowMerge}, PRs: []int{12, 14}, Until: until}, 99, false, "#12, #14"},
		{Ruling{Allow: []string{AllowMerge}, AnyPR: true, Until: until}, 99, true, "any pull request"},
		{Ruling{Allow: []string{AllowMerge}, Until: until}, 12, false, "no pull request"},
	} {
		if got := c.r.Grants(AllowMerge, "app", c.pr, t0); got != c.ok {
			t.Errorf("%+v grants #%d = %v, want %v", c.r, c.pr, got, c.ok)
		}
		if got := c.r.Reach(); got != c.reach {
			t.Errorf("reach = %q, want %q", got, c.reach)
		}
	}
	for in, want := range map[string][]int{"12": {12}, "#12, 14,12": {12, 14}} {
		got, anyPR, err := ParsePRs(in)
		if err != nil || anyPR || !slices.Equal(got, want) {
			t.Errorf("ParsePRs(%q) = %v %v %v", in, got, anyPR, err)
		}
	}
	if _, anyPR, err := ParsePRs("ANY"); err != nil || !anyPR {
		t.Errorf("any: %v %v", anyPR, err)
	}
	for _, bad := range []string{"", "twelve", "12,", "-3", "0"} {
		if _, _, err := ParsePRs(bad); err == nil {
			t.Errorf("ParsePRs(%q) accepted", bad)
		}
	}
}

// Found in review: a rulings file that would not parse was read as no rulings
// and then written over with the one being added, dropping every other.
func TestAddRulingLeavesAnUnreadableFileAlone(t *testing.T) {
	withFakes(t, &fakeSup{trusted: true}, exePath(t))
	dir := ProductDir("player")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rulings.json")
	broken := []byte(`[{"id":"r1","text":"merge greens","allow":["merge"],"any_pr":true,` + "\n")
	if err := os.WriteFile(path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddRuling("player", Ruling{Text: "one deploy at a time"}); err == nil || !strings.Contains(err.Error(), "fix or move it first") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, broken) {
		t.Fatalf("the file was rewritten: %s", got)
	}
	// Readers still take it as no rulings, which grants nothing.
	if rs := Rulings("player"); rs != nil {
		t.Errorf("rulings = %+v", rs)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := AddRuling("player", Ruling{Text: "one deploy at a time"}); err != nil {
		t.Fatalf("no file at all is no rulings yet: %v", err)
	}
}
