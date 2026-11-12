package cause

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// What nix develop printed on the reporting machine when a dispatch to
// playerpulse failed: fifty lines of "copying path" and "building", then the
// failure. The record kept the first line — a download from cache.nixos.org —
// so the row said "did not start: tmux new-session: copying path …" and the
// reason was nowhere on screen.
func TestADownloadThatFailedIsNamedNotTheFirstPathCopied(t *testing.T) {
	got := Of(fixture(t, "nix-develop-download-failed.txt"))
	want := "error: Cannot build 'google-chrome-stable_142.0.7444.175-1_amd64.deb.drv' — " +
		"cannot download google-chrome-stable_142.0.7444.175-1_amd64.deb from any mirror"
	if got != want {
		t.Errorf("Of =\n%q\nwant\n%q", got, want)
	}
}

// A builder whose log has no "error:" line still says what it last printed,
// which is the line a human would read first in `nix log`.
func TestABuilderThatFailedIsNamedWithItsLastWords(t *testing.T) {
	got := Of(fixture(t, "nix-develop-builder-failed.txt"))
	want := "error: Cannot build 'broken-dep-1794486413.drv' — make: *** [Makefile:12: all] Error 2"
	if got != want {
		t.Errorf("Of =\n%q\nwant\n%q", got, want)
	}
}

// An evaluation error is a bare "error:" over a trace of what was being
// evaluated, with the error itself at the bottom of the trace.
func TestAnEvaluationErrorIsTheErrorAtTheBottomOfItsTrace(t *testing.T) {
	got := Of(fixture(t, "nix-develop-eval-failed.txt"))
	want := "error: attribute 'nonexistent' missing"
	if got != want {
		t.Errorf("Of = %q, want %q", got, want)
	}
}

func TestAFetchThatFailedIsTheErrorUnderItsTrace(t *testing.T) {
	got := Of(fixture(t, "nix-develop-fetch-failed.txt"))
	if !strings.HasPrefix(got, "error: Failed to open archive") || !strings.Contains(got, "HTTP error 404") {
		t.Errorf("Of = %q", got)
	}
}

// The two flake failures measured on the reporting machine before this: the
// error is the second line, under a note about where nix looked.
func TestAMissingFlakeIsTheErrorNotWhereNixLooked(t *testing.T) {
	for out, want := range map[string]string{
		"path \"/home/u\" does not contain a 'flake.nix', searching up\nerror: could not find a flake.nix file":                                                                      "error: could not find a flake.nix file",
		"path \"/w/x\" does not contain a 'flake.nix', searching up\nerror: path \"/w/x\" is not part of a flake (neither it nor its parent directories contain a 'flake.nix' file)": "error: path \"/w/x\" is not part of a flake (neither it nor its parent directories contain a 'flake.nix' file)",
	} {
		if got := Of(out); got != want {
			t.Errorf("Of = %q, want %q", got, want)
		}
	}
}

// git says what it is doing before it says why it could not.
func TestGitIsReadForItsFatalLine(t *testing.T) {
	out := "Preparing worktree (checking out 'feature/x')\nfatal: '/w/x' is a missing but already registered worktree;\nuse 'add -f' to override, or 'prune' or 'remove' to clear"
	if got, want := Of(out), "fatal: '/w/x' is a missing but already registered worktree;"; got != want {
		t.Errorf("Of = %q, want %q", got, want)
	}
}

// One line is its own cause, as tmux's own refusals are ("command too long",
// measured for ADR 0009); nothing in it is rewritten but the store hashes.
func TestOneLineIsItsOwnCause(t *testing.T) {
	for out, want := range map[string]string{
		"command too long\n":                      "command too long",
		"no server running on /tmp/tmux-1000/app": "no server running on /tmp/tmux-1000/app",
		"": "",
		"cannot exec /nix/store/0123456789abcdfghijklmnpqrsvwxyz-claude-2.1/bin/claude": "cannot exec claude-2.1/bin/claude",
	} {
		if got := Of(out); got != want {
			t.Errorf("Of(%q) = %q, want %q", out, got, want)
		}
	}
}

// Output with no error line in it is read from the end, past nix's progress,
// because that is where a command that died says the last thing it knew.
func TestWithNoErrorLineTheLastWordsAreTheCause(t *testing.T) {
	out := "copying path '/nix/store/0123456789abcdfghijklmnpqrsvwxyz-a' from 'https://cache.nixos.org'...\n" +
		"Killed\n" +
		"building '/nix/store/0123456789abcdfghijklmnpqrsvwxyz-b.drv'...\n"
	if got := Of(out); got != "Killed" {
		t.Errorf("Of = %q, want %q", got, "Killed")
	}
}

// A terminal's colours and a progress bar's carriage returns are not words.
func TestColourAndCarriageReturnsAreNotPartOfTheCause(t *testing.T) {
	out := "fetching 10%\rfetching 90%\r\x1b[31;1merror:\x1b[0m could not reach it\n"
	if got, want := Of(out), "error: could not reach it"; got != want {
		t.Errorf("Of = %q, want %q", got, want)
	}
}

func TestACauseIsOneLineOfBoundedLength(t *testing.T) {
	got := Of("error: " + strings.Repeat("x", 2000))
	if strings.Contains(got, "\n") || len([]rune(got)) > maxLen {
		t.Errorf("Of is %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a cut cause does not say it was cut: %q", got[len(got)-10:])
	}
}

// The error a failed command becomes says its cause in one line, and keeps
// everything the command printed for whoever needs the rest — wrapped or not.
func TestAnErrorSaysItsCauseAndKeepsItsOutput(t *testing.T) {
	out := fixture(t, "nix-develop-download-failed.txt")
	err := &Error{Op: "tmux new-session", Output: out}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("Error() is more than a line: %q", err.Error())
	}
	if want := "tmux new-session: " + Of(out); err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	wrapped := fmt.Errorf("rebuild /w/x: %w", err)
	full := Full(wrapped)
	if !strings.HasPrefix(full, wrapped.Error()+"\n") {
		t.Errorf("Full does not lead with the error it was given:\n%s", full[:200])
	}
	if !strings.Contains(full, strings.TrimSpace(out)) {
		t.Error("Full dropped some of what the command printed")
	}
	if _, ok := Output(wrapped); !ok {
		t.Error("Output did not find the command's output through the wrap")
	}
}

func TestAnErrorWithNothingPrintedSaysItFailed(t *testing.T) {
	err := &Error{Op: "git worktree add feature/x", Output: "  \n"}
	if got, want := err.Error(), "git worktree add feature/x failed"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got := Full(err); got != err.Error() {
		t.Errorf("Full = %q", got)
	}
}

// Full of an error that is not a command's is that error's own words.
func TestFullOfAPlainErrorIsItsMessage(t *testing.T) {
	err := errors.New("no dispatch record")
	if Full(err) != "no dispatch record" {
		t.Errorf("Full = %q", Full(err))
	}
	if _, ok := Output(err); ok {
		t.Error("a plain error claims an output")
	}
	if Full(nil) != "" {
		t.Error("Full(nil) is not empty")
	}
}

// A command that printed nothing is described by how it ended.
func TestACommandThatPrintedNothingSaysHowItEnded(t *testing.T) {
	err := Failed("tmux new-session", nil, errors.New("exec: \"nix\": executable file not found in $PATH"))
	if got, want := err.Error(), "tmux new-session: exec: \"nix\": executable file not found in $PATH"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	err = Failed("git worktree add feature/x", []byte("Preparing worktree (checking out 'feature/x')\nfatal: '../wt' already exists\n"), errors.New("exit status 128"))
	if got, want := err.Error(), "git worktree add feature/x: fatal: '../wt' already exists"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// Reading all of it starts at the error, not at the top of fifty lines of
// downloads: LineOf is where the error Of reads opens, counted the way an
// editor counts, blank lines included.
func TestLineOfIsWhereTheErrorOpens(t *testing.T) {
	out := fixture(t, "nix-develop-download-failed.txt")
	n := LineOf(out)
	lines := strings.Split(out, "\n")
	if n < 1 || n > len(lines) || !strings.HasPrefix(lines[n-1], "error: Cannot build") {
		t.Fatalf("LineOf = %d, which is not the first error line", n)
	}
	if got := LineOf("summary line\n\n" + out); got != n+2 {
		t.Errorf("LineOf under a two-line lead = %d, want %d", got, n+2)
	}
	if got := LineOf("Killed\n"); got != 0 {
		t.Errorf("LineOf with no error line = %d, want 0", got)
	}
}
