package dispatch

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"claude-dispatcher/internal/state"
)

// stubResumeOne swaps the single resume ResumeMany runs per record. fn is what
// each call does; the stub keeps the order the records were asked for in.
func stubResumeOne(t *testing.T, fn func(d *state.Dispatch, prompt string) (ResumeMode, string, error)) {
	t.Helper()
	prev := resumeOne
	t.Cleanup(func() { resumeOne = prev })
	resumeOne = fn
}

func rec(id, feature, slug, socket, repo string) *state.Dispatch {
	return &state.Dispatch{ID: id, Feature: feature, Slug: slug, TmuxSocket: socket, RepoPath: repo}
}

func laneIDs(lanes [][]*state.Dispatch) [][]string {
	out := make([][]string, len(lanes))
	for i, l := range lanes {
		for _, d := range l {
			out[i] = append(out[i], d.ID)
		}
	}
	return out
}

func TestResumeLanesGroupByServerAndRepoTransitively(t *testing.T) {
	// a and b share a repo, b and c a server: one lane, though a and c share
	// nothing. d shares neither and goes at the same time.
	lanes := resumeLanes([]*state.Dispatch{
		rec("a", "A", "a", "s1", "/r1"),
		rec("d", "D", "d", "s4", "/r4"),
		rec("b", "B", "b", "s2", "/r1"),
		rec("c", "C", "c", "s2", "/r3"),
	})
	got := laneIDs(lanes)
	want := [][]string{{"a", "b", "c"}, {"d"}}
	if len(got) != len(want) {
		t.Fatalf("lanes = %v, want %v", got, want)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("lanes = %v, want %v", got, want)
		}
	}
}

func TestResumeLanesTheDefaultServerIsOneServer(t *testing.T) {
	// No socket is the default server, which records from before per-repo
	// servers all share; no repo path joins nothing.
	got := laneIDs(resumeLanes([]*state.Dispatch{
		rec("a", "A", "a", "", ""),
		rec("b", "B", "b", "", ""),
		rec("c", "C", "c", "own", ""),
	}))
	if len(got) != 2 || strings.Join(got[0], ",") != "a,b" || strings.Join(got[1], ",") != "c" {
		t.Fatalf("lanes = %v, want [[a b] [c]]", got)
	}
}

func TestResumeManyKeepsTheFirstRecordOfAFeature(t *testing.T) {
	var asked []string
	stubResumeOne(t, func(d *state.Dispatch, _ string) (ResumeMode, string, error) {
		asked = append(asked, d.ID)
		return ResumeStarted, "disp-" + d.Slug, nil
	})
	var got []ResumeResult
	ResumeMany([]*state.Dispatch{
		rec("new", "login", "login", "s", "/r"),
		rec("old", "login", "login", "s", "/r"),
	}, "", func(r ResumeResult) { got = append(got, r) })

	if strings.Join(asked, ",") != "new" {
		t.Fatalf("resumed %v, want only the first record of the feature", asked)
	}
	if len(got) != 2 {
		t.Fatalf("reported %d results, want one per record", len(got))
	}
	for _, r := range got {
		switch r.ID {
		case "new":
			if r.Err != nil || r.Mode != ResumeStarted || r.Session != "disp-login" {
				t.Errorf("first record: %+v", r)
			}
		case "old":
			if r.Err == nil || !strings.Contains(r.Err.Error(), "twice") {
				t.Errorf("second record of the feature was not refused by name: %+v", r)
			}
		}
	}
}

func TestResumeManyPassesThePromptAndEveryAnswer(t *testing.T) {
	boom := errors.New("tmux said no")
	stubResumeOne(t, func(d *state.Dispatch, prompt string) (ResumeMode, string, error) {
		if prompt != "continue" {
			t.Errorf("%s was asked with %q, want the batch's prompt", d.ID, prompt)
		}
		switch d.ID {
		case "live":
			return ResumeLive, "disp-live", nil
		case "bad":
			return "", "", boom
		}
		return ResumeStarted, "disp-" + d.ID, nil
	})
	by := map[string]ResumeResult{}
	ResumeMany([]*state.Dispatch{
		rec("ok", "OK", "ok", "s1", "/1"),
		rec("live", "Live", "live", "s2", "/2"),
		rec("bad", "Bad", "bad", "s3", "/3"),
		nil,
	}, "continue", func(r ResumeResult) { by[r.ID] = r })

	if len(by) != 3 {
		t.Fatalf("reported %v, want ok, live and bad (a nil record is skipped)", by)
	}
	if by["live"].Mode != ResumeLive || by["live"].Err != nil {
		t.Errorf("live: %+v", by["live"])
	}
	if !errors.Is(by["bad"].Err, boom) {
		t.Errorf("bad: %+v, want Resume's own error", by["bad"])
	}
	if by["ok"].Mode != ResumeStarted || by["ok"].Feature != "OK" {
		t.Errorf("ok: %+v", by["ok"])
	}
}

// TestResumeManyRunsLanesAtOnceAndALaneInTurn holds every resume open until
// the test lets it go, and reads how many were in flight together: two
// servers' worth at once, never two on one server.
func TestResumeManyRunsLanesAtOnceAndALaneInTurn(t *testing.T) {
	var inFlight, peak int32
	perServer := map[string]*int32{"s1": new(int32), "s2": new(int32)}
	var serverPeak int32
	release := make(chan struct{})
	started := make(chan string, 4)
	stubResumeOne(t, func(d *state.Dispatch, _ string) (ResumeMode, string, error) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		if s := atomic.AddInt32(perServer[d.TmuxSocket], 1); s > atomic.LoadInt32(&serverPeak) {
			atomic.StoreInt32(&serverPeak, s)
		}
		started <- d.ID
		<-release
		atomic.AddInt32(perServer[d.TmuxSocket], -1)
		atomic.AddInt32(&inFlight, -1)
		return ResumeStarted, d.ID, nil
	})

	var mu sync.Mutex
	var reported []string
	done := make(chan struct{})
	go func() {
		ResumeMany([]*state.Dispatch{
			rec("a1", "A1", "a1", "s1", "/a"),
			rec("a2", "A2", "a2", "s1", "/a"),
			rec("b1", "B1", "b1", "s2", "/b"),
			rec("b2", "B2", "b2", "s2", "/b"),
		}, "", func(r ResumeResult) {
			mu.Lock()
			reported = append(reported, r.ID)
			mu.Unlock()
		})
		close(done)
	}()

	// Both lanes start before either is let go: they run at the same time.
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("the two servers' lanes did not start together")
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ResumeMany did not return")
	}
	if peak := atomic.LoadInt32(&peak); peak != 2 {
		t.Errorf("peak in flight = %d, want 2 (one per server)", peak)
	}
	if serverPeak := atomic.LoadInt32(&serverPeak); serverPeak != 1 {
		t.Errorf("one server had %d resumes in flight at once, want 1", serverPeak)
	}
	if len(reported) != 4 {
		t.Errorf("reported %v, want all four", reported)
	}
}
