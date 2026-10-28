package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// scan.go reads the transcripts the windows are summed from — once.
//
// It used to re-read every transcript modified in the last eight days on every
// load, and the cockpit loads on every poll and every dispatch-record write.
// Measured on a working machine that was 117 files and 611 MB, decoded line by
// line on one core: 2.5 seconds each time, for an answer that had changed by a
// few lines since the last one. Transcripts are append-only, so each file is
// remembered — what it has said so far, reduced to the few facts the windows
// need, and how far into the file that reading reached — and a later load
// reads only the bytes appended since. The first read still has to see
// everything, so it is spread across the cores.
//
// Most of those bytes cannot matter: tool output, file contents, the prompt.
// A line that counts is an assistant message carrying a "usage" object, or an
// API error carrying "isApiErrorMessage". Inside a JSON string every quote is
// escaped, so those two keys appear literally — `"usage"` with its quotes —
// only where they are keys, and a line holding neither is skipped before it is
// decoded. On that machine it was 23% of the bytes.

// use is one line that counts, as the windows need it.
type use struct {
	at      time.Time
	model   string // the family bucket
	tok     int
	session string
	limit   bool // a 429 rate_limit, not usage
}

// fileScan is one transcript as far as it has been read.
type fileScan struct {
	size   int64
	mod    time.Time
	offset int64 // bytes consumed, always at a line boundary
	uses   []use
}

var (
	scanMu sync.Mutex
	scans  = map[string]*fileScan{}
)

var (
	keyUsage = []byte(`"usage"`)
	keyError = []byte(`"isApiErrorMessage"`)
)

// scanWindows walks transcripts modified within the last ~8 days, summing
// countable tokens (input + output + cache-creation; cache-read excluded) into
// the trailing 5-hour and 7-day windows, and collecting 429 rate-limit
// timestamps (529 overloads are ignored — they are not usage limits).
func scanWindows(now time.Time) windows {
	w := windows{fhByModel: map[string]int{}, wkByModel: map[string]int{}}
	dir := claudeDir()
	if dir == "" {
		return w
	}
	fileCut := now.Add(-week - 24*time.Hour) // slack for file mtime vs line time

	type candidate struct {
		path string
		size int64
		mod  time.Time
	}
	var files []candidate
	_ = filepath.WalkDir(filepath.Join(dir, "projects"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(fileCut) {
			return nil
		}
		files = append(files, candidate{path, info.Size(), info.ModTime()})
		return nil
	})

	// Bring every file up to date, at most one per core at a time.
	results := make([]*fileScan, len(files))
	sem := make(chan struct{}, max(runtime.NumCPU(), 1))
	var wg sync.WaitGroup
	for i, f := range files {
		scanMu.Lock()
		prev := scans[f.path]
		scanMu.Unlock()
		wg.Add(1)
		go func(i int, f candidate) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = refreshScan(f.path, f.size, f.mod, prev, now)
		}(i, f)
	}
	wg.Wait()

	// Keep what was read; forget files that have aged out or gone.
	scanMu.Lock()
	next := make(map[string]*fileScan, len(files))
	for i, f := range files {
		if results[i] != nil {
			next[f.path] = results[i]
		}
	}
	scans = next
	scanMu.Unlock()

	fhCut, wkCut := now.Add(-fiveHour), now.Add(-week)
	fhSess, wkSess := map[string]bool{}, map[string]bool{}
	for _, fs := range results {
		if fs == nil {
			continue
		}
		for _, u := range fs.uses {
			if u.limit {
				w.limits = append(w.limits, u.at)
				continue
			}
			if u.at.Before(wkCut) {
				continue
			}
			w.wkTotal += u.tok
			w.wkByModel[u.model] += u.tok
			if u.session != "" {
				wkSess[u.session] = true
			}
			if !u.at.Before(fhCut) {
				w.fhTotal += u.tok
				w.fhByModel[u.model] += u.tok
				if u.session != "" {
					fhSess[u.session] = true
				}
			}
		}
	}
	w.fhSessions, w.wkSessions = len(fhSess), len(wkSess)
	return w
}

// refreshScan brings one file's reading up to date: unchanged, it is returned
// as it was; grown, only the appended bytes are read; anything else — shrunk,
// replaced, never seen — is read from the start. A copy is returned rather
// than prev changed, because prev is shared with whatever load ran before.
// Uses older than the week are dropped as they are read: time only moves
// forward, so they can never count again.
func refreshScan(path string, size int64, mod time.Time, prev *fileScan, now time.Time) *fileScan {
	if prev != nil && prev.size == size && prev.mod.Equal(mod) {
		return prev
	}
	fs := &fileScan{size: size, mod: mod}
	if prev != nil && size >= prev.size {
		fs.offset = prev.offset
		fs.uses = append([]use(nil), prev.uses...)
	}
	f, err := os.Open(path)
	if err != nil {
		return prev
	}
	defer func() { _ = f.Close() }()
	if fs.offset > 0 {
		if _, err := f.Seek(fs.offset, io.SeekStart); err != nil {
			fs.offset, fs.uses = 0, nil
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return prev
			}
		}
	}
	wkCut := now.Add(-week)
	rd := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			// Only a whole line is consumed: the last one may still be being
			// written, and is read next time, complete.
			fs.offset += int64(len(line))
			if u, ok := parseUse(line); ok && (u.limit || !u.at.Before(wkCut)) {
				fs.uses = append(fs.uses, u)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return prev
			}
			break
		}
	}
	return fs
}

// parseUse reads one transcript line, reporting whether it counts.
func parseUse(line []byte) (use, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return use{}, false
	}
	if !bytes.Contains(line, keyUsage) && !bytes.Contains(line, keyError) {
		return use{}, false
	}
	var l tLine
	if json.Unmarshal(line, &l) != nil {
		return use{}, false
	}
	ts, err := time.Parse(time.RFC3339, l.Timestamp)
	if err != nil {
		return use{}, false
	}
	ts = ts.Local()
	// Limit hit: a real 429 rate limit (not a 529 overload).
	if l.IsAPIError && l.APIErrorStatus == 429 && strings.Contains(l.Error, "rate_limit") {
		return use{at: ts, limit: true}, true
	}
	if l.Type != "assistant" {
		return use{}, false
	}
	u := l.Message.Usage
	tok := u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens
	if tok <= 0 {
		return use{}, false
	}
	return use{at: ts, model: bucket(l.Message.Model), tok: tok, session: l.SessionID}, true
}
