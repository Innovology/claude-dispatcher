package dispatch

// conversation.go answers the question Resume has to ask before it starts a
// claude: is this conversation already running?
//
// The record cannot answer it. A record names the session it last started, and
// a copy of a record is as old as whatever read it: the cockpit acts on the
// copy its last load read, and a load is slow — seconds warm, a minute cold,
// which is what every load is just after a reboot. Measured on the report that
// led here: ⏎ on a finished row resumed it under `nix develop`, a load that had
// read the records before that resume saved landed after it and put the row
// back to "finished · ⏎ resume", and the second ⏎ resumed the record as it was
// before the first. The session the first resume started was, to that copy, a
// younger one that had taken the name, so it was left alone and a new name was
// taken — two `claude --resume` of one conversation in one worktree, twenty
// seconds apart. Both reported to the one record, which followed the second;
// both wrote to the one transcript; and the two claudes found each other over
// Claude Code's own messaging and settled between them which of them ran the
// CI, each believing the other was a separate session.
//
// So the machine is asked. Claude Code's registry (`sessions/<pid>.json`, the
// file adoption reads — see BindSession) names the conversation each live
// claude is running, and the processes under each pane of the repo's server
// are where a dispatcher's claude can be. A conversation found there is
// running, whatever the record says, and starting another claude on it is the
// one thing Resume must never do.

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// serverPIDs is the seam the scan reads a server through: every session on it
// with the processes running in its panes.
var serverPIDs = supervisor.ServerPIDs

// ConversationSessions is every session on d's server in which a claude is
// running d's conversation, in name order. Nothing for a record with no
// conversation, or a backend that cannot see into its sessions.
//
// One is the dispatcher's session. Two is the defect this file exists for,
// and the cockpit names the other when it attaches to one (see resumeCmd).
func ConversationSessions(d *state.Dispatch) []supervisor.Session {
	if d == nil {
		return nil
	}
	sid := SessionIDOf(d)
	if sid == "" {
		return nil
	}
	bySession := serverPIDs(d.TmuxSocket)
	names := make([]string, 0, len(bySession))
	for name := range bySession {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []supervisor.Session
	for _, name := range names {
		if runsConversation(bySession[name], sid, d.ConfigDir) {
			out = append(out, supervisor.Session{Name: name, Socket: d.TmuxSocket})
		}
	}
	return out
}

// runsConversation reports whether any of pids is a claude running conversation
// sid: by Claude Code's registry, or by the transcript a process holds open.
func runsConversation(pids []int, sid, configDir string) bool {
	for _, pid := range pids {
		if registrySessionID(pid, configDir) == sid {
			return true
		}
		for _, id := range openTranscripts(pid) {
			if id == sid {
				return true
			}
		}
	}
	return false
}

// runningConversation is where d's conversation is running, preferring the
// session the record already names when it is among them; false when it runs
// nowhere on d's server.
func runningConversation(d *state.Dispatch) (supervisor.Session, bool) {
	found := ConversationSessions(d)
	if len(found) == 0 {
		return supervisor.Session{}, false
	}
	for _, s := range found {
		if s.Name == d.TmuxSession {
			return s, true
		}
	}
	return found[0], true
}

// ownSessionName reports whether name is one Launch or Resume would have given
// d's session: `disp-<slug>`, or that with the `-N` UniqueName adds when the
// name is taken. Only a session so named is claimed for the record — one a
// human started on the same server (a wrapper names its sessions after the
// repo and branch) is attached to and left theirs, since a record that claimed
// it would end it on `x`.
func ownSessionName(d *state.Dispatch, name string) bool {
	base := "disp-" + d.Slug
	if d.Slug == "" {
		base = "disp-" + d.ID
	}
	if name == base {
		return true
	}
	// UniqueName counts from 2 and writes the number plainly, so "-1", "-02"
	// and "-x" are not its.
	n, ok := strings.CutPrefix(name, base+"-")
	i, err := strconv.Atoi(n)
	return ok && err == nil && i >= 2 && strconv.Itoa(i) == n
}

// claimSession points d at the session its conversation was found running in,
// on disk and in d: the record must name where its claude is, or the ghost
// sweep retires a dispatcher that is running and every ⏎ resumes it again.
//
// Written to the record as it stands on disk, under the hook lock, with only
// the address changed — the copy Resume was handed may be the stale one that
// started all this, and saving it whole would put its status back over what
// the hooks have said since. The hold is stamped no later than the session was
// made, so the session reads as the one this record took hold of
// (HoldsSessionBorn) rather than a younger one that has its name.
func claimSession(d *state.Dispatch, at supervisor.Session) {
	var born time.Time
	for _, s := range sessionList(at.Socket) {
		if s.Name == at.Name {
			born = s.Created
			break
		}
	}
	apply := func(r *state.Dispatch) {
		r.TmuxSession = at.Name
		if !born.IsZero() && !r.HoldsSessionBorn(born) {
			b := born
			r.SessionStartedAt = &b
		}
	}
	release := state.Lock()
	defer release()
	if cur := state.Load(d.ID); cur != nil {
		apply(cur)
		_ = state.Save(cur)
	}
	apply(d)
}
