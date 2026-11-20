package initcmd

import (
	"os"
	"path/filepath"
	"testing"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
)

// A `go run` or `go test` build lives in a directory the toolchain deletes, so
// a hook baked to it would stop firing the moment the build is cleaned up.
// Installing from one says so and writes nothing — into no account at all.
func TestATemporaryBuildInstallsNothing(t *testing.T) {
	exe, err := installExe()
	if err != nil || exe != "" {
		t.Fatalf("a test binary offered itself as the hook's binary: %q, %v", exe, err)
	}
	dir := t.TempDir()
	if err := InstallFor(account.Account{Name: "work", Dir: dir}); err != nil {
		t.Fatalf("InstallFor: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	if err := installAll(&config.Config{}); err != nil {
		t.Fatalf("installAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("a temporary build wrote settings.json into the account: %v", err)
	}
}
