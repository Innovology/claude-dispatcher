package cockpit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/steward"
)

// A wait the steward has read leads with its note; one it has not falls back
// to the dispatcher's own ask; a note about an earlier wait says nothing.
func TestQueueSignalLeadsWithTheStewardsNote(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Minute)
	rec := &state.Dispatch{Status: state.StatusNeedsInput, WaitingSince: &earlier,
		Said: "Want me to merge it?", StewardNote: "yours: merge #71 — CI green", StewardNoteAt: &now}
	if got := cqQueueSignal(rec, "review"); got != "steward · yours: merge #71 — CI green" {
		t.Errorf("signal = %q", got)
	}
	rec.WaitingSince = &now
	rec.StewardNoteAt = &earlier
	if got := cqQueueSignal(rec, "review"); got != "Want me to merge it?" {
		t.Errorf("a stale note must not speak for a new wait: %q", got)
	}
}

// withStewards installs the stewards a load found, and fakes for the acts.
func withStewards(t *testing.T, rows ...stewardRow) *[]string {
	t.Helper()
	prevNow, prevOrder, prevQs := stewardsNow, productOrder, stewardQuestionsNow
	ps, pr, pe, pu := stewardStart, stewardReopen, stewardEnd, stewardRulings
	t.Cleanup(func() {
		stewardsNow, productOrder, stewardQuestionsNow = prevNow, prevOrder, prevQs
		stewardStart, stewardReopen, stewardEnd, stewardRulings = ps, pr, pe, pu
	})
	stewardRulings = func(p string, _ time.Time) []steward.Ruling {
		if p != "alpha" {
			return nil
		}
		return []steward.Ruling{
			{ID: "r1", Text: "one deploy at a time"},
			{ID: "r2", Text: "merge greens", Allow: []string{steward.AllowMerge}, Repo: "alpha-api", Until: time.Date(2026, 11, 16, 9, 0, 0, 0, time.Local)},
		}
	}
	stewardsNow = rows
	stewardQuestionsNow = nil
	productOrder = []string{"alpha", "beta"}
	var acts []string
	stewardStart = func(p string) (*steward.Steward, error) {
		acts = append(acts, "start "+p)
		return &steward.Steward{ID: "new", Product: p}, nil
	}
	stewardReopen = func(id string) (*steward.Steward, error) {
		acts = append(acts, "reopen "+id)
		return &steward.Steward{ID: id, Product: "alpha"}, nil
	}
	stewardEnd = func(id string) (*steward.Steward, error) {
		acts = append(acts, "end "+id)
		return &steward.Steward{ID: id, Product: "alpha"}, nil
	}
	return &acts
}

// The headline says nothing without stewards, how many are live, and the
// interrupted ones apart — those wait on the human.
func TestStewardClause(t *testing.T) {
	withStewards(t)
	if got := stewardClause(); got != "" {
		t.Errorf("none: %q", got)
	}
	stewardsNow = []stewardRow{{id: "a", product: "alpha", state: steward.Live}, {id: "o", product: "alpha", state: steward.Ended}}
	if got := stewardClause(); got != "steward live" {
		t.Errorf("one live: %q", got)
	}
	stewardsNow = append(stewardsNow, stewardRow{id: "b", product: "beta", state: steward.Live}, stewardRow{id: "c", product: "gamma", state: steward.Interrupted})
	if got := stewardClause(); got != "2 stewards live · 1 steward interrupted" {
		t.Errorf("mixed: %q", got)
	}
}

// t opens the picker on the selected row's product; each product lists its
// stewards newest first, then a "new steward" line of its own.
func TestStewardPickerLists(t *testing.T) {
	installFleetFixture(t)
	withStewards(t,
		stewardRow{id: "live1", product: "alpha", state: steward.Live, opened: time.Now()},
		stewardRow{id: "old1", product: "alpha", state: steward.Ended, opened: time.Now().Add(-48 * time.Hour)},
	)
	items := stewardItems()
	var got []string
	for _, it := range items {
		name := "new"
		if it.st != nil {
			name = it.st.id
		}
		got = append(got, it.product+":"+name)
	}
	if strings.Join(got, " ") != "alpha:live1 alpha:old1 alpha:new beta:new" {
		t.Fatalf("items = %v", got)
	}
	m := newModel()
	mm, _, _ := m.updateFloorQueue("t")
	if !mm.stewardPickOpen {
		t.Fatal("t must open the steward picker")
	}
	if it := items[mm.stewardPickCursor]; it.product != mm.stewardPickProduct() {
		t.Errorf("opened on %q, the row is %q", it.product, mm.stewardPickProduct())
	}
	view := mm.viewStewardPick(100, 30)
	for _, want := range []string{"alpha", "beta", "live1", "old1", "new steward", "live", "ended", "2 rulings standing · may merge in alpha-api until Mon 09:00"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker lacks %q:\n%s", want, view)
		}
	}
	if h := m.cqFooterHelp(); !strings.Contains(h, "t stewards") {
		t.Errorf("footer = %q", h)
	}
}

// enter reopens an older steward, starts a new one where the product has none
// live, refuses to start one over a live steward (x ends it on purpose), and x
// ends one.
func TestStewardPickerActs(t *testing.T) {
	installFleetFixture(t)
	acts := withStewards(t,
		stewardRow{id: "live1", product: "alpha", state: steward.Live},
		stewardRow{id: "old1", product: "alpha", state: steward.Ended},
	)
	m := newModel()
	m.stewardPickOpen = true
	at := func(i int) model { mm := m; mm.stewardPickCursor = i; return mm }

	mm, cmd := at(1).updateStewardPick("enter")
	if cmd == nil || mm.stewardPickOpen {
		t.Fatal("enter on an older steward must reopen it")
	}
	if msg := cmd().(stewardOpenedMsg); msg.err != nil || msg.product != "alpha" {
		t.Fatalf("reopen came back %+v", msg)
	}

	mm, cmd = at(2).updateStewardPick("enter")
	if cmd != nil || !mm.stewardPickOpen || !strings.Contains(mm.notice, "x ends it") {
		t.Fatalf("a new steward over a live one must be refused: %q", mm.notice)
	}

	_, cmd = at(3).updateStewardPick("enter")
	if cmd == nil {
		t.Fatal("enter on beta's new line must start one")
	}
	cmd()

	_, cmd = at(0).updateStewardPick("x")
	if cmd == nil {
		t.Fatal("x on a live steward must end it")
	}
	if msg := cmd().(actionMsg); !strings.Contains(msg.notice, "ended") {
		t.Errorf("end notice %q", msg.notice)
	}
	if mm, cmd = at(1).updateStewardPick("x"); cmd != nil || !strings.Contains(mm.notice, "nothing to end") {
		t.Errorf("x on an ended steward: %q", mm.notice)
	}
	if strings.Join(*acts, ",") != "reopen old1,start beta,end live1" {
		t.Fatalf("acts = %v", *acts)
	}
	if mm, _ = at(0).updateStewardPick("esc"); mm.stewardPickOpen {
		t.Error("esc must close the picker")
	}
}

func TestOnStewardOpened(t *testing.T) {
	m := newModel()
	mm, _ := m.onStewardOpened(stewardOpenedMsg{product: "alpha", err: errors.New("tmux is not available")})
	if !strings.Contains(mm.notice, "tmux is not available") {
		t.Fatalf("a failed start must say why: %q", mm.notice)
	}
	// Already live, or started untrusted, both go on to the handover; with no
	// real session here the handover reports there is nothing to attach to,
	// which is enough to show it was attempted.
	for _, err := range []error{steward.ErrRunning, steward.ErrUntrusted, nil} {
		mm, _ = m.onStewardOpened(stewardOpenedMsg{product: "alpha", err: err})
		if strings.HasPrefix(mm.notice, "steward: ") {
			t.Errorf("%v must not be reported as a failure: %q", err, mm.notice)
		}
	}
}

// Saved questions are on the headline — the line that says there is a recap to
// ask for — and in the picker, with the keyword that asks for it.
func TestSavedQuestionsShow(t *testing.T) {
	installFleetFixture(t)
	withStewards(t, stewardRow{id: "live1", product: "alpha", state: steward.Live})
	stewardQuestionsNow = map[string]int{"alpha": 2, "beta": 1}
	if got := stewardClause(); got != "steward live · 3 questions saved for you" {
		t.Errorf("clause = %q", got)
	}
	m := newModel()
	m.cfg = &config.Config{RecapKeyword: "back"}
	mm, _ := m.stewardPickOpenFor("alpha")
	if view := mm.viewStewardPick(100, 30); !strings.Contains(view, "2 questions saved for you · say back to its steward") {
		t.Errorf("picker:\n%s", view)
	}
}

// The recap keyword and the away window are settings, and a window that is
// not a duration is refused where it is typed.
func TestStewardSettingsFields(t *testing.T) {
	cfg := &config.Config{}
	field := func(key string) settingsField {
		for _, f := range settingsFields {
			if f.key == key {
				return f
			}
		}
		t.Fatalf("no settings field %q", key)
		return settingsField{}
	}
	recap, away := field("recap_keyword"), field("steward_away_after")
	if settingsValue(cfg, recap) != "recap" || settingsValue(cfg, away) != "15m" {
		t.Fatalf("defaults: %q %q", settingsValue(cfg, recap), settingsValue(cfg, away))
	}
	if settingsProblem(away, "soon") == "" || settingsProblem(away, "45m") != "" {
		t.Fatal("the away window must be a duration")
	}
	settingsCommit(cfg, recap, " back ")
	settingsCommit(cfg, away, "45m")
	if cfg.Recap() != "back" || cfg.AwayAfter() != 45*time.Minute {
		t.Fatalf("committed %q %v", cfg.Recap(), cfg.AwayAfter())
	}
}

// Whether an interrupted steward is reopened on its own is a choice on the
// settings screen: enter steps never → interrupted → never and saves each,
// without opening the text editor, since a typed "interupted" would be a
// setting that silently means never.
func TestStewardReopenIsAChoiceInSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel()
	m.width, m.height = 160, 60
	m.cfg = &config.Config{}
	m.lens = "products"
	m = press(m, ",")
	for m.settings.fields[m.settings.cursor].key != "steward_reopen" {
		if m.settings.cursor == len(m.settings.fields)-1 {
			t.Fatal("no steward_reopen field")
		}
		m = press(m, "j")
	}
	if !strings.Contains(m.View(), "never") {
		t.Error("the field does not say its default")
	}
	for _, want := range []string{"interrupted", "never"} {
		m = press(m, "enter")
		if m.settings.editing {
			t.Fatal("a choice field opened the text editor")
		}
		if got := m.cfg.StewardReopenMode(); got != want {
			t.Fatalf("enter stepped to %q, want %q", got, want)
		}
		if got, err := config.Load(); err != nil || got.StewardReopenMode() != want {
			t.Fatalf("saved %q (%v), want %q", got.StewardReopen, err, want)
		}
		if m.settings.saved != "steward reopen saved" {
			t.Errorf("flash = %q", m.settings.saved)
		}
	}
}

// A choice that cannot be saved says so rather than flashing "saved", and with
// no config there is nothing to step.
func TestStewardReopenSaveFailureIsSaid(t *testing.T) {
	home := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(home, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	m := newModel()
	m.settings = newSettings(nil)
	if mm, _ := m.cycleStewardReopen(); mm.notice != "" || mm.settings.saved != "" {
		t.Fatal("no config, yet something was said")
	}
	m.cfg = &config.Config{}
	m.settings = newSettings(m.cfg)
	m, _ = m.cycleStewardReopen()
	if !strings.HasPrefix(m.notice, "save failed: ") || m.settings.saved != "" {
		t.Fatalf("notice %q, flash %q", m.notice, m.settings.saved)
	}
}
