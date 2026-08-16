package cockpit

// quota.go says the one thing about the forge that no lens can say for itself.
//
// When GitHub locks us out for the rest of the hour, every gh read degrades to
// no signal, exactly as it does for a repo with no CI and a machine with no
// network — so the screen fills with "—" in the check columns and empty review
// queues, and every one of those reads as a fact about the repositories. It is
// not. It is a fact about us, and the human needs it: it is why the cockpit
// looks quiet, and it is why their own `gh` has stopped working in the terminal
// next door.

import (
	"strings"

	"claude-dispatcher/internal/gh"
)

// quotaNotice prefixes the footer line so it can recognise — and retire — its
// own message without disturbing anything else that lands there.
const quotaNotice = "github api quota spent — forge signals paused, retrying in "

// noteQuota keeps the footer honest about the lockout, ambiently: it yields to
// anything the human just did (a merge, a dispatch, an error), and it takes
// itself down when the window resets rather than sitting there after the fact.
func (m model) noteQuota() model {
	until, throttled := gh.Throttled()
	switch {
	case throttled && (m.notice == "" || strings.HasPrefix(m.notice, quotaNotice)):
		m.notice = quotaNotice + gh.ThrottledFor(until)
	case !throttled && strings.HasPrefix(m.notice, quotaNotice):
		m.notice = ""
	}
	return m
}
