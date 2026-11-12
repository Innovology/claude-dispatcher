// Package cause finds, in what a failed command printed, the line that says
// why it failed.
//
// A launch reported its failure as the first line of the supervisor's output,
// on the record, in the footer and on the row. That works for tmux, whose
// refusals are one line ("command too long"), and fails for everything that
// wraps it. Measured on a NixOS machine, a dispatch whose `nix develop` could
// not build its shell printed 883 lines: "copying path … from
// https://cache.nixos.org" and "building '…'" for every dependency, then the
// failure. The row said "did not start: tmux new-session: copying path
// '/nix/store/…-nss-cacert-3.115' from 'https://cache.nixos.org'...", which is
// the one line of the whole output that says nothing went wrong. git does the
// same thing more briefly: "Preparing worktree (…)", then the fatal line.
//
// So the cause is read where tools put it. The first line that opens with
// "error:" or "fatal:" names the failure, and the indented block under it —
// nix's account of a build, or the trace of an evaluation — is searched for
// the deepest error it holds. Output with no such line is read from the end,
// past nix's progress, because a command that died says the last thing it knew
// last.
package cause

import (
	"errors"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Error is a command that failed: what was run, and everything it printed.
// Error() is one line — the operation and the cause — because every place an
// error is shown has room for one; the output stays behind it for the one
// place that has room for all of it (Full).
type Error struct {
	// Op names what was run, as the error leads with it: "tmux new-session".
	Op string
	// Output is everything the command printed, stdout and stderr together.
	Output string
}

// Failed is the error for the command op, which ended with err after printing
// out. A command that printed nothing is described by how it ended — "exit
// status 1", or the exec's own "executable file not found" — rather than by
// silence.
func Failed(op string, out []byte, err error) *Error {
	text := string(out)
	if strings.TrimSpace(text) == "" && err != nil {
		text = err.Error()
	}
	return &Error{Op: op, Output: text}
}

func (e *Error) Error() string {
	if c := Of(e.Output); c != "" {
		return e.Op + ": " + c
	}
	return e.Op + " failed"
}

// Output is what the failed command behind err printed, found through any
// wrapping; false when err is not a command's.
func Output(err error) (string, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Output, true
	}
	return "", false
}

// Full is err's one line followed by everything its command printed, for a
// record of the failure that has room for the whole of it. An error that is
// not a command's, or whose command said no more than its cause, is its own
// message.
func Full(err error) string {
	if err == nil {
		return ""
	}
	out, ok := Output(err)
	out = strings.TrimSpace(out)
	if !ok || out == "" || out == Of(out) {
		return err.Error()
	}
	return err.Error() + "\n\n" + out
}

// maxLen bounds a cause. It is shown in a table's detail panel and a footer,
// and an error line can be a whole paragraph — nix prints a failed fetch's
// response body inside it.
const maxLen = 300

// storeHash is a Nix store path's hash and the dash after it. Thirty-two
// characters of base32 say nothing to a person reading why a dispatch did not
// start, and the name after them says everything; the output kept beside the
// cause (Full) has the paths whole, for `nix log`.
var storeHash = regexp.MustCompile(`/nix/store/[0-9a-df-np-sv-z]{32}-`)

// Of is the line of out that says why the command that printed it failed, in
// one line of at most maxLen runes; "" for no output at all.
func Of(out string) string {
	lines := clean(out)
	if len(lines) == 0 {
		return ""
	}
	for i, ln := range lines {
		if !topLevel(ln) || !isError(strings.TrimSpace(ln)) {
			continue
		}
		end := i + 1
		for end < len(lines) && !topLevel(lines[end]) {
			end++
		}
		return finish(fromBlock(strings.TrimSpace(ln), lines[i+1:end]))
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); !progress(s) {
			return finish(s)
		}
	}
	return finish(strings.TrimSpace(lines[len(lines)-1]))
}

// fromBlock is the cause an error line and the indented block under it give.
//
// nix explains a failed build under its error line: the reason ("builder
// failed with exit code 1"), then the builder's last log lines, each behind a
// "> ", where the builder's own complaint usually is. That complaint is the
// cause ("cannot download google-chrome-stable_….deb from any mirror"), and
// the error line says what it was the cause of. An evaluation error is a bare
// "error:" over a trace of what was being evaluated, with the error itself at
// the bottom, so there the bottom error is the whole cause.
func fromBlock(head string, block []string) string {
	var nested, logged, reason string
	for _, ln := range block {
		s := strings.TrimSpace(ln)
		if rest, ok := strings.CutPrefix(s, ">"); ok {
			if rest = strings.TrimSpace(rest); strings.Contains(strings.ToLower(rest), "error") {
				logged = rest
			}
			continue
		}
		if isError(s) {
			nested = s
			continue
		}
		if r, ok := strings.CutPrefix(s, "Reason: "); ok && reason == "" {
			reason = r
		}
	}
	if message(head) == "" {
		if nested != "" {
			return nested
		}
		head = strings.TrimSpace(head + " " + logged)
		return head
	}
	deeper := nested
	if deeper == "" {
		deeper = logged
	}
	if deeper == "" {
		deeper = reason
	}
	if deeper == "" {
		return head
	}
	return strings.TrimSuffix(head, ".") + " — " + message(deeper)
}

// clean splits out into the lines a person would have seen: colour codes
// gone, a progress line's carriage returns resolved to what it last said, and
// blank lines dropped. Indentation is kept, because it is how nix says which
// lines belong to which error.
func clean(out string) []string {
	var lines []string
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if i := strings.LastIndexByte(ln, '\r'); i >= 0 {
			ln = ln[i+1:]
		}
		ln = strings.TrimRight(ansi.Strip(ln), " \t")
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	return lines
}

func topLevel(ln string) bool { return ln != "" && ln[0] != ' ' && ln[0] != '\t' }

// isError reports whether s opens with the word a tool fails with: git's
// "fatal:", and "error:" for nix and git both, in any case.
func isError(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "error:") || strings.HasPrefix(l, "fatal:")
}

// message is an error line without its "error:" or "fatal:".
func message(s string) string {
	if !isError(s) {
		return s
	}
	return strings.TrimSpace(s[strings.IndexByte(s, ':')+1:])
}

// progressPrefixes open the lines a command prints while it is still working:
// nix's downloads and builds, and git setting up a worktree. None of them is
// ever why it stopped.
var progressPrefixes = []string{
	"copying path ", "building '", "unpacking '", "downloading '", "fetching ",
	"evaluating ", "these ", "this path will be", "this derivation will be",
	"warning: ", "Preparing worktree",
}

func progress(s string) bool {
	for _, p := range progressPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	// A bare store path is a line of nix's list of what it will fetch.
	return strings.HasPrefix(s, "/nix/store/") && !strings.ContainsAny(s, " \t")
}

// finish makes a cause fit where it is shown: store hashes gone and its length
// bounded, saying so when it was cut.
func finish(s string) string {
	s = storeHash.ReplaceAllString(s, "")
	if r := []rune(s); len(r) > maxLen {
		s = strings.TrimSpace(string(r[:maxLen-1])) + "…"
	}
	return s
}
