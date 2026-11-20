package initcmd

import (
	"runtime"
	"testing"
)

func commandEntry(matcher string, cmds ...string) any {
	inner := []any{}
	for _, c := range cmds {
		inner = append(inner, map[string]any{"type": "command", "command": c})
	}
	e := map[string]any{"hooks": inner}
	if matcher != "" {
		e["matcher"] = matcher
	}
	return e
}

func commandsFor(hooks map[string]any, event, matcher string) []string {
	var out []string
	for _, e := range hooks[event].([]any) {
		entry := e.(map[string]any)
		if m, _ := entry["matcher"].(string); m != matcher {
			continue
		}
		for _, h := range entry["hooks"].([]any) {
			out = append(out, h.(map[string]any)["command"].(string))
		}
	}
	return out
}

// The reported machine: three builds' worth of hooks, two of them collected,
// all named .claude-dispatcher-wrapped. Re-running init has to leave one set
// pointing at the durable path, not append a fourth.
func TestReconcileRepairsHooksLeftByCollectedBuilds(t *testing.T) {
	const exe = "/run/current-system/sw/bin/claude-dispatcher"
	hooks := map[string]any{}
	for _, spec := range hookSpecs() {
		var entries []any
		for _, hash := range []string{"9vzx", "w5ls", "8gk9"} {
			entries = append(entries, commandEntry(spec.matcher,
				"/nix/store/"+hash+"-claude-dispatcher-dev/bin/.claude-dispatcher-wrapped hook "+spec.arg))
		}
		prior, _ := hooks[spec.event].([]any)
		hooks[spec.event] = append(prior, entries...)
	}
	hooks["SessionStart"] = append([]any{commandEntry("", "/usr/bin/other-tool start")}, hooks["SessionStart"].([]any)...)

	ch := reconcileHooks(hooks, exe)

	n := len(hookSpecs())
	if want := (hookChanges{repointed: n, dropped: 2 * n}); ch != want {
		t.Fatalf("changes = %+v, want %+v", ch, want)
	}
	for _, spec := range hookSpecs() {
		got := commandsFor(hooks, spec.event, spec.matcher)
		if spec.event == "SessionStart" {
			if len(got) != 2 || got[0] != "/usr/bin/other-tool start" {
				t.Fatalf("SessionStart = %q, want the other tool's hook kept first", got)
			}
			got = got[1:]
		}
		if want := exe + " hook " + spec.arg; len(got) != 1 || got[0] != want {
			t.Fatalf("%s/%s = %q, want exactly %q", spec.event, spec.matcher, got, want)
		}
	}
	if again := reconcileHooks(hooks, exe); again != (hookChanges{}) {
		t.Fatalf("second run changed %+v, want nothing", again)
	}
}

// A hook of ours sharing an entry with someone else's is taken out of the entry
// and the entry kept; nothing that is not ours is ever removed.
func TestReconcileKeepsForeignHooksInASharedEntry(t *testing.T) {
	const exe = "/home/u/.local/bin/claude-dispatcher"
	hooks := map[string]any{
		"Stop": []any{commandEntry("", "/old/claude-dispatcher hook Stop", "notify-send done")},
	}

	reconcileHooks(hooks, exe)

	if got := commandsFor(hooks, "Stop", ""); len(got) != 2 || got[0] != "notify-send done" || got[1] != exe+" hook Stop" {
		t.Fatalf("Stop = %q, want the foreign hook kept and ours repointed", got)
	}
}

func TestIsOurCommand(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/home/u/.local/bin/claude-dispatcher hook Stop":                                true,
		"/nix/store/abc-claude-dispatcher-dev/bin/.claude-dispatcher-wrapped hook Stop": true,
		`C:\Program Files\claude dispatcher\claude-dispatcher.exe hook Stop`:            true,
		"/home/u/.local/bin/claude-dispatcher hook SessionEnd":                          false,
		"/usr/bin/not-claude-dispatcher hook Stop":                                      false,
		"echo claude-dispatcher hook Stop":                                              false,
	} {
		if got := isOurCommand(cmd, "Stop"); got != want {
			t.Errorf("isOurCommand(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// Our status line goes in where there is none, and wraps one the human has —
// their line still draws, on the same input — and is never installed twice.
func TestAddStatusLine(t *testing.T) {
	root := map[string]any{}
	if addStatusLine(root, "/bin/claude-dispatcher") == "" {
		t.Fatal("no change reported for a settings file without a status line")
	}
	if got := root["statusLine"].(map[string]any)["command"]; got != "/bin/claude-dispatcher statusline" {
		t.Errorf("command = %v", got)
	}
	if addStatusLine(root, "/bin/claude-dispatcher") != "" {
		t.Error("installed twice")
	}

	if runtime.GOOS == "windows" {
		return
	}
	root = map[string]any{"statusLine": map[string]any{"type": "command", "command": "bash ~/it's.sh", "padding": 2.0}}
	addStatusLine(root, "/bin/claude-dispatcher")
	sl := root["statusLine"].(map[string]any)
	if got := sl["command"]; got != `/bin/claude-dispatcher statusline --then 'bash ~/it'\''s.sh'` {
		t.Errorf("wrapped command = %v", got)
	}
	if sl["padding"] != 2.0 {
		t.Error("the human's other status line settings were dropped")

	}
}
