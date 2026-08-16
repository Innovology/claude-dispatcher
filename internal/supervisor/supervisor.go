// Package supervisor is the OS abstraction over the process supervisor that
// hosts dispatcher sessions: a detached, persistent shell per dispatch that the
// cockpit can attach to, kill, and send input to.
//
// It is the one Unix-specific seam in the app. On Unix the backend is tmux
// (supervisor_unix.go); a Windows backend (ConPTY + a session manager) will
// slot in behind the same surface via supervisor_windows.go, so nothing else in
// the codebase needs to know which platform it is on.
//
// The surface, implemented by each build-tagged file:
//
//	Available() bool                         — is the backend usable on this host
//	Backend() string                         — its name, for messages ("tmux")
//	NewSession(name, dir, shellCommand) error — start a detached session
//	HasSession(name) bool                    — is that session still alive
//	Sessions() []string                      — every live session, in one call
//	AttachCmd(name) *exec.Cmd                — hand the terminal to it
//	KillSession(name) error                  — end it
//	SendKeys(name, text) error               — type text at its prompt + Enter
//	UniqueName(base) string                  — a session name not already taken
//	SetStatusHint(name)                      — show the way back in its status line
//	EnsureBackKey()                          — bind the prefix-free "back" key
//	EnsureFocusEvents()                      — have the host report focus changes
//	AttachSwitches() bool                    — does AttachCmd exit on the way out
package supervisor
