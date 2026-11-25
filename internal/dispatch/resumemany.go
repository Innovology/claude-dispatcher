package dispatch

// resumemany.go reopens several finished dispatchers at once (ADR 0036).
//
// ⏎ resumes one dispatcher and hands the terminal to it, which is the right
// answer for the one you want to look at and the wrong one for ten that a
// reboot took down together: ten resumes, ten jump-ins, ten ways back out.
// This is the same Resume run for each record with nothing attached, plus the
// two rules Resume cannot apply to itself, because they are about the batch:
//
//   - One record per feature. A shipped feature dispatched again reuses its
//     name, so two finished records can share a slug, and with it a worktree.
//     Resume's own guard looks for a LIVE dispatcher of the slug, and the first
//     of the two is not live yet when the second asks — its session has only
//     just been made. So the batch keeps the first and refuses the rest by
//     name, before anything starts.
//   - One session server, and one repository, at a time. Starting a session on
//     a server that is not running yet starts the server, and two clients doing
//     that at once race for it; rebuilding a reclaimed worktree writes to the
//     repository's git dir, and two of those at once can meet on its lock. So
//     records sharing either go one after the other, and records sharing
//     neither go at once: a resume is mostly waiting — on tmux, on nix develop —
//     and ten of them in a row is the slowest way to wait.

import (
	"fmt"
	"sync"

	"claude-dispatcher/internal/state"
)

// resumeOne is Resume, as a seam: tests watch the order and overlap of the
// calls rather than stand up sessions.
var resumeOne = Resume

// ResumeResult is what happened to one record of a ResumeMany.
type ResumeResult struct {
	ID      string
	Feature string
	// Mode and Session are Resume's answer, set when Err is nil: what it did,
	// and the session the dispatcher is in now.
	Mode    ResumeMode
	Session string
	Err     error
}

// ResumeMany resumes every record in recs, with prompt as the first thing said
// to each when it is not empty, and calls report once per record as it lands.
// report is never called twice at once, so it needs no lock of its own, and
// ResumeMany returns only after the last call.
//
// Nothing is attached. A record whose claude is still running comes back as
// ResumeLive with nothing started and the prompt unsent, exactly as Resume
// reports it; what to say about that is the caller's.
func ResumeMany(recs []*state.Dispatch, prompt string, report func(ResumeResult)) {
	var mu sync.Mutex
	say := func(r ResumeResult) {
		mu.Lock()
		defer mu.Unlock()
		report(r)
	}

	kept := make([]*state.Dispatch, 0, len(recs))
	first := map[string]bool{}
	for _, d := range recs {
		if d == nil {
			continue
		}
		key := d.Slug
		if key == "" {
			key = "id:" + d.ID
		}
		if first[key] {
			say(ResumeResult{ID: d.ID, Feature: d.Feature, Err: fmt.Errorf(
				"%q is in this resume twice, and one feature has one worktree — the first was resumed, this one was not",
				d.Feature)})
			continue
		}
		first[key] = true
		kept = append(kept, d)
	}

	var wg sync.WaitGroup
	for _, lane := range resumeLanes(kept) {
		wg.Add(1)
		go func(lane []*state.Dispatch) {
			defer wg.Done()
			for _, d := range lane {
				mode, session, err := resumeOne(d, prompt)
				say(ResumeResult{ID: d.ID, Feature: d.Feature, Mode: mode, Session: session, Err: err})
			}
		}(lane)
	}
	wg.Wait()
}

// resumeLanes splits recs into the groups that must go one at a time: any two
// records on the same session server, or of the same repository, land in one
// lane, transitively. Each lane keeps the order recs gave, and the lanes come
// in the order their first record did.
//
// An empty socket is a real server — the default one, which every record from
// before repos had servers of their own shares — so it is grouped like any
// other. An empty repo path names no repository and joins nothing.
func resumeLanes(recs []*state.Dispatch) [][]*state.Dispatch {
	parent := make([]int, len(recs))
	for i := range parent {
		parent[i] = i
	}
	root := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	owner := map[string]int{}
	join := func(i int, key string) {
		if j, ok := owner[key]; ok {
			a, b := root(i), root(j)
			if a < b {
				parent[b] = a
			} else {
				parent[a] = b
			}
			return
		}
		owner[key] = i
	}
	for i, d := range recs {
		join(i, "server:"+d.TmuxSocket)
		if d.RepoPath != "" {
			join(i, "repo:"+d.RepoPath)
		}
	}

	var lanes [][]*state.Dispatch
	at := map[int]int{}
	for i, d := range recs {
		r := root(i)
		n, ok := at[r]
		if !ok {
			n = len(lanes)
			at[r] = n
			lanes = append(lanes, nil)
		}
		lanes[n] = append(lanes[n], d)
	}
	return lanes
}
