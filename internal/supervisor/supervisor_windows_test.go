//go:build windows

package supervisor

import (
	"reflect"
	"testing"
)

func TestBackend(t *testing.T) {
	if got := Backend(); got != "windows-console" {
		t.Errorf("Backend() = %q, want windows-console", got)
	}
	if !Available() {
		t.Error("Available() = false, want true on Windows")
	}
}

func TestUniqueNameFree(t *testing.T) {
	// Isolate the registry so no session is ever "taken".
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if got := UniqueName("disp-fresh"); got != "disp-fresh" {
		t.Errorf("UniqueName of a free name = %q, want disp-fresh", got)
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	want := map[string]int{"disp-a": 111, "disp-b": 222}
	if err := saveRegistry(want); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}
	got := loadRegistry()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadRegistry() = %v, want %v", got, want)
	}
}

func TestLoadRegistryMissingIsEmpty(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if got := loadRegistry(); len(got) != 0 {
		t.Errorf("loadRegistry() on missing file = %v, want empty", got)
	}
}
