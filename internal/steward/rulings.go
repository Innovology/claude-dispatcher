package steward

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"claude-dispatcher/internal/state"
)

// A Ruling is something the human decided for a product that stands for a
// while: "merge greens while I'm out", "#931 waits for my goldens", "one deploy
// at a time today". The coordinators the human ran by hand worked on exactly
// these — each one dated ("developer ruling 2026-11-10") and handed back the
// next morning — and a steward that only held them in its context lost them
// with the conversation and could not show anyone what it had been told.
//
// A ruling is written down, has an end, and is the human's: adding one is the
// verb a steward must ask permission for, so the human confirms every ruling it
// records, and the steward cannot widen its own authority. Most rulings are
// guidance the steward reads and follows. A few grant it a hand it does not
// otherwise have (Allow), and those the code enforces: `merge` refuses unless a
// ruling allows merging there.
type Ruling struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// Allow is what the ruling lets the steward do itself: "merge" is the one
	// there is. Empty is guidance only.
	Allow []string `json:"allow,omitempty"`
	// Repo narrows the ruling to one repo of the product; "" is all of them.
	Repo    string     `json:"repo,omitempty"`
	GivenAt time.Time  `json:"given_at"`
	Until   time.Time  `json:"until"`
	EndedAt *time.Time `json:"ended_at,omitempty"`
}

// AllowMerge is the one hand a ruling can grant.
const AllowMerge = "merge"

// Allows is the kinds a ruling may grant, for the verb that adds one to refuse
// anything else by name.
var Allows = []string{AllowMerge}

// DefaultRulingFor is how long a ruling stands when nobody said: long enough to
// cover being away for the evening, short enough that a forgotten "merge
// anything green" does not outlive the week it was meant for.
const DefaultRulingFor = 24 * time.Hour

// Active reports whether the ruling stands at t.
func (r Ruling) Active(t time.Time) bool { return r.EndedAt == nil && t.Before(r.Until) }

// Grants reports whether the ruling, standing at t, lets the steward do kind in
// repo.
func (r Ruling) Grants(kind, repo string, t time.Time) bool {
	return r.Active(t) && slices.Contains(r.Allow, kind) && (r.Repo == "" || r.Repo == repo)
}

func rulingsPath(product string) string { return filepath.Join(ProductDir(product), "rulings.json") }

// Rulings is every ruling ever given for product, newest first.
func Rulings(product string) []Ruling {
	raw, err := os.ReadFile(rulingsPath(product))
	if err != nil {
		return nil
	}
	var rs []Ruling
	if json.Unmarshal(raw, &rs) != nil {
		return nil
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].GivenAt.After(rs[j].GivenAt) })
	return rs
}

// ActiveRulings is the rulings standing at t, newest first.
func ActiveRulings(product string, t time.Time) []Ruling {
	var out []Ruling
	for _, r := range Rulings(product) {
		if r.Active(t) {
			out = append(out, r)
		}
	}
	return out
}

func saveRulings(product string, rs []Ruling) error {
	if err := os.MkdirAll(ProductDir(product), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	tmp := rulingsPath(product) + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, rulingsPath(product))
}

// AddRuling records a ruling for product, under the state lock.
func AddRuling(product string, r Ruling) (Ruling, error) {
	r.Text = strings.Join(strings.Fields(r.Text), " ")
	if r.Text == "" {
		return r, errors.New("say what the ruling is — its text is what the steward follows")
	}
	for _, a := range r.Allow {
		if !slices.Contains(Allows, a) {
			return r, fmt.Errorf("a ruling cannot allow %q (it can allow: %s)", a, strings.Join(Allows, ", "))
		}
	}
	t := now()
	if r.GivenAt.IsZero() {
		r.GivenAt = t
	}
	if r.Until.IsZero() {
		r.Until = r.GivenAt.Add(DefaultRulingFor)
	}
	if !r.Until.After(t) {
		return r, errors.New("that ruling would already be over")
	}
	r.ID = "r" + state.NewID()[:6]
	release := state.Lock()
	defer release()
	return r, saveRulings(product, append(Rulings(product), r))
}

// EndRuling ends a ruling early, wherever it is.
func EndRuling(id string) (product string, r Ruling, err error) {
	release := state.Lock()
	defer release()
	for _, s := range productsOnRecord() {
		rs := Rulings(s)
		for i := range rs {
			if rs[i].ID != id {
				continue
			}
			if rs[i].EndedAt != nil {
				return s, rs[i], fmt.Errorf("ruling %s has already ended", id)
			}
			t := now()
			rs[i].EndedAt = &t
			return s, rs[i], saveRulings(s, rs)
		}
	}
	return "", Ruling{}, fmt.Errorf("no ruling %q", id)
}

// MergeRuling is the ruling that lets the steward merge in repo of product at
// t, if one does.
func MergeRuling(product, repo string, t time.Time) (Ruling, bool) {
	for _, r := range ActiveRulings(product, t) {
		if r.Grants(AllowMerge, repo, t) {
			return r, true
		}
	}
	return Ruling{}, false
}

// productsOnRecord is every product that has a folder of its own — a steward,
// a ruling or a question — by the product name its records carry.
func productsOnRecord() []string {
	dirs, _ := filepath.Glob(filepath.Join(state.Dir(), "stewards", "*"))
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, s := range All() {
		add(Key(s.Product))
	}
	for _, d := range dirs {
		// A product with rulings but no steward yet is found by its folder,
		// whose name is the slug; slug(slug) is the slug, so it reads back.
		if _, err := os.Stat(filepath.Join(d, "rulings.json")); err == nil {
			add(filepath.Base(d))
		}
	}
	return out
}

// ParseUntil reads when a ruling ends: a duration from t ("6h", "3d"), a time
// ("2026-11-16T09:00", "2026-11-16 09:00", RFC 3339), or a day ("2026-11-16", the end of it).
// Empty is DefaultRulingFor.
func ParseUntil(s string, t time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return t.Add(DefaultRulingFor), nil
	}
	if strings.HasSuffix(s, "d") {
		var n int
		if _, err := fmt.Sscanf(s, "%dd", &n); err == nil && n > 0 {
			return t.Add(time.Duration(n) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return t.Add(d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02T15:04"} {
		if v, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return v, nil
		}
	}
	if v, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return v.Add(24*time.Hour - time.Minute), nil
	}
	return time.Time{}, fmt.Errorf("cannot read %q as a time — a duration (6h, 3d), a time (2026-11-16T09:00) or a day (2026-11-16)", s)
}
