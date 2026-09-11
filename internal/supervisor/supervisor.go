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
// The surface, implemented by each build-tagged file:
//
//	Available() bool                          — is the backend usable on this host
//	Backend() string                          — its name, for messages ("tmux")
//	NewSession(s, dir, shellCommand, env)     — start a detached session
//	HasSession(s) bool                        — is that session still alive
//	Sessions(socket) []string                 — every live session there, in one call
//	AttachCmd(s, env) *exec.Cmd               — hand the terminal to it
//	KillSession(s) error                      — end it
//	SendKeys(s, text) error                   — type text at its prompt + Enter
//	UniqueName(s) string                      — a session name not already taken
//	SetStatusHint(s)                          — show the way back in its status line
//	EnsureBackKey(sockets...)                 — bind the prefix-free "back" key
//	EnsureFocusEvents()                       — have the host report focus changes
//	AttachSwitches(s) bool                    — does AttachCmd exit on the way out
//	Servers() []string                        — every server found by looking
//	SessionList(socket) []SessionInfo         — its sessions, with their directories
package supervisor

// Session is where a dispatcher's session can be reached: its name, and the
// server it lives on.
//
// The name alone stopped being an address when a repo could name a socket of
// its own — "disp-login" on two sockets is two sessions, and asking the wrong
// server about one gets a confident "no such session". So the pair travels
// together, and it is on the record (state.Dispatch.TmuxSocket) rather than
// recomputed, because a socket worked out from config that has since changed is
// a dispatcher nothing can find again.
//
// Socket is empty for the default server, which is every dispatch made before
// repos could name one and every repo that does not.
type Session struct {
	Name   string
	Socket string
}

// SessionInfo is a session found by looking rather than by remembering: its
// address and the directory it runs in.
//
// The path is what makes an unknown session attributable. A session this
// cockpit did not start has no record and no dispatcher id, so where it is
// working is the only thing tying it to a repository.
type SessionInfo struct {
	Session
	Path string
}
