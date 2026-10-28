//go:build !windows

package cockpit

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A .md that is not a regular file is not read: a FIFO stats at size 0 and
// then blocks the read forever, which would hold up every load after it.
func TestDcnFileRecordsSkipsWhatIsNotAFile(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "DECISIONS.md"), 0o600); err != nil {
		t.Skipf("no fifo here: %v", err)
	}
	done := make(chan []decision, 1)
	go func() { done <- dcnFileRecords(root, "DECISIONS.md") }()
	select {
	case recs := <-done:
		if len(recs) != 0 {
			t.Errorf("read %d records out of a fifo", len(recs))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reading a fifo named .md blocked the load")
	}
}
