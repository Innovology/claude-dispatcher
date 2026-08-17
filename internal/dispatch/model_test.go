package dispatch

// model_test.go covers the model choice: what the form may offer, what reaches
// the command line, and what happens on a machine whose claude advertises
// nothing — plus the fan-out sentence, which is the prompt's half of the same
// form.

import (
	"strings"
	"testing"
)

// modelHelp is the shape claude's help has today: --fallback-model sits above
// --model (so a naive substring anchor grabs the wrong flag), the aliases are
// quoted in prose, and a full model name is quoted right beside them.
const modelHelp = `Options:
  --fallback-model <model>              Enable automatic fallback to specified
                                        model(s) when the default model is
                                        overloaded (only works with --print)
  --model <model>                       Model for the current session. Provide
                                        an alias for the latest model (e.g.
                                        'fable', 'opus', or 'sonnet') or a
                                        model's full name (e.g.
                                        'claude-fable-5').
`

// withAliases answers for the installed claude so a test never depends on
// whichever one is on the machine's PATH.
func withAliases(t *testing.T, aliases []string) {
	t.Helper()
	prev := claudeModelAliases
	claudeModelAliases = func() []string { return aliases }
	t.Cleanup(func() { claudeModelAliases = prev })
}

func TestParseModelAliasesReadsTheAdvertisedAliases(t *testing.T) {
	got := parseModelAliases(modelHelp)
	want := []string{"fable", "opus", "sonnet"}
	if len(got) != len(want) {
		t.Fatalf("parseModelAliases = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseModelAliases = %v, want %v", got, want)
		}
	}
}

// A help output we cannot read yields no aliases, and the offer collapses to
// the default — never to a guess.
func TestParseModelAliasesSaysNothingWhenItCannotTell(t *testing.T) {
	for _, help := range []string{
		"Usage: claude [options]\n  --permission-mode <mode>  the mode\n", // no such flag
		"  --model <model>   Model for the current session",               // no quoted aliases
		"  --model <model>   an alias (e.g. 'wibble' or 'wobble')",        // not a list we recognise
	} {
		if got := parseModelAliases(help); got != nil {
			t.Errorf("help %q yielded %v, want nothing", help, got)
		}
	}
}

// The offer is the default first — the one answer that cannot be wrong — then
// whatever this machine's claude advertises, and nothing else.
func TestModelsOffersDefaultThenAdvertised(t *testing.T) {
	withAliases(t, parseModelAliases(modelHelp))
	all := Models()
	if all[0] != ModelDefault {
		t.Fatalf("Models() opens on %q, want the default", all[0])
	}
	if len(all) != 4 || all[1] != "fable" || all[2] != "opus" || all[3] != "sonnet" {
		t.Fatalf("Models() = %v", all)
	}

	withAliases(t, nil)
	if all := Models(); len(all) != 1 || all[0] != ModelDefault {
		t.Fatalf("with nothing advertised, Models() = %v, want just the default", all)
	}
}

func TestModelArgsNamesTheChosenModel(t *testing.T) {
	withAliases(t, []string{"fable", "opus", "sonnet"})
	if args := ModelArgs(Model("opus")); len(args) != 2 || args[0] != "--model" || args[1] != "opus" {
		t.Errorf("ModelArgs(opus) = %v", args)
	}
	// The default is the absence of the flag, and so is anything this claude
	// does not advertise: a value it may reject is a launch that never happens.
	if args := ModelArgs(ModelDefault); args != nil {
		t.Errorf("ModelArgs(default) = %v, want no flag", args)
	}
	if args := ModelArgs(Model("haiku")); args != nil {
		t.Errorf("ModelArgs(unadvertised) = %v, want no flag", args)
	}
	withAliases(t, nil)
	if args := ModelArgs(Model("opus")); args != nil {
		t.Errorf("with nothing advertised, ModelArgs(opus) = %v, want no flag", args)
	}
}

func TestModelNormalizeAndKnown(t *testing.T) {
	withAliases(t, []string{"opus", "sonnet"})
	if got := Model("opus").Normalize(); got != "opus" {
		t.Errorf("Normalize(opus) = %q", got)
	}
	for _, in := range []Model{"", "nonsense", "OPUS"} {
		if got := in.Normalize(); got != ModelDefault {
			t.Errorf("Model(%q).Normalize() = %q, want the default", in, got)
		}
	}
	// A record written before the model existed did not choose the default —
	// it did not choose. Screens read Known, not Normalize, so they can say so.
	if Model("").Known() {
		t.Error("an unset model must not report as a model that was chosen")
	}
}

func TestModelCyclesBothWays(t *testing.T) {
	withAliases(t, []string{"opus", "sonnet"})
	all := Models()
	m := all[0]
	for range all {
		m = NextModel(m)
	}
	if m != all[0] {
		t.Errorf("cycling through every model landed on %q, want %q", m, all[0])
	}
	if got := PrevModel(all[0]); got != all[len(all)-1] {
		t.Errorf("PrevModel(%q) = %q, want the last model", all[0], got)
	}
	if got := NextModel(""); got != all[0] {
		t.Errorf("NextModel(\"\") = %q, want %q", got, all[0])
	}
}

// Each offered model says something about itself, and none of it names a key.
func TestModelHintsAreDistinctAndKeyless(t *testing.T) {
	withAliases(t, []string{"fable", "opus", "sonnet"})
	seen := map[string]bool{}
	for _, m := range Models() {
		h := m.Hint()
		if h == "" || seen[h] {
			t.Errorf("Model(%q).Hint() = %q — empty or a repeat", m, h)
		}
		seen[h] = true
		for _, key := range []string{"space", "enter", "tab"} {
			if strings.Contains(h, key) {
				t.Errorf("Model(%q).Hint() names the %q key: %q", m, key, h)
			}
		}
	}
}

// The launch and resume commands carry the flag, and carry the same one: a
// resumed dispatcher that came back on a different model than it went out on
// would be a change nobody asked for.
func TestLaunchAndResumeCarryTheModel(t *testing.T) {
	withModes(t, parseModeNames(modernHelp))
	withAliases(t, []string{"opus", "sonnet"})

	launch := launchCommand("abc123", "do the thing", ModeAuto, Model("opus"))
	if !strings.Contains(launch, "--model opus") {
		t.Errorf("launch command has no model flag:\n%s", launch)
	}
	resume := resumeCommand("abc123", "sess-1", "", ModeAuto, Model("opus"))
	if !strings.Contains(resume, "--model opus") {
		t.Errorf("resume command has no model flag:\n%s", resume)
	}

	// And the default leaves the command exactly what it always was — no
	// stray spaces where the flag would have gone.
	withModes(t, nil)
	if got := launchCommand("abc123", "go", ModeAuto, ModelDefault); strings.Contains(got, "claude  ") {
		t.Errorf("a missing flag left a double space:\n%s", got)
	}
}

// Fan-out is not a flag: it is the ultracode keyword, appended once.
func TestWithFanOutAppendsTheOptInOnce(t *testing.T) {
	if got := withFanOut("fix the bug", false); got != "fix the bug" {
		t.Errorf("fan-out off changed the prompt: %q", got)
	}
	on := withFanOut("fix the bug", true)
	if !strings.Contains(on, "ultracode") {
		t.Errorf("fan-out on carries no ultracode keyword:\n%s", on)
	}
	if !strings.HasPrefix(on, "fix the bug") {
		t.Errorf("fan-out rewrote the human's prompt:\n%s", on)
	}
	// A human who already typed the keyword has already opted in.
	already := "do this, and ULTRACODE the hard parts"
	if got := withFanOut(already, true); got != already {
		t.Errorf("the opt-in was said twice:\n%s", got)
	}
}
