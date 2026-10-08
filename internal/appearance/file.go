package appearance

import (
	"fmt"
	"os"
	"strings"
)

// FromFile reads the appearance out of a file whose whole contents are the
// word "light" or "dark".
//
// It exists because the portal is not universal truth. Measured on a NixOS/niri
// desktop running dankMaterialShell: `org.freedesktop.appearance color-scheme`
// answers 0, "no preference", while the desktop is plainly in light mode and
// publishes that fact to its own consumers by writing a file — the same file
// that machine's tmux.conf reads to pick its palette. A reporter that can only
// ask D-Bus is blind there, and blind in the same way on any setup whose theme
// switch is a script rather than a desktop environment.
//
// So the human may name the file their switch already writes. It is the one
// source we never have to guess at, which is why the cockpit prefers it over
// the portal, and it is a plain read rather than a subprocess, which is why it
// costs nothing to ask it often.
//
// A file that is missing, unreadable, or says something else is an error and
// NOT a vote: the caller falls through to the OS, exactly as if no file had
// been named. Naming a file must never be a way to end up with a wrong theme
// pinned on, and "light" appearing nowhere in it is not evidence of dark.
func FromFile(path string) (Appearance, error) {
	if strings.TrimSpace(path) == "" {
		return Unknown, ErrUnsupported
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Unknown, err
	}
	switch word := strings.ToLower(strings.TrimSpace(string(b))); word {
	case "light", "prefer-light", "2":
		return Light, nil
	case "dark", "prefer-dark", "1":
		return Dark, nil
	case "":
		return Unknown, fmt.Errorf("appearance file %s is empty", path)
	default:
		return Unknown, fmt.Errorf("appearance file %s says %q, which is neither light nor dark", path, word)
	}
}
