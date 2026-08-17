package dispatch

// model.go is the model a dispatcher's claude session runs.
//
// Like the permission mode (mode.go), this is a launch flag: `--model` with an
// alias like `opus` or `sonnet`. And like the mode, the offer has to come from
// the claude actually installed — its help text names the aliases it takes, and
// a flag value we invented would either kill the launch or open a session that
// errors on its first message with nobody watching. So the form offers exactly
// two kinds of answer: "default", which passes no flag and lets the session
// open on whatever the human's own Claude Code is configured for, and the
// aliases this machine's claude advertises.

import (
	"regexp"
	"strings"
	"sync"
)

// Model is which model the session is asked to run — a claude CLI alias, or
// ModelDefault for "pass no flag".
type Model string

// ModelDefault passes no --model at all: the session opens on whatever the
// human's own Claude Code defaults to, which is exactly what every dispatch
// did before the model was a choice.
const ModelDefault Model = "default"

// DefaultModel is what a dispatch runs when nothing chose one.
const DefaultModel = ModelDefault

// Models is the offer order: the no-flag default first — taking it is one
// keypress, and it is the only answer that cannot be wrong — then the aliases
// the installed claude advertises, in the order its help names them.
func Models() []Model {
	out := []Model{ModelDefault}
	for _, a := range claudeModelAliases() {
		out = append(out, Model(a))
	}
	return out
}

// Known reports whether m names a model this machine's claude offers — the
// test for whether a record actually recorded a choice this build can read.
func (m Model) Known() bool {
	for _, k := range Models() {
		if m == k {
			return true
		}
	}
	return false
}

// Normalize resolves what to launch with. Empty, unrecognised, or an alias
// this machine's claude does not advertise all become ModelDefault: there is
// no older spelling to fall back to the way modes have, and no flag at all is
// the one value that launches everywhere.
//
// Like the mode, this is a launch-time answer, not a display one: a record
// written before the model existed carries "", and screens read that as
// unknown rather than calling it the default, because a record that never
// chose did not choose.
func (m Model) Normalize() Model {
	if m.Known() {
		return m
	}
	return ModelDefault
}

// Hint is the one line a form shows beside the model. Capability claims would
// go stale with every release, so the hints say only what the choice does.
func (m Model) Hint() string {
	if m.Normalize() == ModelDefault {
		return "whatever your own claude is set to"
	}
	return "runs the latest " + capitalize(string(m.Normalize()))
}

// capitalize upper-cases the first rune, for reading an alias as a name.
func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// NextModel and PrevModel walk the offer, so one key can cycle a switch.
func NextModel(m Model) Model {
	all := Models()
	for i, k := range all {
		if k == m {
			return all[(i+1)%len(all)]
		}
	}
	return all[0]
}

func PrevModel(m Model) Model {
	all := Models()
	for i, k := range all {
		if k == m {
			return all[(i+len(all)-1)%len(all)]
		}
	}
	return all[0]
}

// ModelArgs is the launch flag for m, as command-line words — empty for the
// default, and empty for an alias this claude does not advertise. A dispatch
// resumed on a machine whose claude no longer names the recorded alias opens
// on that build's own default rather than being handed a value it may reject:
// a rejected flag is not a degraded session — it is a launch that never
// happens.
func ModelArgs(m Model) []string {
	m = m.Normalize()
	if m == ModelDefault {
		return nil
	}
	return []string{"--model", string(m)}
}

// claudeModelAliases is the list of --model aliases the installed claude
// advertises, read once per process. A variable so tests can answer for it
// instead of depending on whichever claude is on the test machine's PATH.
var claudeModelAliases = sync.OnceValue(readClaudeModelAliases)

// modelAliasRe pulls the quoted aliases out of the flag's description —
// claude's help quotes them 'like this', unlike the mode choices' "this".
var modelAliasRe = regexp.MustCompile(`'([a-z][a-z0-9-]*)'`)

// modelHelpWindow bounds how far past `--model ` the aliases may sit; the
// description wraps, so they span lines.
const modelHelpWindow = 400

// readClaudeModelAliases asks claude which model aliases it takes.
//
// Reading the help text is the only way to ask, and the help does not
// enumerate choices the way --permission-mode does — it names example aliases
// in prose ("an alias for the latest model (e.g. 'fable', 'opus', or
// 'sonnet')"). Those examples are the aliases worth offering: a name the help
// does not vouch for is a guess, and a guessed model is a session erroring on
// its first message with nobody watching it.
func readClaudeModelAliases() []string {
	out, err := claudeHelp()
	if err != nil {
		return nil
	}
	return parseModelAliases(string(out))
}

// parseModelAliases pulls the advertised aliases out of claude's help text.
func parseModelAliases(help string) []string {
	// "--model " with the trailing space: --fallback-model and the <model>
	// placeholders must not anchor the window.
	i := strings.Index(help, "--model ")
	if i < 0 {
		return nil
	}
	rest := help[i:]
	if len(rest) > modelHelpWindow {
		rest = rest[:modelHelpWindow]
	}
	var names []string
	sane := false
	for _, m := range modelAliasRe.FindAllStringSubmatch(rest, -1) {
		a := m[1]
		if strings.Contains(a, "-") {
			// A full model name ('claude-fable-5') — an example of the other
			// argument form, not an alias to offer.
			continue
		}
		names = append(names, a)
		if a == "opus" || a == "sonnet" {
			sane = true
		}
	}
	if !sane {
		// Not the list we were looking for — the format changed, or the window
		// caught someone else's quotes. Claiming these names would be claiming
		// a launch that works.
		return nil
	}
	return names
}
