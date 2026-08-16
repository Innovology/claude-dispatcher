package dispatch

// mode_test.go covers the permission mode: what it resolves to, what it puts on
// the command line, and what it does when the claude on this machine spells its
// modes differently.

import (
	"strings"
	"testing"
)

// modernHelp is the shape claude's help has today: the choices wrap across
// lines, which is why the parser works on the whole string rather than a line.
const modernHelp = `Usage: claude [options] [command] [prompt]

Options:
  --permission-mode <mode>              Permission mode to use for the session
                                        (choices: "acceptEdits", "auto",
                                        "bypassPermissions", "manual",
                                        "dontAsk", "plan")
  --plugin-dir <path>                   Load a plugin from a directory
`

// legacyHelp is a build from before the rename: no auto, no manual.
const legacyHelp = `Options:
  --permission-mode <mode>              Permission mode to use for the session
                                        (choices: "acceptEdits", "bypassPermissions", "default", "plan")
`

// withModes answers for the installed claude so a test never depends on
// whichever one is on the machine's PATH.
func withModes(t *testing.T, names map[string]bool) {
	t.Helper()
	prev := claudeModeNames
	claudeModeNames = func() map[string]bool { return names }
	t.Cleanup(func() { claudeModeNames = prev })
}

func TestModeNormalizeDefaultsToAuto(t *testing.T) {
	for _, in := range []Mode{"", "acceptEdits", "nonsense", "AUTO"} {
		if got := in.Normalize(); got != ModeAuto {
			t.Errorf("Mode(%q).Normalize() = %q, want auto", in, got)
		}
	}
	for _, in := range Modes() {
		if got := in.Normalize(); got != in {
			t.Errorf("Mode(%q).Normalize() = %q, want itself", in, got)
		}
	}
	// A record written before the mode existed did not choose auto — it did not
	// choose. Screens read Known, not Normalize, so they can say so.
	if Mode("").Known() {
		t.Error("an unset mode must not report as a mode that was chosen")
	}
}

// The whole point of the field: what the human picked reaches the command line.
func TestPermissionArgsNamesTheChosenMode(t *testing.T) {
	withModes(t, parseModeNames(modernHelp))
	for _, m := range Modes() {
		args := PermissionArgs(m)
		if len(args) != 2 || args[0] != "--permission-mode" || args[1] != string(m) {
			t.Errorf("PermissionArgs(%q) = %v", m, args)
		}
	}
}

// An older claude spells two of the three differently. Passing today's name to
// it is not a degraded session: the CLI rejects the argument and exits before
// it reads the prompt, so the dispatch dies at launch.
func TestPermissionArgsFallsBackToTheOlderSpelling(t *testing.T) {
	withModes(t, parseModeNames(legacyHelp))
	want := map[Mode]string{ModeAuto: "acceptEdits", ModeManual: "default", ModePlan: "plan"}
	for m, spelling := range want {
		args := PermissionArgs(m)
		if len(args) != 2 || args[1] != spelling {
			t.Errorf("PermissionArgs(%q) = %v, want the %q spelling", m, args, spelling)
		}
	}
}

// A claude with no --permission-mode at all, or a help output we cannot read,
// gets no flag: the session opens in that build's own default, which is what
// every dispatch did before the mode was plumbed. A guessed flag name would
// trade a mode we cannot set for a launch that does not happen.
func TestPermissionArgsSaysNothingWhenItCannotTell(t *testing.T) {
	for _, help := range []string{
		"Usage: claude [options]\n  --model <model>  the model\n",             // no such flag
		"  --permission-mode <mode>   Permission mode to use for the session", // no choices
		`  --permission-mode <mode>  (choices: "wibble", "wobble")`,           // not a list we recognise
	} {
		withModes(t, parseModeNames(help))
		if args := PermissionArgs(ModeAuto); args != nil {
			t.Errorf("help %q yielded %v, want no flag", help, args)
		}
	}
}

// The launch and resume commands carry the flag, and carry the same one: a
// resumed dispatcher that came back in a different mode than it went out in
// would be a permission change nobody asked for.
func TestLaunchAndResumeCarryTheMode(t *testing.T) {
	withModes(t, parseModeNames(modernHelp))

	launch := launchCommand("abc123", "do the thing", ModePlan)
	if !strings.Contains(launch, "--permission-mode plan") {
		t.Errorf("launch command has no mode flag:\n%s", launch)
	}
	if !strings.Contains(launch, "do the thing") {
		t.Errorf("launch command lost the prompt:\n%s", launch)
	}

	resume := resumeCommand("abc123", "sess-1", "", ModePlan)
	if !strings.Contains(resume, "--permission-mode plan") {
		t.Errorf("resume command has no mode flag:\n%s", resume)
	}
	if !strings.Contains(resume, "--resume") {
		t.Errorf("resume command lost --resume:\n%s", resume)
	}

	// And with nothing to pass, the command is exactly what it always was — no
	// stray spaces where the flag would have gone.
	withModes(t, nil)
	if got := launchCommand("abc123", "go", ModeAuto); strings.Contains(got, "claude  ") {
		t.Errorf("a missing flag left a double space:\n%s", got)
	}
}

func TestModeCyclesBothWays(t *testing.T) {
	all := Modes()
	m := all[0]
	for range all {
		m = Next(m)
	}
	if m != all[0] {
		t.Errorf("cycling through every mode landed on %q, want %q", m, all[0])
	}
	if got := Prev(all[0]); got != all[len(all)-1] {
		t.Errorf("Prev(%q) = %q, want the last mode", all[0], got)
	}
	// An unset mode has to go somewhere on the first keypress rather than
	// staying unset, or the switch reads as broken.
	if got := Next(""); got != all[0] {
		t.Errorf("Next(\"\") = %q, want %q", got, all[0])
	}
}

// Each mode says something different about itself, and none of it names a key:
// the two forms that show these bind different ones.
func TestModeHintsAreDistinctAndKeyless(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Modes() {
		h := m.Hint()
		if h == "" || seen[h] {
			t.Errorf("Mode(%q).Hint() = %q — empty or a repeat", m, h)
		}
		seen[h] = true
		for _, key := range []string{"space", "enter", "tab"} {
			if strings.Contains(h, key) {
				t.Errorf("Mode(%q).Hint() names the %q key: %q", m, key, h)
			}
		}
	}
}
