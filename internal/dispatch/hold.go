package dispatch

// hold.go is when a session younger than a record's hold is the record's own
// after all.
//
// A record's hold (state.Dispatch.SessionStartedAt) is how the sweep and Resume
// tell the session a record took hold of from a younger one that has taken its
// name since (HoldsSessionBorn). It is a proxy, and a proxy can be wrong: the
// hold is a field on a record, and a copy of the record saved back over a
// resume's write puts back the hold of a session the resume replaced. Measured
// on 7b388dcc30f3: its relaunch at 14:35:00 made the session, an older copy was
// saved over the stamp, and the record held 14:32:21 from then on — so every
// load read its own live session as a stranger's and retired it, every hook put
// it back to working, and the row flickered between running and finished for
// two hours until the session went quiet watching CI and the finished stuck.
// ⏎ could not bring it back either: Resume asked the same proxy.
//
// So where the proxy says "stranger", the machine is asked the question the
// proxy stands in for: is that session running this record's conversation?
// Claude Code's own registry names the conversation each live claude runs (the
// file adoption reads — see BindSession), so a session running the record's
// conversation is the record's, whenever it was made, and the hold is what was
// wrong. It is asked only there — a named session younger than the hold — which
// is no record at all on a fleet whose holds are right.

import (
	"time"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// sessionRunsConversation reports whether a claude under sess's panes is
// running conversation sid. A seam, so tests need no claude of their own.
var sessionRunsConversation = func(sess supervisor.Session, sid string) bool {
	if sid == "" {
		return false
	}
	for _, pid := range paneChildren(sess) {
		one := []int{pid}
		if sessionIDFromRegistry(one) == sid || sessionIDFromProc(one) == sid {
			return true
		}
	}
	return false
}

// runsOwnConversation reports whether the session at d's address is running
// d's conversation. A record with no conversation yet has nothing to find, and
// a released one has let its session go and claims nothing.
func runsOwnConversation(d *state.Dispatch) bool {
	if d == nil || d.ReleasedAt != nil {
		return false
	}
	sid := SessionIDOf(d)
	return sid != "" && sessionRunsConversation(SessionOf(d), sid)
}

// restampHold makes d hold the session at its address, created at born: the
// hold moves to the session's birth, so HoldsSessionBorn reads it as the one d
// took hold of. Written to the record as it stands on disk, and only while it
// still names that address with a hold the session is younger than — a resume
// that has moved it on since is the newer word. d is left as the record now is.
func restampHold(d *state.Dispatch, born time.Time) {
	at := SessionOf(d)
	cur, _ := state.Edit(d.ID, func(cur *state.Dispatch) bool {
		if SessionOf(cur) != at || cur.HoldsSessionBorn(born) {
			return false
		}
		b := born
		cur.SessionStartedAt = &b
		return true
	})
	if cur != nil {
		*d = *cur
	}
}
