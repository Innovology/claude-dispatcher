package gh

// live_test.go stands a fake `gh` on PATH and counts what the package asks it
// for. The point of these tests is the request count, not the parse: this
// package's job is to answer the cockpit without spending the hourly quota.

import (
	"os"
	"path/filepath"
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
	InvalidateCache()
	t.Cleanup(InvalidateCache)
	return func() []string {
		b, err := os.ReadFile(log)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
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
			got := calls()
			if len(got) != 1 {
				t.Fatalf("wanted one request, got %d: %v", len(got), got)
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
	if got := calls(); len(got) != 2 || !strings.Contains(got[1], "pr view") {
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
		if pr := PRForBranch(repo, "feature/x"); pr == nil || pr.Number != 3 {
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
