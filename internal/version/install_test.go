package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noNixProfile is the selector for a store path that is in no imperative
// profile — the declarative case.
func noNixProfile(string) (string, bool) { return "", false }

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		want   Method
		cmd    string
		canUpg bool
	}{
		{
			name: "homebrew cask", // what the tap actually ships
			path: "/opt/homebrew/Caskroom/claude-dispatcher/3.1.2/claude-dispatcher",
			want: MethodBrewCask, cmd: "brew upgrade --cask claude-dispatcher", canUpg: true,
		},
		{
			name: "intel mac cask",
			path: "/usr/local/Caskroom/claude-dispatcher/3.1.2/claude-dispatcher",
			want: MethodBrewCask, cmd: "brew upgrade --cask claude-dispatcher", canUpg: true,
		},
		{
			name: "homebrew formula",
			path: "/opt/homebrew/Cellar/claude-dispatcher/3.1.2/bin/claude-dispatcher",
			want: MethodBrewFormula, cmd: "brew upgrade claude-dispatcher", canUpg: true,
		},
		{
			// What a WSL2 install actually is: the tap ships one cask, and
			// goreleaser gives it an `on_linux` block, so Linuxbrew stages it
			// under its own Caskroom and `U` must offer the cask command there
			// too — not the formula one it would get by guessing from GOOS.
			name: "linuxbrew cask",
			path: "/home/linuxbrew/.linuxbrew/Caskroom/claude-dispatcher/3.1.2/claude-dispatcher",
			want: MethodBrewCask, cmd: "brew upgrade --cask claude-dispatcher", canUpg: true,
		},
		{
			name: "linuxbrew formula",
			path: "/home/linuxbrew/.linuxbrew/Cellar/claude-dispatcher/3.1.2/bin/claude-dispatcher",
			want: MethodBrewFormula, cmd: "brew upgrade claude-dispatcher", canUpg: true,
		},
		{
			name: "nix, not in any imperative profile",
			path: "/nix/store/abc123-claude-dispatcher-3.1.2/bin/claude-dispatcher",
			want: MethodNixManaged, canUpg: false,
		},
		{
			name: "nix store somewhere other than /nix (chroot or single-user)",
			path: "/home/alex/.local/share/nix/root/nix/store/abc-claude-dispatcher-3.1.2/bin/claude-dispatcher",
			want: MethodNixManaged, canUpg: false,
		},
		{
			name: "scoop",
			path: `C:\Users\alex\scoop\apps\claude-dispatcher\current\claude-dispatcher.exe`,
			want: MethodScoop, cmd: "scoop update claude-dispatcher", canUpg: true,
		},
		{
			name: "winget",
			path: `C:\Users\alex\AppData\Local\Microsoft\WinGet\Packages\Innovology.claude-dispatcher_x\claude-dispatcher.exe`,
			want: MethodWinget, cmd: "winget upgrade --id Innovology.claude-dispatcher", canUpg: true,
		},
		{
			name: "a binary someone built and copied",
			path: "/home/alex/.local/bin/claude-dispatcher",
			want: MethodUnknown, canUpg: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classify(c.path, noNixProfile)
			if got.Method != c.want {
				t.Errorf("method = %q, want %q", got.Method, c.want)
			}
			if got.CanUpgrade() != c.canUpg {
				t.Errorf("CanUpgrade() = %v, want %v", got.CanUpgrade(), c.canUpg)
			}
			if c.canUpg && strings.Join(got.Cmd, " ") != c.cmd {
				t.Errorf("cmd = %q, want %q", strings.Join(got.Cmd, " "), c.cmd)
			}
			// An install we will not upgrade still has to say something: the
			// footer prints Hint() either way, and an empty clause reads as a
			// rendering bug rather than as a decision.
			if !c.canUpg && got.Hint() == "" {
				t.Error("an install with no command must still explain itself")
			}
		})
	}
}

// The whole point of the Nix branch: a store path is only upgradable in place
// when the imperative profile manifest claims it. Anything else is declared in
// a file somewhere, and `nix profile upgrade` would either fail or install a
// second copy shadowing the declared one.
func TestClassifyNixImperative(t *testing.T) {
	const path = "/nix/store/abc123-claude-dispatcher-3.1.2/bin/claude-dispatcher"
	got := classify(path, func(string) (string, bool) { return "claude-dispatcher", true })
	if got.Method != MethodNixProfile {
		t.Fatalf("method = %q, want %q", got.Method, MethodNixProfile)
	}
	if want := "nix profile upgrade claude-dispatcher"; strings.Join(got.Cmd, " ") != want {
		t.Errorf("cmd = %q, want %q", strings.Join(got.Cmd, " "), want)
	}
}

// manifest.json has had two shapes and both are still on people's machines:
// v1/v2 name an element by its index, v3 by a key.
func TestManifestSelector(t *testing.T) {
	const resolved = "/nix/store/abc123-claude-dispatcher-3.1.2/bin/claude-dispatcher"

	v3 := []byte(`{"version":3,"elements":{
		"ripgrep":{"storePaths":["/nix/store/zzz-ripgrep-14"]},
		"claude-dispatcher":{"storePaths":["/nix/store/abc123-claude-dispatcher-3.1.2"]}}}`)
	if sel, ok := manifestSelector(v3, resolved); !ok || sel != "claude-dispatcher" {
		t.Errorf("v3: got (%q, %v), want (claude-dispatcher, true)", sel, ok)
	}

	v2 := []byte(`{"version":2,"elements":[
		{"storePaths":["/nix/store/zzz-ripgrep-14"]},
		{"storePaths":["/nix/store/abc123-claude-dispatcher-3.1.2"]}]}`)
	if sel, ok := manifestSelector(v2, resolved); !ok || sel != "1" {
		t.Errorf("v2: got (%q, %v), want (1, true)", sel, ok)
	}

	// A manifest that does not carry this binary must not claim it — this is
	// the check standing between a home-manager install and an imperative
	// upgrade run against it.
	other := []byte(`{"version":3,"elements":{"ripgrep":{"storePaths":["/nix/store/zzz-ripgrep-14"]}}}`)
	if sel, ok := manifestSelector(other, resolved); ok {
		t.Errorf("a foreign manifest claimed this binary as %q", sel)
	}
	if _, ok := manifestSelector([]byte("not json"), resolved); ok {
		t.Error("unreadable manifest must not claim anything")
	}

	// A store path must match on a path boundary, never as a bare prefix:
	// …-claude-dispatcher-3.1.2 must not be satisfied by …-claude-dispatcher-3
	// on the way past.
	near := []byte(`{"version":3,"elements":{"x":{"storePaths":["/nix/store/abc123-claude-dispatcher-3"]}}}`)
	if _, ok := manifestSelector(near, resolved); ok {
		t.Error("a different store path matched on a bare prefix")
	}
}

// Homebrew skips its own tap refresh on a machine that fetched recently, which
// would make "upgrade me now" a no-op seconds after a release. The child env
// has to force the refresh, and must not disturb anything else.
func TestEnvForcesHomebrewRefresh(t *testing.T) {
	t.Setenv("HOMEBREW_NO_AUTO_UPDATE", "1")
	t.Setenv("HOMEBREW_AUTO_UPDATE_SECS", "86400")
	t.Setenv("CLAUDE_DISPATCHER_STATE", "/tmp/keep-me")

	env := Install{Method: MethodBrewCask}.Env()
	var secs int
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOMEBREW_NO_AUTO_UPDATE=") {
			t.Errorf("HOMEBREW_NO_AUTO_UPDATE survived as %q", kv)
		}
		if kv == "HOMEBREW_AUTO_UPDATE_SECS=0" {
			secs++
		} else if strings.HasPrefix(kv, "HOMEBREW_AUTO_UPDATE_SECS=") {
			t.Errorf("stale refresh window survived as %q", kv)
		}
	}
	if secs != 1 {
		t.Errorf("HOMEBREW_AUTO_UPDATE_SECS=0 appears %d times, want 1", secs)
	}
	if !contains(env, "CLAUDE_DISPATCHER_STATE=/tmp/keep-me") {
		t.Error("the rest of the environment must pass through untouched")
	}

	// Nothing to force anywhere else: nix, scoop and winget all fetch on every
	// invocation.
	if nix := (Install{Method: MethodNixProfile}).Env(); len(nix) != len(os.Environ()) {
		t.Error("a non-homebrew install must inherit the environment unchanged")
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// Detect runs against whatever built the test binary, so it cannot assert a
// method — only that it always answers, never panics, and never hands the
// cockpit a half-formed offer.
func TestDetectAlwaysAnswers(t *testing.T) {
	in := Detect()
	if in.CanUpgrade() {
		if len(in.Cmd) < 2 {
			t.Errorf("an upgrade command needs a verb: %q", in.Cmd)
		}
	} else if in.Hint() == "" {
		t.Error("Hint must explain why there is no command")
	}
	if in.Hint() != UpgradeHint() {
		t.Error("UpgradeHint must be Detect().Hint()")
	}
}

// nixProfileSelector reads real files; point it at a temp profile and check it
// both finds and refuses.
func TestNixProfileSelectorReadsManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")

	profile := filepath.Join(home, ".nix-profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	const resolved = "/nix/store/abc123-claude-dispatcher-3.1.2/bin/claude-dispatcher"
	if _, ok := nixProfileSelector(resolved); ok {
		t.Error("no manifest yet, nothing may be claimed")
	}

	manifest := `{"version":3,"elements":{"claude-dispatcher":{"storePaths":["/nix/store/abc123-claude-dispatcher-3.1.2"]}}}`
	if err := os.WriteFile(filepath.Join(profile, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	sel, ok := nixProfileSelector(resolved)
	if !ok || sel != "claude-dispatcher" {
		t.Errorf("got (%q, %v), want (claude-dispatcher, true)", sel, ok)
	}
}
