// Package usage measures Claude subscription consumption and learns the
// subscription's limits empirically — there is no API that exposes them.
//
// The subscription enforces a 5-hour rolling window and a weekly quota. We
// measure both from the per-message timestamps and token counts in the recent
// session transcripts under ~/.claude/projects (Claude Code's own stats cache
// is not used — it can lag by weeks). Cache-read tokens are excluded: they are
// the bulk of raw tokens but count for almost nothing against the limit, so
// including them would swamp the gauge. We count input + output + cache-creation.
//
// The limit itself is learned. A hit is a 429 rate_limit line in a transcript
// (isApiErrorMessage + apiErrorStatus 429; 529 overloaded is NOT a limit). The
// transcript does not say which window a 429 belongs to or when it resets, so:
//   - when a 429 fires, the usage in a window at that moment is taken as that
//     window's cap ("assume that's the limit");
//   - when usage later exceeds a learned cap without a 429, the cap ratchets up
//     ("we went over and it was fine — the limit is higher than we thought").
//
// The learned caps persist in the state dir so the estimate improves over time.
//
// Everything is best-effort: missing files or parse errors degrade to zero /
// "still learning", never an error that disturbs the cockpit.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Window durations.
const (
	fiveHour = 5 * time.Hour
	week     = 7 * 24 * time.Hour
)

// Stat is one window's consumption and its learned cap.
type Stat struct {
	Total     int            // tokens consumed in the window
	ByModel   map[string]int // model family → tokens (see bucket: Families, else the id)
	Sessions  int            // distinct sessions contributing
	Cap       int            // learned cap; 0 = still learning
	CapSource string         // "limit" (from a 429) | "observed" (ratcheted high-water) | ""
	HighWater int            // most tokens ever seen in this window (provisional gauge)
}

// Denom is the best figure to measure against: the learned cap, or the
// high-water mark while still learning. 0 only before any usage at all.
func (s Stat) Denom() int {
	if s.Cap > 0 {
		return s.Cap
	}
	return s.HighWater
}

// Pct returns consumption as a whole-number percentage of the learned cap, or
// -1 when the cap is unknown (caller shows raw tokens instead).
func (s Stat) Pct() int {
	if s.Cap <= 0 {
		return -1
	}
	p := 100 * s.Total / s.Cap
	if p > 100 {
		p = 100
	}
	return p
}

// Summary is the current usage picture.
type Summary struct {
	FiveHour  Stat
	Weekly    Stat
	LastLimit time.Time // most recent 429 seen, zero if none
}

// store is the persisted learned state.
type store struct {
	FiveHourCap    int    `json:"five_hour_cap"`
	FiveHourSource string `json:"five_hour_cap_source"`
	FiveHourHW     int    `json:"five_hour_high_water"`
	WeeklyCap      int    `json:"weekly_cap"`
	WeeklySource   string `json:"weekly_cap_source"`
	WeeklyHW       int    `json:"weekly_high_water"`
	LastLimitUnix  int64  `json:"last_limit_unix"`
}

// Families are the model families we know by name, most expensive first. The
// cockpit renders them in this order; anything else keeps its own id (see
// bucket), so a new model never disappears into an unlabelled "other" row.
var Families = []string{"fable", "mythos", "opus", "sonnet", "haiku"}

// bucket maps a raw model id to its family. An id from no known family is NOT
// folded into a catch-all: it keeps its own name (minus the "claude-" prefix
// and any date suffix), because "other models" tells you nothing when a whole
// new tier — Fable 5 was 40% of a week — lands in it.
func bucket(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	for _, f := range Families {
		if strings.Contains(m, f) {
			return f
		}
	}
	return trimModelID(m)
}

// trimModelID shortens a raw id for display: "claude-gizmo-6-20260401" → "gizmo-6".
func trimModelID(m string) string {
	if m == "" {
		return "unknown"
	}
	m = strings.TrimPrefix(m, "claude-")
	// Drop a trailing 8-digit date stamp ("-20251001"), which is noise in a label.
	if i := strings.LastIndex(m, "-"); i > 0 && len(m)-i == 9 && isDigits(m[i+1:]) {
		m = m[:i]
	}
	return m
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func claudeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// Compute reads consumption, folds in any new limit hits, updates the learned
// caps, persists them under stateDir, and returns the current summary. `now` is
// passed in for testability.
func Compute(stateDir string, now time.Time) Summary {
	st := load(stateDir)

	// Both windows + recent limit hits, from the session transcripts.
	w := scanWindows(now)
	fhTotal, fhByModel, fhSessions := w.fhTotal, w.fhByModel, w.fhSessions
	wkTotal, wkByModel, wkSessions := w.wkTotal, w.wkByModel, w.wkSessions
	limits := w.limits

	// Ratchet the high-water marks.
	st.FiveHourHW = max(st.FiveHourHW, fhTotal)
	st.WeeklyHW = max(st.WeeklyHW, wkTotal)

	// Fold in limit hits newer than the last one we processed.
	newest := st.LastLimitUnix
	limitInWindow := false
	for _, ev := range limits {
		if ev.Unix() <= st.LastLimitUnix {
			continue
		}
		if ev.Unix() > newest {
			newest = ev.Unix()
		}
		if now.Sub(ev) <= fiveHour {
			limitInWindow = true
		}
		// Attribution heuristic: if the 5-hour window is well below an already
		// known 5-hour cap, the wall we hit must be the weekly quota; otherwise
		// treat it as the 5-hour cap. With no prior knowledge, default 5-hour.
		if st.FiveHourCap > 0 && fhTotal < st.FiveHourCap*8/10 {
			st.WeeklyCap, st.WeeklySource = wkTotal, "limit"
		} else {
			st.FiveHourCap, st.FiveHourSource = fhTotal, "limit"
		}
	}
	st.LastLimitUnix = newest

	// Ratchet caps up when we sail past them without being stopped — but not
	// while a hit is active in the current 5-hour window (we're capped now).
	if !limitInWindow && fhTotal > st.FiveHourCap {
		st.FiveHourCap, st.FiveHourSource = fhTotal, "observed"
	}
	if wkTotal > st.WeeklyCap {
		st.WeeklyCap, st.WeeklySource = wkTotal, "observed"
	}

	save(stateDir, st)

	sum := Summary{
		FiveHour: Stat{Total: fhTotal, ByModel: fhByModel, Sessions: fhSessions, Cap: st.FiveHourCap, CapSource: st.FiveHourSource, HighWater: st.FiveHourHW},
		Weekly:   Stat{Total: wkTotal, ByModel: wkByModel, Sessions: wkSessions, Cap: st.WeeklyCap, CapSource: st.WeeklySource, HighWater: st.WeeklyHW},
	}
	if newest > 0 {
		sum.LastLimit = time.Unix(newest, 0)
	}
	return sum
}

// ---- consumption source: the session transcripts ---------------------------

type tLine struct {
	Type           string `json:"type"`
	Timestamp      string `json:"timestamp"`
	IsAPIError     bool   `json:"isApiErrorMessage"`
	APIErrorStatus int    `json:"apiErrorStatus"`
	Error          string `json:"error"`
	SessionID      string `json:"session_id"`
	Message        struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// windows holds both rolling windows' consumption plus the limit hits seen.
type windows struct {
	fhTotal, fhSessions int
	fhByModel           map[string]int
	wkTotal, wkSessions int
	wkByModel           map[string]int
	limits              []time.Time
}

// scanWindows walks transcripts modified within the last ~8 days, summing
// countable tokens (input + output + cache-creation; cache-read excluded) into
// the trailing 5-hour and 7-day windows, and collecting 429 rate-limit
// timestamps (529 overloads are ignored — they are not usage limits).
func scanWindows(now time.Time) windows {
	w := windows{fhByModel: map[string]int{}, wkByModel: map[string]int{}}
	fhSess := map[string]bool{}
	wkSess := map[string]bool{}
	dir := claudeDir()
	if dir == "" {
		return w
	}
	projects := filepath.Join(dir, "projects")
	fhCut := now.Add(-fiveHour)
	wkCut := now.Add(-week)
	fileCut := now.Add(-week - 24*time.Hour) // slack for file mtime vs line time

	_ = filepath.WalkDir(projects, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(fileCut) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			row := strings.TrimSpace(sc.Text())
			if row == "" || row[0] != '{' {
				continue
			}
			var l tLine
			if json.Unmarshal([]byte(row), &l) != nil {
				continue
			}
			ts, err := time.Parse(time.RFC3339, l.Timestamp)
			if err != nil {
				continue
			}
			ts = ts.Local()
			// Limit hit: a real 429 rate limit (not a 529 overload).
			if l.IsAPIError && l.APIErrorStatus == 429 && strings.Contains(l.Error, "rate_limit") {
				w.limits = append(w.limits, ts)
				continue
			}
			if l.Type != "assistant" || ts.Before(wkCut) {
				continue
			}
			u := l.Message.Usage
			tok := u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens
			if tok <= 0 {
				continue
			}
			model := bucket(l.Message.Model)
			w.wkTotal += tok
			w.wkByModel[model] += tok
			if l.SessionID != "" {
				wkSess[l.SessionID] = true
			}
			if !ts.Before(fhCut) {
				w.fhTotal += tok
				w.fhByModel[model] += tok
				if l.SessionID != "" {
					fhSess[l.SessionID] = true
				}
			}
		}
		return nil
	})
	w.fhSessions = len(fhSess)
	w.wkSessions = len(wkSess)
	return w
}

// ---- persistence ------------------------------------------------------------

func storePath(stateDir string) string { return filepath.Join(stateDir, "usage-limits.json") }

func load(stateDir string) store {
	var s store
	if b, err := os.ReadFile(storePath(stateDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func save(stateDir string, s store) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(stateDir, 0o755)
	tmp := storePath(stateDir) + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, storePath(stateDir))
	}
}
