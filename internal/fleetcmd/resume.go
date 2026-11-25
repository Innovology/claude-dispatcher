package fleetcmd

// resume.go is `claude-dispatcher resume`: several finished dispatchers
// reopened from a shell, by the same rules the triage table's R uses
// (dispatch.ResumeMany, ADR 0036), and nothing attached.

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// resumeMany is the batch, as a seam: tests answer for it rather than start
// sessions.
var resumeMany = dispatch.ResumeMany

const resumeUsage = `usage: resume <id|feature>... [--say "message"]`

// runResume resolves every name it is given before it resumes any of them: a
// typo in the third name must not leave the first two running and the rest
// not, with the human reading the output to work out which.
func runResume(args []string, out io.Writer) error {
	keys, say, err := resumeArgs(args)
	if err != nil {
		return err
	}
	all := state.LoadAll()
	var recs []*state.Dispatch
	seen := map[string]bool{}
	var missing []string
	for _, k := range keys {
		d, err := findAny(all, k)
		if err != nil {
			missing = append(missing, err.Error())
			continue
		}
		if !seen[d.ID] {
			seen[d.ID] = true
			recs = append(recs, d)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("nothing was resumed: %s", strings.Join(missing, "; "))
	}

	var mu sync.Mutex
	failed := 0
	resumeMany(recs, say, func(r dispatch.ResumeResult) {
		mu.Lock()
		defer mu.Unlock()
		line, ok := resumeLine(r, say)
		if !ok {
			failed++
		}
		_, _ = fmt.Fprintln(out, line)
	})
	if failed > 0 {
		return fmt.Errorf("%d of %d did not resume", failed, len(recs))
	}
	return nil
}

// resumeArgs reads the names and the one flag. The flag may come anywhere —
// `resume a b --say continue` as readily as before them — which the flag
// package does not allow, since it stops at the first name.
func resumeArgs(args []string) (keys []string, say string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			keys = append(keys, args[i+1:]...)
			i = len(args)
		case a == "--say" || a == "-say":
			if i+1 >= len(args) {
				return nil, "", errors.New("--say needs the message · " + resumeUsage)
			}
			say = args[i+1]
			i++
		case strings.HasPrefix(a, "--say="):
			say = strings.TrimPrefix(a, "--say=")
		case strings.HasPrefix(a, "-") && a != "-":
			return nil, "", fmt.Errorf("unknown flag %s · %s", a, resumeUsage)
		default:
			keys = append(keys, a)
		}
	}
	if len(keys) == 0 {
		return nil, "", errors.New(resumeUsage)
	}
	return keys, strings.TrimSpace(say), nil
}

// resumeLine is one record's outcome as a line, and whether it counts as done:
// a record whose claude was still running was not resumed, but nothing went
// wrong either — it is where it was asked to be.
func resumeLine(r dispatch.ResumeResult, say string) (string, bool) {
	switch {
	case r.Err != nil:
		return quote(r.Feature) + " did not resume: " + firstLine(r.Err.Error()), false
	case r.Mode == dispatch.ResumeLive:
		line := quote(r.Feature) + " is still running · session " + r.Session
		if say != "" {
			line += " · the message was not sent to it"
		}
		return line, true
	case r.Mode == dispatch.ResumeRelaunched:
		return "started " + quote(r.Feature) + " again · it never ran · session " + r.Session, true
	}
	return "resumed " + quote(r.Feature) + " · session " + r.Session, true
}

// findAny resolves a record id, or a feature name among every record,
// finished ones included — resuming is for the finished ones, which find
// leaves out. A name that more than one record carries is refused with their
// ids, newest first, rather than guessed at: re-dispatching a shipped feature
// reuses its name, and which conversation to reopen is the human's to say.
func findAny(all []*state.Dispatch, key string) (*state.Dispatch, error) {
	for _, d := range all {
		if d.ID == key {
			return d, nil
		}
	}
	var hits []*state.Dispatch
	for _, d := range all {
		if d.Feature == key {
			hits = append(hits, d)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("no dispatcher %q — name it by its id or its feature", key)
	case 1:
		return hits[0], nil
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].UpdatedAt.After(hits[j].UpdatedAt) })
	ids := make([]string, len(hits))
	for i, d := range hits {
		ids[i] = d.ID + " (" + string(d.Status) + ")"
	}
	return nil, fmt.Errorf("%q names %d dispatchers — use an id: %s", key, len(hits), strings.Join(ids, ", "))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
