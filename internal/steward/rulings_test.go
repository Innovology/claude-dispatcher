package steward

import (
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
	merge, err := AddRuling("player", Ruling{Text: "merge greens", Allow: []string{AllowMerge}, Repo: "player-app", Until: t0.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if got := ActiveRulings("player", t0); len(got) != 2 {
		t.Fatalf("active = %+v", got)
	}
	if r, ok := MergeRuling("player", "player-app", t0); !ok || r.ID != merge.ID {
		t.Fatal("a merge ruling must grant the merge")
	}
	if _, ok := MergeRuling("player", "design-system", t0); ok {
		t.Fatal("a ruling for one repo granted another")
	}
	if _, ok := MergeRuling("player", "player-app", t0.Add(2*time.Hour)); ok {
		t.Fatal("a ruling past its time granted a merge")
	}
	if _, ok := MergeRuling("tools", "player-app", t0); ok {
		t.Fatal("one product's ruling granted another's")
	}
	product, ended, err := EndRuling(merge.ID)
	if err != nil || product != "player" || ended.EndedAt == nil {
		t.Fatalf("end: %v %s %+v", err, product, ended)
	}
	if _, ok := MergeRuling("player", "player-app", t0); ok {
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
