package fuzzy

// differential_test.go checks the claim the package doc makes: that this is
// fzf, not something fzf-like. It runs the installed `fzf -f` over the same
// candidates and requires the same order back.
//
// It is the only test that can catch the class of bug this package already had
// twice — algo.Init never called, and the pattern handed over unfolded — both
// of which produced a matcher that looked fine, returned plausible rankings,
// and disagreed with fzf. A hand-written expectation cannot find those, because
// the expectation is written by the same person who got it wrong.
//
// Skipped where fzf is not installed: the library is linked in and works
// without the binary, so its absence is not a failure.

import (
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
)

// linkedFzfVersion is the fzf whose matcher is compiled in, read from the build
// info rather than written down, so it cannot drift from go.mod.
func linkedFzfVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, d := range info.Deps {
		if d.Path == "github.com/junegunn/fzf" {
			return strings.TrimPrefix(d.Version, "v")
		}
	}
	return ""
}

// installedFzfVersion is the `fzf --version` of the binary on PATH, which
// prints "0.72.0 (refs/tags/v0.72.0)".
func installedFzfVersion() string {
	out, err := exec.Command("fzf", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Fields(string(out))[0])
}

func fzfOrder(t *testing.T, query string, candidates []string) []string {
	t.Helper()
	cmd := exec.Command("fzf", "-f", query)
	cmd.Stdin = strings.NewReader(strings.Join(candidates, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		// fzf exits 1 when nothing matched, which is an answer, not an error.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("fzf -f %q: %v", query, err)
	}
	var got []string
	for _, ln := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if ln != "" {
			got = append(got, ln)
		}
	}
	return got
}

func TestMatchesTheRealFzf(t *testing.T) {
	if _, err := exec.LookPath("fzf"); err != nil {
		t.Skip("fzf not installed — the library does not need it, this check does")
	}
	// fzf's scoring changes between releases, so a mismatch here would be a
	// finding about the two versions and not about this package. Measured: at
	// 0.72.0 against a 0.74.3 library, exactly one of the queries below ranks a
	// path differently; with the versions matched, all of them agree.
	if linked, installed := linkedFzfVersion(), installedFzfVersion(); linked != installed {
		t.Skipf("fzf %s is installed and %s is linked in — scoring differs between releases, so this would compare versions rather than implementations",
			installed, linked)
	}

	// Names and paths as the cockpit actually searches them: repo names, a
	// branch list, and worktree paths.
	corpus := append([]string{}, repos...)
	corpus = append(corpus,
		"/home/dev/Projects/Org/player-app/main",
		"/home/dev/Projects/Org/player-app/ci-scheduling",
		"/home/dev/Projects/Org/playerpulse/.git",
		"feature/bridge-join-requests-ui",
		"fix/staging-seed-rpe-comments",
		"release-qa-2026-08-30-1",
		"main", "master", "dev",
	)

	queries := []string{
		"papp", "ppulse", "pp", "dev", "main", "cldisp", "sched", "qa",
		"feat", "fix", "app", "p", "er", "2026", "Player", "PP",
		"jointui", "hdpam", "kolch", "tab", "zzz", "-", ".",
	}

	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			want := fzfOrder(t, q, corpus)
			hits := Rank(q, corpus)
			var got []string
			for _, h := range hits {
				got = append(got, corpus[h.Index])
			}
			if len(got) != len(want) {
				t.Fatalf("query %q: got %d matches, fzf found %d\n got: %v\nfzf: %v",
					q, len(got), len(want), got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("query %q: rank %d is %q, fzf says %q\n got: %v\nfzf: %v",
						q, i, got[i], want[i], got, want)
				}
			}
		})
	}
}
