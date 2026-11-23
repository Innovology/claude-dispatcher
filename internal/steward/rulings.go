package steward

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
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
	Repo string `json:"repo,omitempty"`
	// PRs are the pull requests a merge grant is for, and AnyPR is every one
	// in its repos while it stands. A merge grant names one or the other
	// (`--pr 12,14` or `--pr any`), because what the human confirms is the
	// command: a reach written only in the prose — "merge ONLY #12" — is not
	// one the code can hold, and a grant that read wider than its words merged
	// #99 under it. One that names neither grants no merge at all.
	PRs     []int      `json:"prs,omitempty"`
	AnyPR   bool       `json:"any_pr,omitempty"`
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

// Grants reports whether the ruling, standing at t, lets the steward do kind to
// pull request pr in repo.
func (r Ruling) Grants(kind, repo string, pr int, t time.Time) bool {
	return r.Active(t) && slices.Contains(r.Allow, kind) && (r.Repo == "" || r.Repo == repo) &&
		(r.AnyPR || slices.Contains(r.PRs, pr))
}

// Reach is which pull requests a grant is for, in words: "#12, #14", "any pull
// request", or "no pull request" for a grant that names neither.
func (r Ruling) Reach() string {
	if r.AnyPR {
		return "any pull request"
	}
	if len(r.PRs) == 0 {
		return "no pull request"
	}
	nums := make([]string, len(r.PRs))
	for i, n := range r.PRs {
		nums[i] = fmt.Sprintf("#%d", n)
	}
	return strings.Join(nums, ", ")
}

// ParsePRs reads `--pr`: "any", or pull request numbers separated by commas,
// with or without their #.
func ParsePRs(s string) (prs []int, anyPR bool, err error) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "any") {
		return nil, true, nil
	}
	for _, f := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(f), "#"))
		if err != nil || n <= 0 {
			return nil, false, fmt.Errorf("--pr %q: pull request numbers (12,14) or any", s)
		}
		if !slices.Contains(prs, n) {
			prs = append(prs, n)
		}
	}
	return prs, false, nil
}

// Rulings is every ruling ever given for product, newest first.
func Rulings(product string) []Ruling { return rulingsIn(ProductDir(product)) }

// rulingsIn reads the rulings in a product's folder. The store is addressed by
// folder rather than by product name wherever it is walked, because a folder's
// name is a product's slug and a slug is not always its own slug again.
func rulingsIn(dir string) []Ruling {
	rs, _ := readRulings(dir)
	return rs
}

// readRulings is rulingsIn saying when the file is there and cannot be read,
// which a reader can take as no rulings and a writer must not: writing over it
// would replace every ruling in it with the one being added.
func readRulings(dir string) ([]Ruling, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "rulings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rs []Ruling
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, err
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].GivenAt.After(rs[j].GivenAt) })
	return rs, nil
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

func saveRulingsIn(dir string, rs []Ruling) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "rulings.json")
	if err := os.WriteFile(path+".tmp", append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
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
	scoped := r.AnyPR || len(r.PRs) > 0
	switch {
	case slices.Contains(r.Allow, AllowMerge) && !scoped:
		return r, errors.New("a merge ruling names the pull requests it is for: --pr 12,14, or --pr any for every one in its repos while it stands")
	case !slices.Contains(r.Allow, AllowMerge) && scoped:
		return r, errors.New("--pr scopes a merge grant, and this ruling grants none (--allow merge)")
	case r.AnyPR && len(r.PRs) > 0:
		return r, errors.New("--pr is either pull request numbers or any, not both")
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
	dir := ProductDir(product)
	rs, err := readRulings(dir)
	if err != nil {
		return r, fmt.Errorf("%s cannot be read (%v): fix or move it first, since recording over it would drop every ruling in it", filepath.Join(dir, "rulings.json"), err)
	}
	return r, saveRulingsIn(dir, append(rs, r))
}

// EndRuling ends a ruling early, wherever it is, and says which product's
// folder it was in.
func EndRuling(id string) (product string, r Ruling, err error) {
	release := state.Lock()
	defer release()
	for _, dir := range productDirs() {
		rs := rulingsIn(dir)
		for i := range rs {
			if rs[i].ID != id {
				continue
			}
			if rs[i].EndedAt != nil {
				return filepath.Base(dir), rs[i], fmt.Errorf("ruling %s has already ended", id)
			}
			t := now()
			rs[i].EndedAt = &t
			return filepath.Base(dir), rs[i], saveRulingsIn(dir, rs)
		}
	}
	return "", Ruling{}, fmt.Errorf("no ruling %q", id)
}

// MergeRuling is the ruling that lets the steward merge pull request pr in repo
// of product at t, if one does.
func MergeRuling(product, repo string, pr int, t time.Time) (Ruling, bool) {
	for _, r := range ActiveRulings(product, t) {
		if r.Grants(AllowMerge, repo, pr, t) {
			return r, true
		}
	}
	return Ruling{}, false
}

// productDirs is every product's folder.
func productDirs() []string {
	entries, _ := filepath.Glob(filepath.Join(state.Dir(), "stewards", "*"))
	var out []string
	for _, e := range entries {
		if fi, err := os.Stat(e); err == nil && fi.IsDir() {
			out = append(out, e)
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
