package dispatch

// mode.go is the permission mode a dispatcher's claude session opens in.
//
// The dispatch form has always asked this question — it was the AUTO switch —
// and the answer had nowhere to go. The launch command was `claude <prompt>`
// with no flags, so AUTO was a sentence in the prompt and nothing more: the
// session opened in whatever mode the human's own Claude Code defaults to, and
// an "unattended" dispatcher stopped on its first permission prompt with nobody
// watching it. The switch described a session it did not configure.
//
// The mode is now a launch flag (`--permission-mode`), recorded on the dispatch
// so a resumed session comes back the way it went out. The prompt sentence
// stays, because the two govern different things: the flag decides what claude
// may do without asking, the sentence decides how far it takes the work
// (commit, PR, fix CI). Neither substitutes for the other.

import (
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// Mode is how much a dispatcher may do without asking.
type Mode string

const (
	// ModeAuto is unattended: claude takes the edits and the safe commands
	// itself. It is the default because a dispatcher is by definition work sent
	// somewhere else to happen, with nobody sitting on the permission prompt.
	ModeAuto Mode = "auto"
	// ModeManual asks before each tool call — the human drives, from the
	// session's own terminal.
	ModeManual Mode = "manual"
	// ModePlan opens in plan mode: it reads and proposes, and changes nothing
	// until the plan is accepted.
	ModePlan Mode = "plan"
)

// DefaultMode is what a dispatch runs in when nothing chose one.
const DefaultMode = ModeAuto

// Modes is the offer order, and the order the form's switch cycles in: the
// unattended default first, then the two that hand control back.
func Modes() []Mode { return []Mode{ModeAuto, ModeManual, ModePlan} }

// Normalize resolves what to launch with. An empty or unrecognised mode
// becomes DefaultMode — a launch has to pass something, and the default is
// what the form would have offered.
//
// This is a launch-time answer, not a display one: a record written before the
// mode existed carries "", and the sessions behind those records ran in
// whatever claude defaulted to. Screens read that "" as unknown rather than
// calling it auto, because a record that never chose did not choose auto.
func (m Mode) Normalize() Mode {
	for _, k := range Modes() {
		if m == k {
			return m
		}
	}
	return DefaultMode
}

// Known reports whether m names a mode this build offers — the test for
// whether a record actually recorded one.
func (m Mode) Known() bool {
	for _, k := range Modes() {
		if m == k {
			return true
		}
	}
	return false
}

// Hint is the one line a form shows beside the mode. It describes the
// permission behaviour only — no keys, because the two forms that show it bind
// different ones, and what the dispatcher is told to do with the work is the
// prompt's half of the answer and is worded there.
func (m Mode) Hint() string {
	switch m.Normalize() {
	case ModeManual:
		return "asks you before each step"
	case ModePlan:
		return "plans first, changes nothing until you accept"
	}
	return "takes its own edits and safe commands"
}

// Summary is the mode as it reads on a one-line recap of the whole form.
func (m Mode) Summary() string {
	switch m.Normalize() {
	case ModeManual:
		return "asks each step"
	case ModePlan:
		return "plans first"
	}
	return "auto"
}

// Next is the mode after m in the cycle, so one key can walk all three.
func Next(m Mode) Mode {
	all := Modes()
	for i, k := range all {
		if k == m {
			return all[(i+1)%len(all)]
		}
	}
	return all[0]
}

// Prev is Next the other way, for shift+tab-style backwards cycling.
func Prev(m Mode) Mode {
	all := Modes()
	for i, k := range all {
		if k == m {
			return all[(i+len(all)-1)%len(all)]
		}
	}
	return all[0]
}

// modeAliases names each mode the way claude spells it, newest spelling first.
//
// Claude Code renamed its permission modes: what it now calls `auto` was
// `acceptEdits`, and what it now calls `manual` was `default`. Passing a name a
// given build does not know is not a degraded session — the CLI rejects the
// argument and exits before it ever reads the prompt, so the dispatch dies at
// launch and leaves a tmux session sitting at a shell. Naming the older
// equivalent keeps a dispatcher on an older claude running in the mode that was
// actually asked for.
var modeAliases = map[Mode][]string{
	ModeAuto:   {"auto", "acceptEdits"},
	ModeManual: {"manual", "default"},
	ModePlan:   {"plan"},
}

// PermissionArgs is the launch flag for m, as command-line words — empty when
// this claude has no spelling for it.
//
// Empty is the honest answer for a claude too old to have `--permission-mode`
// at all: the session then opens in that build's own default, which is exactly
// what every dispatch did before this file existed. Guessing a flag name into a
// CLI that will reject it would trade a mode we cannot set for a launch that
// does not happen.
func PermissionArgs(m Mode) []string {
	name := permissionName(m.Normalize())
	if name == "" {
		return nil
	}
	return []string{"--permission-mode", name}
}

// permissionName is the spelling of m this machine's claude accepts, or "".
func permissionName(m Mode) string {
	accepted := claudeModeNames()
	if len(accepted) == 0 {
		return ""
	}
	for _, name := range modeAliases[m] {
		if accepted[name] {
			return name
		}
	}
	return ""
}

// claudeModeNames is the set of --permission-mode values the installed claude
// advertises, read once per process. It is a variable so tests can answer for
// it instead of depending on whichever claude is on the test machine's PATH.
var claudeModeNames = sync.OnceValue(readClaudeModeNames)

// modeChoiceRe pulls the quoted names out of commander's `(choices: …)` tail.
var modeChoiceRe = regexp.MustCompile(`"([A-Za-z]+)"`)

// modeHelpWindow bounds how far past `--permission-mode` the choices may sit.
// Help output wraps, so the list spans lines; a few hundred characters covers
// the wrapped description without reaching the next option's own choices.
const modeHelpWindow = 400

// readClaudeModeNames asks claude which permission modes it takes.
//
// Reading the help text is the only way to ask: there is no capability
// endpoint, and the flag's accepted values have changed between releases. A
// help output we cannot read, or one whose choices do not include `plan` — the
// one spelling every version that ever had this flag accepts — is reported as
// "nothing known", and PermissionArgs then passes no flag rather than a guess.
func readClaudeModeNames() map[string]bool {
	out, err := exec.Command("claude", "--help").Output()
	if err != nil {
		return nil
	}
	return parseModeNames(string(out))
}

// parseModeNames pulls the accepted values out of claude's help text.
func parseModeNames(help string) map[string]bool {
	i := strings.Index(help, "--permission-mode")
	if i < 0 {
		return nil
	}
	rest := help[i:]
	if len(rest) > modeHelpWindow {
		rest = rest[:modeHelpWindow]
	}
	j := strings.Index(rest, "(choices:")
	if j < 0 {
		return nil
	}
	rest = rest[j:]
	if k := strings.Index(rest, ")"); k >= 0 {
		rest = rest[:k]
	}
	names := make(map[string]bool)
	for _, m := range modeChoiceRe.FindAllStringSubmatch(rest, -1) {
		names[m[1]] = true
	}
	if !names["plan"] {
		// Not the list we were looking for — the flag's description wrapped past
		// the window, or the format changed. Claiming these names would be
		// claiming a launch that works.
		return nil
	}
	return names
}
