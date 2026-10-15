package cockpit

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Every hook event from every session is a record write, so the state dir's
// watcher sees bursts. They reach the cockpit as one pending signal however
// many there were, and a cockpit busy with a load must never stall the
// forwarder: it drains the burst without anyone reading, and it ends when the
// watcher closes rather than sitting on a send for the rest of the process.
func TestABurstOfRecordWritesIsOneSignalAndNeverBlocks(t *testing.T) {
	dir := t.TempDir()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Skipf("no file watcher here: %v", err)
	}
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		t.Skipf("cannot watch %s: %v", dir, err)
	}
	ch := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() { forwardEvents(w, ch); close(done) }()

	for i := range 20 {
		if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(i)+".json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("twenty writes raised no signal")
	}

	_ = w.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the forwarder outlived its watcher")
	}
}
