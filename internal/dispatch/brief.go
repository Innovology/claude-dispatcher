package dispatch

import (
	"time"

	"claude-dispatcher/internal/state"
)

// brief.go hands an adopted session the one convention a dispatched one is
// launched with.
//
// A dispatch carries its working contract in the prompt (contract.go): wait on
// slow things with a background task, take the next step rather than offering
// it, and **end your message on the one question you need answered**. That
// last sentence is what makes a wait visible — the cockpit reads status from
// hooks, and a turn that ends is the hook that says "your move".
//
// A session started outside the cockpit never saw any of it, and cannot be
// given it the way a dispatch is: the prompt was typed hours ago by somebody
// else. Adoption without it is half a migration — the record is there, the
// hooks land on it, and the session goes on asking mid-turn where nothing can
// report it. So the convention is sent as a message, the way the human would
// send it.

// AdoptBrief is what an adopted session is told. Two sentences, because it is
// arriving in the middle of somebody's work and every extra line is context
// taken from the thing they were doing.
//
// It asks for the signal, not for a change of behaviour: keep working the way
// you work, and when you need me, end the turn there. The question tool is
// named too, because that is the other shape the cockpit can see (a menu
// fires PreToolUse) and an orchestrator that prefers options should know it
// is understood.
const AdoptBrief = "This session is now on a dispatch cockpit, which reads your status from " +
	"Claude Code's hooks. Keep working as you have been; the one thing it needs is that when " +
	"you want something from me, you end your message on that question rather than asking " +
	"mid-turn and carrying on — a menu through the question tool works too. A turn that ends " +
	"is what puts your row in front of me."

// BriefPending reports that an adopted dispatcher has not yet been handed the
// convention. A released record is nobody's to brief.
func BriefPending(d *state.Dispatch) bool {
	return d.Adopted() && d.AdoptBriefAt == nil
}

// DeliverBriefs types the brief into every adopted session still owed it, and
// reports how many landed.
//
// The rules are RetryFailed's, for the same reasons and one more. The session
// must be live and claude provably still at its prompt — an unknown answer is
// a skip, because text typed into a pane whose claude has gone lands in the
// shell. It must not be blocked: a blocked session is sitting on a menu or a
// permission prompt, and typing at a menu PICKS one of its options, which is
// the one way this could do real harm. And it must not be mid-turn: a brief
// queued behind an hour of work arrives detached from the moment it explains,
// so it waits for a turn to end, which is exactly the state it is about.
//
// It is claimed under the hook lock against a fresh read, so a brief is sent
// once even if two loads race, and a send that fails is not retried in a tight
// loop — the stamp is spent either way, because a session that cannot be typed
// into now is one the human will have to tell themselves.
func DeliverBriefs(ds []*state.Dispatch, now time.Time) (sent int) {
	if !supervisorReady() {
		return 0
	}
	for _, d := range ds {
		if !BriefPending(d) || d.TmuxSession == "" {
			continue
		}
		if d.Status != state.StatusNeedsInput {
			continue // working, blocked, or finished: not a moment to interrupt
		}
		if !sessionAlive(SessionOf(d)) {
			continue
		}
		if idle, known := sessionIdle(SessionOf(d)); idle || !known {
			continue
		}
		rec, ok := claimBrief(d.ID, now)
		if !ok {
			continue
		}
		if sendKeys(SessionOf(rec), AdoptBrief) == nil {
			sent++
		}
	}
	return sent
}

// claimBrief stamps the record under the hook lock and against a fresh read,
// so the brief is spent once. It returns the record the keys should go to, or
// false when somebody got there first.
func claimBrief(id string, now time.Time) (*state.Dispatch, bool) {
	release := state.Lock()
	defer release()
	for _, d := range state.LoadAll() {
		if d.ID != id {
			continue
		}
		if !BriefPending(d) {
			return nil, false
		}
		d.AdoptBriefAt = &now
		if state.Save(d) != nil {
			return nil, false
		}
		return d, true
	}
	return nil, false
}
