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

// One holder at a time, and a refusal rather than a wait for the second;
// released, the lock is free again. Different names are different locks.
func TestTryLockRefusesASecondHolderAndFreesOnRelease(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	release, ok := TryLock("resume-abc")
	if !ok {
		t.Fatal("the first holder was refused")
	}
	if _, ok := TryLock("resume-abc"); ok {
		t.Error("a second holder got the lock while the first had it")
	}
	other, ok := TryLock("resume-def")
	if !ok {
		t.Error("another record's lock was refused")
	}
	other()
	release()
	again, ok := TryLock("resume-abc")
	if !ok {
		t.Error("the lock stayed taken after its release")
	}
	again()
}

// A lock that cannot be made is no holder: the caller goes ahead as it did
// before there was a lock, the rule Lock follows.
func TestTryLockThatCannotBeMadeIsNoHolder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	if err := os.WriteFile(filepath.Join(dir, "locks"), []byte("a file, not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	release, ok := TryLock("resume-abc")
	if !ok {
		t.Error("an unmakeable lock refused the caller")
	}
	release()
}
