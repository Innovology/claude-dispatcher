// Package appearance reads whether the operating system is set to a light or a
// dark appearance — the switch a desktop flips at sunset, which a terminal
// following it repaints on. It is asked, never subscribed to: every platform
// has a command that answers in milliseconds, and none of them has a push
// channel reachable without a new dependency (D-Bus signals, NSDistributed-
// NotificationCenter, a registry watch).
package appearance

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// Appearance is the light/dark answer. Unknown is a real answer: a desktop
// with no preference set, or one we could not read, is not a vote for either.
type Appearance int

const (
	Unknown Appearance = iota
	Light
	Dark
)

func (a Appearance) String() string {
	switch a {
	case Light:
		return "light"
	case Dark:
		return "dark"
	}
	return "unknown"
}

// ErrUnsupported says there is nothing on this machine to ask — no portal
// client on Linux, say. It is how a caller polling for changes knows to stop
// rather than spawn a failing lookup every few seconds for the life of the
// process.
var ErrUnsupported = errors.New("no way to read the system appearance here")

// Query asks the operating system which appearance it is set to.
func Query(ctx context.Context) (Appearance, error) {
	switch runtime.GOOS {
	case "darwin":
		return queryDarwin(ctx)
	case "windows":
		return queryWindows(ctx)
	default:
		return queryPortal(ctx)
	}
}

// queryPortal reads the freedesktop appearance setting through the desktop
// portal — the one place GNOME, KDE and the wlroots/niri portals all publish
// it. busctl is tried first because it answers in a couple of milliseconds;
// gdbus is the portable fallback on a machine without systemd.
func queryPortal(ctx context.Context) (Appearance, error) {
	if _, err := exec.LookPath("busctl"); err == nil {
		out, err := exec.CommandContext(ctx, "busctl", "--user", "call",
			"org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop",
			"org.freedesktop.portal.Settings", "ReadOne", "ss",
			"org.freedesktop.appearance", "color-scheme").Output()
		if err == nil {
			return parsePortal(string(out)), nil
		}
	}
	if _, err := exec.LookPath("gdbus"); err == nil {
		out, err := exec.CommandContext(ctx, "gdbus", "call", "--session",
			"--dest", "org.freedesktop.portal.Desktop",
			"--object-path", "/org/freedesktop/portal/desktop",
			"--method", "org.freedesktop.portal.Settings.ReadOne",
			"org.freedesktop.appearance", "color-scheme").Output()
		if err != nil {
			return Unknown, err
		}
		return parsePortal(string(out)), nil
	}
	return Unknown, ErrUnsupported
}

// parsePortal reads the portal's color-scheme value out of either client's
// output: busctl prints `v u 1`, gdbus prints `(<uint32 1>,)`. The value is
// 1 for prefer-dark, 2 for prefer-light and 0 for no preference — which is
// Unknown, not light, because it means "the app's own default".
func parsePortal(out string) Appearance {
	f := strings.FieldsFunc(out, func(r rune) bool {
		return r < '0' || r > '9'
	})
	if len(f) == 0 {
		return Unknown
	}
	switch f[len(f)-1] {
	case "1":
		return Dark
	case "2":
		return Light
	}
	return Unknown
}

// queryDarwin reads AppleInterfaceStyle, which exists only while the Mac is
// dark: the key being absent is how macOS says light, so that exit status is
// an answer rather than a failure.
func queryDarwin(ctx context.Context) (Appearance, error) {
	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleInterfaceStyle").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "does not exist") {
			return Light, nil
		}
		return Unknown, err
	}
	if strings.EqualFold(strings.TrimSpace(string(out)), "dark") {
		return Dark, nil
	}
	return Light, nil
}

// queryWindows reads the apps half of the personalisation switch — the one
// terminals follow — rather than the system half, which colours the taskbar.
func queryWindows(ctx context.Context) (Appearance, error) {
	out, err := exec.CommandContext(ctx, "reg", "query",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		"/v", "AppsUseLightTheme").Output()
	if err != nil {
		return Unknown, err
	}
	return parseRegistry(string(out)), nil
}

// parseRegistry reads `AppsUseLightTheme    REG_DWORD    0x1` out of reg's
// output: 0x1 light, 0x0 dark.
func parseRegistry(out string) Appearance {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "AppsUseLightTheme" {
			switch f[2] {
			case "0x1":
				return Light
			case "0x0":
				return Dark
			}
		}
	}
	return Unknown
}
