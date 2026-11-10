package dispatch

// StewardLaunch is what a steward session is started with (internal/steward):
// the conversation it is, whether it is new or reopened, the name sessions
// address it by, and its first message.
//
// The session id is chosen here rather than learned from a hook because the
// steward is not a dispatcher and its hooks are attributed to nothing: a
// conversation started with --session-id is one that --resume can reopen by
// that same id, which is the whole of "reopen an older steward".
type StewardLaunch struct {
	SessionID string
	// Resume reopens SessionID's conversation instead of starting it.
	Resume bool
	// Name is claude's --name: the session's display name, and the name
	// other Claude sessions on the machine message it by.
	Name    string
	Opening string
}

// stewardArgs is the session flags as a leading-space fragment, quoted with q.
func stewardArgs(l StewardLaunch, q func(string) string) string {
	out := ""
	if l.SessionID != "" {
		if l.Resume {
			out += " --resume " + q(l.SessionID)
		} else {
			out += " --session-id " + q(l.SessionID)
		}
	}
	if l.Name != "" {
		out += " --name " + q(l.Name)
	}
	return out
}
