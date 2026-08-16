package gh

// live_test.go stands a fake `gh` on PATH and counts what the package asks it
// for. The point of these tests is the request count, not the parse: this
// package's job is to answer the cockpit without spending the hourly quota.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeGH puts a `gh` on PATH that appends its argv to a log file and replies
// from the script it is given. Returns a func reading the calls made so far.
//
// The script is a shell case body: each entry matches on the joined argv.
func fakeGH(t *testing.T, body string) func() []string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + log + "\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// A clean slate is the harness's job: a park left behind by an earlier test
	// would make the next one pass by asking nothing at all.
	InvalidateCache()
	clearThrottle()
	t.Cleanup(func() { InvalidateCache(); clearThrottle() })
	return func() []string {
		b, err := os.ReadFile(log)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
}

// perPR drops the batched repo read from a call list, leaving the per-pull-
// request work the batch is meant to make unnecessary.
func perPR(calls []string) []string {
	var out []string
	for _, c := range calls {
		if !strings.HasPrefix(c, "pr list --state open") {
			out = append(out, c)
		}
	}
	return out
}

// `gh pr checks` reports its answer in the exit code as well as on stdout: 1
// when a check has failed, 8 while any is still pending. Those are exactly the
// two states worth polling, so treating the exit code as "no answer" and
// re-asking the rollup meant every running or red PR cost two GraphQL requests
// instead of one — on the largest key class the cache has, on every refresh.
func TestPRChecksReadsStdoutDespiteTheExitCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
		want Checks
	}{
		{"pending", "8", Checks{Total: 2, Passed: 1, Running: 1}},
		{"failing", "1", Checks{Total: 2, Passed: 1, Failing: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := "IN_PROGRESS"
			if tc.name == "failing" {
				state = "FAILURE"
			}
			calls := fakeGH(t, `case "$*" in
  *"pr checks"*) echo '[{"state":"SUCCESS"},{"state":"`+state+`"}]'; exit `+tc.code+` ;;
  *) echo '[]' ;;
esac`)

			if got := PRChecksFor(t.TempDir(), 7); got != tc.want {
				t.Errorf("checks = %+v, want %+v", got, tc.want)
			}
			got := perPR(calls())
			if len(got) != 1 {
				t.Fatalf("wanted one per-PR request, got %d: %v", len(got), got)
			}
			if strings.Contains(got[0], "pr view") {
				t.Errorf("fell back to the rollup with a usable answer in hand: %q", got[0])
			}
		})
	}
}

// The fallback is still there for the case it was written for: gh exits
// non-zero and prints nothing, because the PR has no checks at all. The rollup
// is populated before `pr checks` can answer, so that read is worth making.
func TestPRChecksFallsBackWhenThereIsNoAnswer(t *testing.T) {
	calls := fakeGH(t, `case "$*" in
  *"pr checks"*) echo "no checks reported" >&2; exit 1 ;;
  *"pr view"*) echo '{"statusCheckRollup":[{"state":"SUCCESS"}]}' ;;
esac`)

	if got := (PRChecksFor(t.TempDir(), 7)); got != (Checks{Total: 1, Passed: 1}) {
		t.Errorf("checks = %+v, want the rollup's answer", got)
	}
	if got := perPR(calls()); len(got) != 2 || !strings.Contains(got[1], "pr view") {
		t.Errorf("wanted a rollup fallback, got %v", got)
	}
}

// track.Refresh asks for every dispatch that is not done, on every poll, and a
// long-lived state dir is mostly exited records that will never reach done. An
// uncached read there was a per-poll tax that only grew with history.
func TestPRForBranchIsCached(t *testing.T) {
	calls := fakeGH(t, `echo '[{"number":3,"state":"OPEN","url":"u"}]'`)

	repo := t.TempDir()
	for i := 0; i < 3; i++ {
		if pr := PRForBranch(repo, "feature/x", false); pr == nil || pr.Number != 3 {
			t.Fatalf("PRForBranch = %+v, want #3", pr)
		}
	}
	if got := calls(); len(got) != 1 {
		t.Errorf("asked the forge %d times for one answer: %v", len(got), got)
	}
}

// The tracker and the product lens both want the deploy workflow's runs on the
// same poll. Two reads of one list is a wasted request and the only way the two
// could ever disagree about whether a feature went live.
func TestDeploySignalAndStatusShareOneRunList(t *testing.T) {
	calls := fakeGH(t, `case "$*" in
  *"workflow list"*) echo '[{"name":"Deploy production"}]' ;;
  *"run list"*) echo '[{"conclusion":"success","status":"completed","createdAt":"2026-08-16T10:00:00Z"}]' ;;
esac`)

	repo := t.TempDir()
	DeploySignal(repo, time.Time{}, "")
	DeployStatus(repo, "")

	var runLists int
	for _, c := range calls() {
		if strings.Contains(c, "run list") {
			runLists++
		}
	}
	if runLists != 1 {
		t.Errorf("listed the deploy runs %d times, want 1: %v", runLists, calls())
	}
}

// The whole point of the batch: a repo's open PRs arrive with their check
// rollups and review posture attached, so asking about ten of them costs one
// request rather than twenty. Per-PR reads used to be the largest line in a
// bill that came to 5,409 GraphQL requests an hour against a limit of 5,000.
func TestOpenPRSignalsCostOneRequestPerRepo(t *testing.T) {
	var prs []string
	for n := 1; n <= 10; n++ {
		prs = append(prs, `{"number":`+strconv.Itoa(n)+`,"headRefName":"feature/x",`+
			`"reviewDecision":"APPROVED","additions":3,"deletions":1,`+
			`"reviews":[{"author":{"login":"a"},"state":"APPROVED","submittedAt":"2026-08-16T10:00:00Z"}],`+
			`"statusCheckRollup":[{"state":"SUCCESS"},{"state":"PENDING"}]}`)
	}
	calls := fakeGH(t, `case "$*" in
  *"pr list --state open"*) echo '[`+strings.Join(prs, ",")+`]' ;;
  *) echo '[]' ;;
esac`)

	repo := t.TempDir()
	for n := 1; n <= 10; n++ {
		if got := PRChecksFor(repo, n); got != (Checks{Total: 2, Passed: 1, Running: 1}) {
			t.Fatalf("PR %d checks = %+v", n, got)
		}
		if got := PRReviewFor(repo, n); got.Approvals != 1 || got.Decision != "APPROVED" {
			t.Fatalf("PR %d review = %+v", n, got)
		}
	}
	if got := perPR(calls()); len(got) != 0 {
		t.Errorf("asked about individual PRs the batch already answered: %v", got)
	}
	if got := calls(); len(got) != 1 {
		t.Errorf("wanted one repo read for twenty signals, got %d: %v", len(got), got)
	}
}

// The head branch and the diff size come back filled in now. They did not
// before: the products lens took its open PRs from a search, which does not
// return either, so the review queue sized every pull request at zero and could
// never tell one of ours from anyone else's.
func TestBatchedPRsCarryTheFieldsTheSearchNeverReturned(t *testing.T) {
	fakeGH(t, `case "$*" in
  *"pr list --state open"*) echo '[{"number":4,"headRefName":"feature/x","additions":120,"deletions":7}]' ;;
  *) echo '[]' ;;
esac`)

	open, _ := RepoPRs(t.TempDir())
	d, ok := open[4]
	if !ok {
		t.Fatal("PR 4 missing from the batch")
	}
	if d.HeadRefName != "feature/x" || d.Additions != 120 || d.Deletions != 7 {
		t.Errorf("got %+v, want the head branch and diff size filled in", d.OpenPR)
	}
}

// A pull request the repo's open list does not contain has merged or closed,
// and its check runs and reviews are history. Re-reading them was most of what
// a long-lived cockpit spent: every feature the portfolio had ever shipped,
// twice a minute, for ever.
func TestMergedPRSignalsAreNotRereadEveryPoll(t *testing.T) {
	calls := fakeGH(t, `case "$*" in
  *"pr list --state open"*) echo '[]' ;;
  *"pr checks"*) echo '[{"state":"SUCCESS"}]' ;;
  *"pr view"*) echo '{"reviewDecision":"APPROVED","reviews":[]}' ;;
esac`)

	repo := t.TempDir()
	PRChecksFor(repo, 12)
	PRReviewFor(repo, 12)

	// A poll later, with PRTTL long expired, the answer must still stand.
	agePast(t, PRTTL)
	PRChecksFor(repo, 12)
	PRReviewFor(repo, 12)

	if got := len(perPR(calls())); got != 2 {
		t.Errorf("read a merged PR's frozen signals %d times, want 2: %v", got, perPR(calls()))
	}
}

// Unless the open list never answered. Absence from a list that failed is not
// evidence that anything has settled, so the ordinary TTL stands and the next
// poll asks again.
func TestAFailedOpenListDoesNotFreezeAnything(t *testing.T) {
	calls := fakeGH(t, `case "$*" in
  *"pr list --state open"*) echo "no git remotes found" >&2; exit 1 ;;
  *"pr checks"*) echo '[{"state":"SUCCESS"}]' ;;
esac`)

	repo := t.TempDir()
	PRChecksFor(repo, 12)
	agePast(t, PRTTL)
	PRChecksFor(repo, 12)

	var checks int
	for _, c := range calls() {
		if strings.HasPrefix(c, "pr checks") {
			checks++
		}
	}
	if checks != 2 {
		t.Errorf("asked %d times, want 2 — a failed list must not settle a PR", checks)
	}
}

// The branch of a dispatcher that has stopped is asked about far less often:
// nothing on our side will raise a PR on it, and a state dir full of finished
// features was costing one request per dead session per poll, for ever.
func TestAFinishedDispatchersBranchIsAskedAboutRarely(t *testing.T) {
	calls := fakeGH(t, `echo '[]'`)

	repo := t.TempDir()
	PRForBranch(repo, "feature/done", true)
	agePast(t, PRTTL)
	PRForBranch(repo, "feature/done", true)
	if got := len(calls()); got != 1 {
		t.Errorf("asked %d times about a finished dispatcher's branch, want 1", got)
	}

	// A live one is still asked at the ordinary rate — its PR may appear at any
	// moment, which is the whole point of the poll.
	PRForBranch(repo, "feature/live", false)
	agePast(t, PRTTL)
	PRForBranch(repo, "feature/live", false)
	var live int
	for _, c := range calls() {
		if strings.Contains(c, "feature/live") {
			live++
		}
	}
	if live != 2 {
		t.Errorf("asked %d times about a live dispatcher's branch, want 2", live)
	}
}

// agePast winds every cached entry back so the next read sees it as older than
// d, without the test having to wait.
func agePast(t *testing.T, d time.Duration) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for _, e := range cache {
		e.at = e.at.Add(-d - time.Second)
	}
}
