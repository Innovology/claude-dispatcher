package state

import (
	"os"
	"path/filepath"
	"testing"
)

// Load reads the one record asked for, as it is on disk now, and nothing for an
// id that has none or that is not a record's id at all.
func TestLoadReadsOneRecordAsItIsOnDisk(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if err := Save(&Dispatch{ID: "abc", Feature: "x", TmuxSession: "disp-x"}); err != nil {
		t.Fatal(err)
	}
	// Another writer moves it on, as Resume does while a screen still holds
	// the copy from before.
	if err := Save(&Dispatch{ID: "abc", Feature: "x", TmuxSession: "disp-x-2"}); err != nil {
		t.Fatal(err)
	}
	if d := Load("abc"); d == nil || d.TmuxSession != "disp-x-2" {
		t.Errorf("Load = %+v, want the record as last saved", d)
	}
	for _, id := range []string{"", "nope", "../abc", `..\abc`} {
		if d := Load(id); d != nil {
			t.Errorf("Load(%q) = %+v, want nothing", id, d)
		}
	}
	// A file whose id is not its name is not that record.
	if err := os.WriteFile(filepath.Join(DispatchesDir(), "other.json"), []byte(`{"id":"abc"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := Load("other"); d != nil {
		t.Errorf("Load read a record under another id's name: %+v", d)
	}
}
