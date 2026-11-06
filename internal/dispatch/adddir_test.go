//go:build !windows

package dispatch

import (
	"strings"
	"testing"
)

// --add-dir is variadic (`--add-dir <directories...>` in claude's own help), so
// it takes every word after it until the next flag. Before the prompt it would
// read the prompt as one more directory and open the session on nothing. It
// goes last, with only the pane's exit shell after it, and on a resume after
// the message for the same reason.
func TestAddDirGoesAfterThePrompt(t *testing.T) {
	for name, line := range map[string]string{
		"launch": launchCommand("abc", "/p/abc.txt", "", ModeAuto, DefaultModel, "", "/src/a", "/src/b c"),
		"resume": resumeCommand("abc", "sess-1", "/p/abc.txt", "", ModeAuto, DefaultModel, "", "/src/a", "/src/b c"),
	} {
		prompt := strings.Index(line, `"$(cat '/p/abc.txt')"`)
		flag := strings.Index(line, ` --add-dir '/src/a' '/src/b c'`)
		shell := strings.Index(line, "; cdsh=")
		if prompt < 0 || flag < prompt || shell < flag {
			t.Errorf("%s: want prompt, then --add-dir, then the exit shell:\n%s", name, line)
		}
	}
	// A session given nothing beyond its own directory is launched exactly as
	// it always was.
	if strings.Contains(launchCommand("abc", "/p/abc.txt", "", ModeAuto, DefaultModel, ""), "--add-dir") ||
		strings.Contains(resumeCommand("abc", "sess-1", "", "", ModeAuto, DefaultModel, ""), "--add-dir") {
		t.Error("--add-dir appeared with no directories to give")
	}
}
