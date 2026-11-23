package cockpit

// actions.go turns the cockpit's key actions into real effects on the selected
// dispatcher's live record: tmux attach/kill, a real squash-merge on ship, a
// send-keys reply, and a fresh dispatch. Every action resolves the selected
// view row to its record via recordFor; with no record (still on seed data, or
// a demo row) it degrades to an honest notice instead of touching anything.

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// actionMsg carries a completed action's notice back to the UI and triggers a
// reload so the change (killed, shipped, dispatched) shows immediately.
type actionMsg struct{ notice string }

// attachReturnedMsg fires when a tmux attach hands control back.
type attachReturnedMsg struct{ err error }

// attach hands the terminal to the feature's tmux session.
//
// A row offering "attach" whose session is not there is a ghost: the record
// still claims a status only a hook could have written, and the session that
// wrote it has since been taken away. Pressing ⏎ on one is how the human finds
// out, so this does not merely refuse — it retires the record on the spot,
// through the same sweep the load runs, and says where the dispatcher went. A
// notice alone would leave the row on the table saying "working", offering the
// same key, forever.
//
// The sweep is what decides, not this function, because the rule about what
// absence proves lives there: a launching record has had no hook at all, so its
// session may simply not exist yet and nothing is claimed about it.
func (m model) attach(feature string) (model, tea.Cmd) {
	rec := recordFor(feature)
	if rec == nil {
		m.notice = "no live session for \"" + feature + "\""
		return m, nil
	}
	if !supervisor.HasSession(dispatchpkg.SessionOf(rec)) {
		if retired, _ := reconcileSessions([]*state.Dispatch{rec}); retired > 0 {
			m.notice = "\"" + feature + "\" lost its " + supervisor.Backend() +
				" session — retired · h for history, where ⏎ resumes it"
			return m.requestLoad(loadPlain)
		}
		if rec.Status == state.StatusLaunching {
			// Nothing has fired a hook for it, so its session may not exist yet
			// rather than not exist at all — the one absence that proves nothing.
			m.notice = "\"" + feature + "\" is still starting — no " + supervisor.Backend() + " session yet"
			return m, nil
		}
		m.notice = "no live " + supervisor.Backend() + " session for \"" + feature + "\""
		return m, nil
	}
	return m.attachSession(dispatchpkg.SessionOf(rec), rec.EnvCommand)
}

// attachSession hands the terminal to a session. attach resolves a feature to
// one; a resume has just started one and knows its address directly.
//
// env is the repo's own environment, and it is passed here as well as at launch
// because the human is about to open panes of their own: a split or a new
// window takes its PATH from the client they arrived through, so an attach
// without it hands them a session whose every further pane is blind to the
// toolchain the first one was given.
func (m model) attachSession(sess supervisor.Session, env string) (model, tea.Cmd) {
	if sess.Name == "" || !supervisor.HasSession(sess) {
		m.notice = "no live session to attach to"
		return m, nil
	}
	supervisor.EnsureBackKey(sess.Socket)
	supervisor.EnsureFocusEvents()
	supervisor.SetStatusHint(sess)
	// Whether this command's exit means "they are back" depends on how the
	// handover works. A plain attach owns the terminal until they detach, so it
	// exits on the way home. A switch-client (inside tmux) or a raised console
	// window (Windows) exits the instant they leave, and the only notice of
	// their return is the focus their pane or window regains — so record that
	// they are away and let that close the loop. See the attachReturnedMsg and
	// tea.FocusMsg cases in model.go.
	m.away = supervisor.AttachSwitches(sess)
	return m, tea.ExecProcess(supervisor.AttachCmd(sess, env), func(err error) tea.Msg {
		return attachReturnedMsg{err: err}
	})
}

// killCmd kills each feature's tmux session, marks the record exited, and
// reclaims its worktree. A worktree with uncommitted changes is left alone —
// killing a dispatcher must never discard unshipped work — and the notice
// says how many were kept.
func killCmd(features []string) tea.Cmd {
	return func() tea.Msg {
		n, kept, released := 0, 0, 0
		for _, f := range features {
			rec := recordFor(f)
			if rec == nil {
				continue
			}
			now := time.Now()
			// An adopted session was the human's before the record and is
			// theirs after it: x lets go of it and leaves it running. Killing
			// it would end an orchestrator this cockpit never started, along
			// with everything in its context.
			if rec.Adopted() {
				rec.Release(now)
				_ = state.Save(rec)
				released++
				continue
			}
			_ = supervisor.KillSession(dispatchpkg.SessionOf(rec))
			// The kill takes the session's subagents with it, and no hook
			// will fire to say so: settle the fan-out with the record.
			rec.SweepSubagents(now)
			// A kill is abandonment, not shelving: clear the park so the
			// record cannot haunt the parked group, whose whole claim is
			// "you will come back to this".
			rec.ParkedReason, rec.ParkedAt = "", nil
			if rec.Status != state.StatusDone {
				rec.Stop(state.StatusExited, "killed from cockpit", now)
			}
			// And it is read: the human ended this one themselves and was told
			// so. The triage table holds a finished dispatcher because an ending
			// nobody watched is news (see state.Dispatch.Held), which an ending
			// they asked for is not — unheld, it would come back as an unread ✓
			// row asking them to dismiss the thing they just killed.
			rec.Dismiss(now)
			_ = state.Save(rec)
			// An adopted dispatcher's directory is the human's own checkout,
			// which existed before the record and outlives it. OwnsWorktree is
			// the only thing standing between `x` and somebody's working copy.
			if rec.OwnsWorktree() && !dispatchpkg.CleanupWorktree(rec.RepoPath, rec.WorktreePath) {
				kept++
			}
			n++
		}
		if n == 0 && released == 0 {
			return actionMsg{notice: "nothing to kill"}
		}
		var parts []string
		if n > 0 {
			parts = append(parts, fmt.Sprintf("killed %d %s", n, plural(n, "dispatcher", "dispatchers")))
		}
		if released > 0 {
			parts = append(parts, fmt.Sprintf("released %d adopted %s, still running", released, plural(released, "session", "sessions")))
		}
		notice := strings.Join(parts, " · ")
		if kept > 0 {
			notice += fmt.Sprintf(" · %d worktree(s) kept (uncommitted changes)", kept)
		}
		return actionMsg{notice: notice}
	}
}

// shipCmd squash-merges the feature's open PR (gh pr merge --squash --auto) and
// marks the record done — "done means live".
func shipCmd(feature string) tea.Cmd {
	return func() tea.Msg {
		rec := recordFor(feature)
		if rec == nil {
			return actionMsg{notice: "nothing to ship for \"" + feature + "\""}
		}
		merged := ""
		if rec.PRNumber > 0 && rec.PRState == "OPEN" {
			cmd := exec.Command("gh", "pr", "merge", fmt.Sprintf("%d", rec.PRNumber), "--squash", "--auto")
			cmd.Dir = rec.RepoPath
			if out, err := cmd.CombinedOutput(); err != nil {
				return actionMsg{notice: "merge failed: " + firstLine(string(out))}
			}
			merged = fmt.Sprintf(" · #%d squash-merged", rec.PRNumber)
		}
		now := time.Now()
		rec.Stop(state.StatusDone, "shipped from cockpit", now)
		// Shipped by the human, so it is already read — the same rule the kill
		// follows: the held row exists for endings nobody watched.
		rec.Dismiss(now)
		_ = state.Save(rec)
		gh.InvalidateCache() // the merge just changed what the forge would say
		return actionMsg{notice: "✓ " + feature + " marked live" + merged}
	}
}

// markDoneCmd marks the feature's record done without merging (the d key).
func markDoneCmd(feature string) tea.Cmd {
	return func() tea.Msg {
		rec := recordFor(feature)
		if rec == nil {
			return actionMsg{notice: "\"" + feature + "\" has no record to mark"}
		}
		now := time.Now()
		rec.Stop(state.StatusDone, "marked shipped", now)
		rec.Dismiss(now) // the human's own ending, and so already read
		_ = state.Save(rec)
		return actionMsg{notice: "\"" + feature + "\" marked shipped"}
	}
}

// launchedMsg is a finished launch attempt, carrying the feature it was for as
// well as the notice.
//
// It is not an actionMsg, and the difference is the feature. Every other action
// works on a record that already exists, so a notice is the whole result. A
// launch is the one that starts before its record does: the cockpit has put a
// placeholder row on the table (see pending.go) and that row is now either
// wrong — nothing launched — or superseded. Neither can be settled from a
// notice string, so the outcome names what it was about.
type launchedMsg struct {
	feature string
	notice  string
	failed  bool
	// reason is the failure in the launch's own words, without the "launch
	// failed: " the notice wears. It is what the failed row says, and the row
	// outlives the notice — a footer holds one line until the next thing the
	// human does, and this is the only account of a dispatch that produced no
	// record at all.
	reason string
}

// resumedMsg carries a finished dispatcher's resume back to the UI: the notice
// to show, and the session to hand the terminal to when one was started.
// Attaching cannot happen inside the command itself — only Update may return
// tea.ExecProcess — so the command reports and Update does the handover.
type resumedMsg struct {
	notice  string
	session supervisor.Session
	env     string
	// id is the record the resume was for, so the screen lets go of it: until
	// this arrives a second ⏎ on its row starts nothing (startResume). A
	// failure comes back the same way, with no session to hand over.
	id string
}

// startResume fires a resume of record id unless one is already on its way,
// and says which.
//
// A resume can take as long as `nix develop` does — twenty seconds on the
// machine that reported this, a minute cold — and for all of it the row still
// reads "finished · ⏎ resume", because nothing has changed on disk yet. A
// second ⏎ in that time used to fire a second resume, and the two together
// could put two claudes on one conversation. Resume itself refuses that now
// (dispatch.ConversationSessions, and its lock); this is the screen not asking
// twice, and saying the first is still coming instead of saying nothing.
func (m model) startResume(id, feature, prompt string) (model, tea.Cmd) {
	if m.resuming[id] {
		m.notice = "still resuming \"" + feature + "\" — it opens as soon as its session is up"
		return m, nil
	}
	m.resuming[id] = true
	m.notice = "resuming \"" + feature + "\"…"
	return m, resumeCmd(id, prompt)
}

// resumeCmd reopens a finished dispatcher's Claude session, with prompt as the
// first thing said to it when there is one.
//
// The record is addressed by id, not by feature: a feature name can belong to
// several finished dispatchers (re-dispatching a shipped feature is normal, and
// deliberately reuses the name), so the feature map would resolve the wrong one.
//
// And it is read from disk, not from the snapshot (recordByID): a snapshot is
// as old as the load that read it, and Resume decides from the record whether
// its session is still there. A copy from before an earlier resume names a
// session that resume has since replaced — the copy that put a second claude on
// one conversation — and Resume then saves that copy back over everything the
// hooks have written since. swapCmd reads from disk for the same reason.
func resumeCmd(id, prompt string) tea.Cmd {
	return func() tea.Msg {
		rec := state.Load(id)
		if rec == nil {
			return resumedMsg{id: id, notice: "no dispatch record to resume"}
		}
		mode, session, err := resumeDispatch(rec, strings.TrimSpace(prompt))
		if errors.Is(err, dispatchpkg.ErrResuming) {
			// Another process — a second cockpit, a swap — is starting it.
			return resumedMsg{id: id, notice: "\"" + rec.Feature + "\" is already being resumed — it opens as soon as its session is up"}
		}
		if err != nil {
			return resumedMsg{id: id, notice: "resume failed: " + err.Error()}
		}
		if mode == dispatchpkg.ResumeLive {
			// Its claude is still up, so the human lands in the conversation
			// itself rather than in a second one reading the same transcript.
			// Anything they typed here has not been said to it: sending keys
			// into a session that may be mid-turn, or sitting on a permission
			// prompt, would answer the wrong question.
			notice := "\"" + rec.Feature + "\" is still running — attaching to it"
			if strings.TrimSpace(prompt) != "" {
				notice += " · say it there, it was not sent"
			}
			notice += twinNotice(rec, session)
			return resumedMsg{id: id, notice: notice, session: resumedAt(rec, session), env: rec.EnvCommand}
		}
		if mode == dispatchpkg.ResumeRelaunched {
			return resumedMsg{
				id:      id,
				notice:  "started \"" + rec.Feature + "\" again in " + rec.RepoName + " · it never ran, so there was nothing to resume",
				session: resumedAt(rec, session),
				env:     rec.EnvCommand,
			}
		}
		return resumedMsg{
			id:      id,
			notice:  "resumed \"" + rec.Feature + "\" in " + rec.RepoName,
			session: resumedAt(rec, session),
			env:     rec.EnvCommand,
		}
	}
}

// resumedAt is where a resume left its session: the name Resume answered with,
// on the server the record lives on. Resume may have taken a new name — the old
// one can be occupied by the shell the finished session dropped to — but never a
// new server, because the repo it reopens in is the one it ran in.
func resumedAt(rec *state.Dispatch, name string) supervisor.Session {
	return supervisor.Session{Name: name, Socket: rec.TmuxSocket}
}

// twinNotice names the other sessions running rec's conversation besides the
// one the human is being sent to — "" when there are none, which is every time
// but one. That one is two claudes on one transcript, left by a resume from
// before Resume asked the machine, or by a human resuming it by hand: each
// takes the other for a separate session, and only the human can say which
// goes. So the notice says where the other is, and nothing ends it.
func twinNotice(rec *state.Dispatch, attached string) string {
	var others []string
	for _, s := range conversationSessions(rec) {
		if s.Name != attached {
			others = append(others, s.Name)
		}
	}
	if len(others) == 0 {
		return ""
	}
	return " · its conversation is also running in " + strings.Join(others, ", ") + " — end the one you do not want"
}

// resumeDispatch and conversationSessions are the seams resumeCmd reaches the
// machine through, so a test can see which record a resume was handed and
// say where a conversation runs without a tmux server of its own.
var (
	resumeDispatch       = dispatchpkg.Resume
	conversationSessions = dispatchpkg.ConversationSessions
)

// launchCmd dispatches a new feature into repoName with prompt, in the
// permission mode the form chose, on the model it chose, cut from the root
// branch it chose, fanning out across agents if the form asked for that.
//
// acctName is the account it runs under, resolved here against the config
// rather than carried as a struct from the form, so an account removed between
// the form and the launch is a launch that says so instead of one that runs on
// a directory nobody configured any more.
//
// prefix is the branch prefix for this one dispatch, as the form showed it —
// settings' default unless the human changed it there. nil is the paths that
// ask nothing (the backlog's enter, the product panel's re-dispatch): they cut
// with the repo's configured prefix, as they always have.
func launchCmd(cfg *config.Config, repoName, feature, prompt string, mode dispatchpkg.Mode, mdl dispatchpkg.Model, root dispatchpkg.Root, prefix *string, fanOut bool, acctName string) tea.Cmd {
	return func() tea.Msg {
		if cfg == nil {
			return launchFailed(feature, "no config — cannot dispatch")
		}
		acct, ok := account.Find(cfg, acctName)
		if !ok {
			return launchFailed(feature, "no account named "+acctName+" — claude-dispatcher account")
		}
		var found *repos.Repo
		for _, r := range repos.Discover(cfg) {
			if r.Name == repoName {
				rr := r
				found = &rr
				break
			}
		}
		if found == nil {
			return launchFailed(feature, "repo not found: "+repoName)
		}
		if prefix != nil {
			p := strings.TrimSpace(*prefix)
			found.BranchPrefix = &p
		}
		d, err := dispatchpkg.Launch(*found, feature, prompt, mode, mdl, root, fanOut, acct)
		if err != nil {
			return launchFailed(feature, err.Error())
		}
		return launchedMsg{feature: feature, notice: "dispatched \"" + d.Feature + "\" → " + d.RepoName}
	}
}

// launchDispatch is the seam both dispatch forms hand off through, named as a
// variable so a test can read what a form actually passes without starting a
// real dispatcher.
//
// The hand-off is where the truncated prompt got out, and it was invisible to
// every test the dx form had: those assert the form's own state, and the form's
// state was correct — m.dxWhat held the whole sentence the entire time. Only the
// two arguments leaving that function were wrong, and nothing looked at them.
// The `+` overlay goes through the same seam for the same reason: the choice
// that matters is the one that leaves the form, not the one on its screen.
var launchDispatch = launchCmd

// launchFailed is the one place a failed dispatch is reported from, so the
// footer and the row it leaves behind can never say different things. The
// notice wears the prefix; the row carries the reason as it was given.
func launchFailed(feature, reason string) launchedMsg {
	return launchedMsg{
		feature: feature,
		notice:  "launch failed: " + firstLine(reason),
		failed:  true,
		reason:  reason,
	}
}

// firstLine returns the first line of s (mirrors transcript.firstLine locally).
func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// openPRCmd opens a pull request in the browser via gh. The lens used to print
// "opening #144" and open nothing — a notice describing an action that never
// happened, the same defect the backlog's ctrl+d had.
func openPRCmd(repoName, pr string) tea.Cmd {
	return func() tea.Msg {
		num := strings.TrimLeft(pr, "#!")
		if num == "" {
			return actionMsg{notice: "no pull request to open"}
		}
		var repoPath string
		for _, r := range lastDiscovered {
			if r.Name == repoName {
				repoPath = r.Path
				break
			}
		}
		if repoPath == "" {
			return actionMsg{notice: "cannot find " + repoName + " to open " + pr}
		}
		cmd := exec.Command("gh", "pr", "view", num, "--web")
		cmd.Dir = repoPath
		if out, err := cmd.CombinedOutput(); err != nil {
			return actionMsg{notice: "could not open " + pr + ": " + firstLine(string(out))}
		}
		return actionMsg{notice: "opened " + pr + " in the browser"}
	}
}
