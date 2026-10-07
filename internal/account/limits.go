package account

// limits.go is how much of a subscription's 5-hour and weekly limits is left.
//
// Claude Code knows — it is enforcing them — and it says so in exactly one
// place outside itself: the JSON its status line command is handed, which
// carries `rate_limits.five_hour` and `rate_limits.seven_day`, each a
// `used_percentage` (0–100) and a `resets_at` (epoch seconds). Measured
// against claude 2.1.281 on a Max login: {"five_hour":{"used_percentage":5,
// "resets_at":1791367800},"seven_day":{"used_percentage":3,...}}. It is there
// only for a subscription, only after the session's first API response, and
// either window may be missing.
//
// So `claude-dispatcher statusline` is installed as each account's status
// line (wrapping one the human already had), and every time a session of that
// account draws it the reading is written here, under the config directory it
// came from. The cockpit reads it back beside the account on the dispatch
// forms. The figure is the subscription's, not the session's, so a reading
// from any session — a dispatcher, the steward, the human's own terminal —
// counts for the account as a whole.
//
// A reading is a fact about the moment it was taken and is shown with its age.
// The one inference made from it is the window's own: once resets_at has
// passed, the window has emptied, whatever it said before. Nothing is guessed
// beyond that; an account no session has drawn a status line for has no
// figure, and says so.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Window is one limit as the status line reported it.
type Window struct {
	UsedPct  float64 `json:"used_percentage"`
	ResetsAt int64   `json:"resets_at"` // epoch seconds
}

// Left is the percentage of w still available at now, and whether the window
// has reset since the reading — in which case all of it is.
func (w Window) Left(now time.Time) (left int, reset bool) {
	if w.ResetsAt > 0 && !now.Before(time.Unix(w.ResetsAt, 0)) {
		return 100, true
	}
	used := int(math.Round(w.UsedPct))
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	return 100 - used, false
}

// Limits is the latest reading for one config directory.
type Limits struct {
	ConfigDir string    `json:"config_dir"`
	FiveHour  *Window   `json:"five_hour,omitempty"`
	SevenDay  *Window   `json:"seven_day,omitempty"`
	At        time.Time `json:"at"`
}

// limitsPath is where configDir's reading is kept: one file per directory,
// named by a hash of the cleaned path so any path makes a file name.
func limitsPath(stateDir, configDir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(configDir)))
	return filepath.Join(stateDir, "accounts", hex.EncodeToString(sum[:])[:16]+".json")
}

// ReadLimits is the latest reading for configDir, or false when no session of
// it has drawn a status line since this was installed.
func ReadLimits(stateDir, configDir string) (Limits, bool) {
	raw, err := os.ReadFile(limitsPath(stateDir, configDir))
	if err != nil {
		return Limits{}, false
	}
	var l Limits
	if json.Unmarshal(raw, &l) != nil || (l.FiveHour == nil && l.SevenDay == nil) {
		return Limits{}, false
	}
	return l, true
}

// rewriteAfter is how stale an unchanged reading may get before it is written
// again just to move its timestamp. The status line redraws many times a
// minute; the age shown beside a figure does not need that resolution.
const rewriteAfter = time.Minute

// RecordStatusLine takes the JSON a status line command was handed and keeps
// its rate limits as configDir's latest reading. A payload without them —
// before the session's first response, or an API-key login that has none — is
// not a reading and leaves the last one in place.
func RecordStatusLine(stateDir, configDir string, payload []byte, now time.Time) error {
	var in struct {
		RateLimits struct {
			FiveHour *Window `json:"five_hour"`
			SevenDay *Window `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return err
	}
	if in.RateLimits.FiveHour == nil && in.RateLimits.SevenDay == nil {
		return nil
	}
	next := Limits{ConfigDir: filepath.Clean(configDir), FiveHour: in.RateLimits.FiveHour,
		SevenDay: in.RateLimits.SevenDay, At: now}
	if prev, ok := ReadLimits(stateDir, configDir); ok &&
		sameWindow(prev.FiveHour, next.FiveHour) && sameWindow(prev.SevenDay, next.SevenDay) &&
		now.Sub(prev.At) < rewriteAfter {
		return nil
	}
	out, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	path := limitsPath(stateDir, configDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Written whole and renamed into place: several sessions of one account
	// draw their status lines at once, and a reader must never see half a file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".limits-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func sameWindow(a, b *Window) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Brief is a reading as one short phrase — "5h 77% · wk 59% left" — or ""
// when there is none. A window that has reset since the reading reads as
// full: that is the window's own rule, not a guess.
func (l Limits) Brief(now time.Time) string {
	part := func(label string, w *Window) string {
		if w == nil {
			return label + " —"
		}
		left, _ := w.Left(now)
		return label + " " + strconv.Itoa(left) + "%"
	}
	if l.FiveHour == nil && l.SevenDay == nil {
		return ""
	}
	return part("5h", l.FiveHour) + " · " + part("wk", l.SevenDay) + " left"
}

// Least is the smaller of the two windows' percentage left — the one that
// will stop a session first — or -1 with no reading.
func (l Limits) Least(now time.Time) int {
	least := -1
	for _, w := range []*Window{l.FiveHour, l.SevenDay} {
		if w == nil {
			continue
		}
		if left, _ := w.Left(now); least < 0 || left < least {
			least = left
		}
	}
	return least
}
