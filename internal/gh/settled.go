package gh

import (
	"encoding/json"
	"maps"
	"os"
	"sync"
	"time"
)

// settled.go keeps the answers that have stopped changing across launches.
//
// SettledTTL is for what cannot move — a merged pull request's checks and
// reviews, the PR of a branch nobody will push to again — and the cache held
// it for half an hour, in memory. So every launch asked again: measured on a
// cockpit opened several times in an hour, one finished dispatcher's merged PR
// cost a `pr checks` and a `pr view` in series on every start, a second and a
// half before the triage table could finish, for an answer that had not
// changed since the merge. Those answers are written to a file in the state
// dir and read back on the next launch, for what is left of the same half
// hour — nothing is held longer than it was, it merely survives a restart.
// Nothing with a shorter TTL is kept: those are the reads that are supposed to
// be asked again.

type diskEntry struct {
	At  time.Time       `json:"at"`
	Val json.RawMessage `json:"val"`
}

var (
	diskPath string                   // "" until KeepSettled names one; guarded by mu
	disk     = map[string]diskEntry{} // guarded by mu
	writeMu  sync.Mutex               // one writer of the file at a time
)

// KeepSettled names the file settled answers are kept in, and reads back what
// an earlier launch left there that is still within SettledTTL.
func KeepSettled(path string) {
	loaded := map[string]diskEntry{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &loaded)
	}
	mu.Lock()
	diskPath, disk = path, map[string]diskEntry{}
	for k, e := range loaded {
		if time.Since(e.At) < SettledTTL {
			disk[k] = e
		}
	}
	mu.Unlock()
}

// fromDisk is a settled answer an earlier launch kept, for a read that is
// itself settled. Called with mu held.
func fromDisk[T any](key string, ttl time.Duration) (T, bool) {
	var v T
	if ttl < SettledTTL {
		return v, false
	}
	e, ok := disk[key]
	if !ok || time.Since(e.At) >= ttl || json.Unmarshal(e.Val, &v) != nil {
		return v, false
	}
	return v, true
}

// toDisk records a settled answer to be written, reporting whether it did.
// Called with mu held; the write itself happens after (persistSettled).
func toDisk(key string, ttl time.Duration, val any) bool {
	if ttl < SettledTTL || diskPath == "" {
		return false
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return false
	}
	disk[key] = diskEntry{At: time.Now(), Val: raw}
	return true
}

// persistSettled writes the kept answers, dropping any that have expired.
// Written whole and renamed into place, so a reader never sees half a file.
func persistSettled() {
	writeMu.Lock()
	defer writeMu.Unlock()
	mu.Lock()
	path := diskPath
	keep := maps.Clone(disk)
	mu.Unlock()
	if path == "" {
		return
	}
	for k, e := range keep {
		if time.Since(e.At) >= SettledTTL {
			delete(keep, k)
		}
	}
	raw, err := json.Marshal(keep)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// forgetSettled drops the kept answers with the rest of the cache, file and
// all: InvalidateCache is the human having just changed something on the
// forge, and a settled answer read back from disk would undo that.
func forgetSettled() {
	writeMu.Lock()
	defer writeMu.Unlock()
	mu.Lock()
	path := diskPath
	disk = map[string]diskEntry{}
	mu.Unlock()
	if path != "" {
		_ = os.Remove(path)
	}
}
