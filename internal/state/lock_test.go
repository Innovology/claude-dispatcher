package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A named lock is held across holders: a second taker waits until the first
// lets go, and a different name is a different lock.
func TestLockNamedSerialisesItsHolders(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	release := LockNamed("steward.lock")
	if _, err := os.Stat(filepath.Join(Dir(), "steward.lock")); err != nil {
		t.Fatalf("the lock is not a file of its own in the state dir: %v", err)
	}

	other := make(chan struct{})
	go func() { LockNamed("other.lock")(); close(other) }()
	select {
	case <-other:
	case <-time.After(5 * time.Second):
		t.Fatal("a lock of another name waited on this one")
	}

	got := make(chan struct{})
	go func() { LockNamed("steward.lock")(); close(got) }()
	select {
	case <-got:
		t.Fatal("a second holder took the lock while the first still had it")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("the second holder never got the lock after the first let go")
	}
}

// A lock that cannot be taken is a no-op rather than a stall: the callers
// proceed as they did before there was one.
func TestLockNamedFailsOpen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_DISPATCHER_STATE", file)
	LockNamed("steward.lock")()
}
