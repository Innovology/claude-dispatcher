package main

// The Homebrew cask is the one thing this repo ships that no other test can
// reach: goreleaser writes it at release time and Homebrew runs it on someone
// else's machine, so a mistake in it is discovered by a stranger's failed
// install rather than by CI. This test reads the config the cask is generated
// from and enforces the one rule that has already been broken once.
//
// The rule: a cask hook runs on every OS the cask covers, and this cask covers
// Linux — goreleaser emits an `on_linux` block from the linux archives, which
// is exactly how a Windows user installs under WSL2 (the README's recommended
// route). So a hook that shells out to a macOS-only tool must ask `OS.mac?`
// first, in Ruby, without spawning anything.
//
// What it must not do is probe for the tool by running it: a cask's
// `system_command` is Homebrew's SystemCommand.run!, which raises
// ErrorDuringExecution on a non-zero exit, and a missing executable comes back
// as exit 127. The probe that was meant to be the guard was itself what failed
// every WSL install in postflight.

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestCaskHooksGuardMacOnlyCommands(t *testing.T) {
	raw, err := os.ReadFile(".goreleaser.yml")
	// The Nix build ships only the Go sources (see nix/package.nix), and runs
	// the suite from that copy — there is no release config there to read.
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("no .goreleaser.yml alongside the sources — nix builds ship only the Go files")
	}
	if err != nil {
		t.Fatal(err)
	}

	bodies := hookBodies(string(raw))
	if len(bodies) == 0 {
		t.Fatal("no cask hooks found — either the config moved or this test stopped reading it")
	}
	for _, body := range bodies {
		guarded := false
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if line == "if OS.mac?" {
				guarded = true
			}
			if strings.Contains(line, "system_command") && !guarded {
				t.Errorf("cask hook runs %q before asking OS.mac? — this hook also runs on Linuxbrew, "+
					"where a missing executable raises out of system_command and fails the install:\n%s", line, body)
			}
		}
	}
}

// hookBodies returns the block scalars under the cask's `hooks:` mapping —
// the Ruby that Homebrew will run. Hand-rolled rather than parsed: the repo
// has no YAML dependency and this needs none, because the shape it reads
// (a `hooks:` key, then `<name>: |` block scalars beneath it) is the only
// shape goreleaser accepts.
func hookBodies(config string) []string {
	var bodies []string
	lines := strings.Split(config, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "hooks:" {
			continue
		}
		hooksIndent := indentOf(line)
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			if indentOf(lines[j]) <= hooksIndent {
				break // out of the hooks mapping
			}
			if !strings.HasSuffix(strings.TrimSpace(lines[j]), ": |") {
				continue
			}
			var body []string
			blockIndent := indentOf(lines[j])
			for k := j + 1; k < len(lines); k++ {
				if strings.TrimSpace(lines[k]) != "" && indentOf(lines[k]) <= blockIndent {
					break
				}
				body = append(body, lines[k])
			}
			bodies = append(bodies, strings.Join(body, "\n"))
		}
	}
	return bodies
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }
