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

// TryLock takes the lock called name — one of many, unlike Lock's one — if no
// other holder has it, and returns the release. ok is false only when somebody
// else holds it right now.
//
// It is for the act that must not happen twice at once and that takes too long
// to queue behind: a resume holds its record's lock across a `nix develop` that
// can run for a minute, and a second resume of the same conversation waiting
// on it would only start a second claude when it got its turn. So the second
// is refused rather than made to wait.
//
// Best-effort like Lock: a lock file that cannot be made or asked about is not
// a holder, so the caller proceeds as it did before there was a lock. The files
// are left in place — removing one another process may have open lets two
// holders each lock a different file of the same name.
func TryLock(name string) (release func(), ok bool) {
	noop := func() {}
	dir := filepath.Join(Dir(), "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return noop, true
	}
	f, err := os.OpenFile(filepath.Join(dir, name+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return noop, true
	}
	if held, err := tryLockFile(f); err != nil {
		_ = f.Close()
		return noop, !held
	}
	return func() {
		unlockFile(f)
		_ = f.Close()
	}, true
}
