package state

import (
	"os"
	"path/filepath"
)

// Lock serialises a load-modify-save of dispatch records against other hook
// processes, and returns the release. Hook events used to arrive one at a
// time — the session's main thread emits them serially — but a fan-out fires
// SubagentStart/SubagentStop from parallel agents, and two unserialised
// hookcmds doing LoadAll→apply→Save last-writer-wins each other's events away:
// a straggler's save can resurrect entries a Stop just swept, or put back a
// status the turn's end already moved past.
//
// It is best-effort by the same rule as everything else in the hook path: a
// lock that cannot be taken must never stall a live Claude session, so any
// failure returns a no-op release and the caller proceeds unlocked — exactly
// the pre-lock behaviour, no worse.
func Lock() (release func()) {
	if err := EnsureDirs(); err != nil {
		return func() {}
	}
	f, err := os.OpenFile(filepath.Join(Dir(), "hook.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return func() {}
	}
	return func() {
		unlockFile(f)
		_ = f.Close()
	}
}
