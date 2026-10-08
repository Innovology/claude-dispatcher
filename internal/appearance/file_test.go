package appearance

import (
	"os"
	"path/filepath"
	"testing"
)

// The file a desktop writes its switch to is the answer where the portal has
// none. Both spellings of each side, because the word a theme script writes is
// not ours to choose.
func TestFromFileReadsEitherWord(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		body string
		want Appearance
	}{
		{"light", Light},
		{"dark", Dark},
		{"  Light\n", Light},
		{"DARK\n", Dark},
		{"prefer-light", Light},
		{"prefer-dark", Dark},
	} {
		path := filepath.Join(dir, "mode")
		if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := FromFile(path)
		if err != nil || got != tc.want {
			t.Errorf("FromFile(%q) = %v, %v — want %v", tc.body, got, err, tc.want)
		}
	}
}

// A named file that cannot be read is not a vote. The caller falls through to
// the OS, because a typo in a path must not pin the wrong theme on, and the
// absence of the word "light" is not evidence of dark.
func TestFromFileRefusesToGuess(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"empty":   "",
		"spaces":  "   \n",
		"a word":  "solarized",
		"a theme": "catppuccin-mocha",
	} {
		path := filepath.Join(dir, "mode")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := FromFile(path)
		if got != Unknown || err == nil {
			t.Errorf("%s: FromFile = %v, %v — want unknown and an error", name, got, err)
		}
	}

	if got, err := FromFile(filepath.Join(dir, "nothing-here")); got != Unknown || err == nil {
		t.Errorf("a missing file: FromFile = %v, %v — want unknown and an error", got, err)
	}
	// No file named is not an error about a file: it is the ordinary case of
	// a machine that has only the OS to ask.
	if got, err := FromFile("  "); got != Unknown || err != ErrUnsupported {
		t.Errorf("no path: FromFile = %v, %v — want unknown and ErrUnsupported", got, err)
	}
}
