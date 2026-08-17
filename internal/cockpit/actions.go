package cockpit

// actions.go turns the cockpit's key actions into real effects on the selected
// dispatcher's live record: tmux attach/kill, a real squash-merge on ship, a
// send-keys reply, and a fresh dispatch. Every action resolves the selected
// view row to its record via recordFor; with no record (still on seed data, or
// a demo row) it degrades to an honest notice instead of touching anything.

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
func (m model) attach(feature string) (model, tea.Cmd) {
	rec := recordFor(feature)
	if rec == nil {
		m.notice = "no live session for \"" + feature + "\""
		return m, nil
	}
	if !supervisor.HasSession(rec.TmuxSession) {
		m.notice = "no live tmux session for \"" + feature + "\""
		return m, nil
	}
	return m.attachSession(rec.TmuxSession)
}

// attachSession hands the terminal to a named session. attach resolves a
// feature to one; a resume has just started one and knows its name directly.
func (m model) attachSession(name string) (model, tea.Cmd) {
	if name == "" || !supervisor.HasSession(name) {
		m.notice = "no live session to attach to"
		return m, nil
	}
	supervisor.EnsureBackKey()
	supervisor.EnsureFocusEvents()
	supervisor.SetStatusHint(name)
	// Whether this command's exit means "they are back" depends on how the
	// handover works. A plain attach owns the terminal until they detach, so it
	// exits on the way home. A switch-client (inside tmux) or a raised console
	// window (Windows) exits the instant they leave, and the only notice of
	// their return is the focus their pane or window regains — so record that
	// they are away and let that close the loop. See the attachReturnedMsg and
	// tea.FocusMsg cases in model.go.
	m.away = supervisor.AttachSwitches()
	return m, tea.ExecProcess(supervisor.AttachCmd(name), func(err error) tea.Msg {
		return attachReturnedMsg{err: err}
	})
}

// killCmd kills each feature's tmux session, marks the record exited, and
// reclaims its worktree. A worktree with uncommitted changes is left alone —
// killing a dispatcher must never discard unshipped work — and the notice
// says how many were kept.
func killCmd(features []string) tea.Cmd {
	return func() tea.Msg {
		n, kept := 0, 0
		for _, f := range features {
			rec := recordFor(f)
			if rec == nil {
				continue
			}
			_ = supervisor.KillSession(rec.TmuxSession)
			if rec.Parked() || rec.Status != state.StatusDone {
				// A kill is abandonment, not shelving: clear the park so the
				// record cannot haunt the parked group, whose whole claim is
				// "you will come back to this".
				rec.ParkedReason, rec.ParkedAt = "", nil
				if rec.Status != state.StatusDone {
					rec.Status = state.StatusExited
					rec.StatusReason = "killed from cockpit"
				}
				_ = state.Save(rec)
			}
			if rec.WorktreePath != "" && !dispatchpkg.CleanupWorktree(rec.RepoPath, rec.WorktreePath) {
				kept++
			}
			n++
		}
		if n == 0 {
			return actionMsg{notice: "nothing to kill"}
		}
		word := "dispatcher"
		if n != 1 {
			word += "s"
		}
		notice := fmt.Sprintf("killed %d %s", n, word)
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
		rec.Status = state.StatusDone
		rec.StatusReason = "shipped from cockpit"
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
		rec.Status = state.StatusDone
		rec.StatusReason = "marked shipped"
		_ = state.Save(rec)
		return actionMsg{notice: "\"" + feature + "\" marked shipped"}
	}
}

// replyCmd sends text into the feature's live session, as if typed at the prompt.
func replyCmd(feature, text string) tea.Cmd {
	return func() tea.Msg {
		rec := recordFor(feature)
		if rec == nil || !supervisor.HasSession(rec.TmuxSession) {
			return actionMsg{notice: "no live session to reply to"}
		}
		_ = supervisor.SendKeys(rec.TmuxSession, text)
		return actionMsg{notice: "replied to \"" + feature + "\" · session resumed"}
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
}

// resumedMsg carries a finished dispatcher's resume back to the UI: the notice
// to show, and the session to hand the terminal to when one was started.
// Attaching cannot happen inside the command itself — only Update may return
// tea.ExecProcess — so the command reports and Update does the handover.
type resumedMsg struct {
	notice  string
	session string
}

// resumeCmd reopens a finished dispatcher's Claude session, with prompt as the
// first thing said to it when there is one.
//
// The record is addressed by id, not by feature: a feature name can belong to
// several finished dispatchers (re-dispatching a shipped feature is normal, and
// deliberately reuses the name), so the feature map would resolve the wrong one.
func resumeCmd(id, prompt string) tea.Cmd {
	return func() tea.Msg {
		rec := recordByID(id)
		if rec == nil {
			return actionMsg{notice: "no dispatch record to resume"}
		}
		mode, session, err := dispatchpkg.Resume(rec, strings.TrimSpace(prompt))
		if err != nil {
			return actionMsg{notice: "resume failed: " + err.Error()}
		}
		if mode == dispatchpkg.ResumeLive {
			return resumedMsg{
				notice:  "\"" + rec.Feature + "\" is still running — attaching to it",
				session: session,
			}
		}
		return resumedMsg{
			notice:  "resumed \"" + rec.Feature + "\" in " + rec.RepoName,
			session: session,
		}
	}
}

// launchCmd dispatches a new feature into repoName with prompt, in the
// permission mode the form chose, on the model it chose, fanning out across
// agents if the form asked for that.
func launchCmd(cfg *config.Config, repoName, feature, prompt string, mode dispatchpkg.Mode, mdl dispatchpkg.Model, fanOut bool) tea.Cmd {
	return func() tea.Msg {
		if cfg == nil {
			return launchedMsg{feature: feature, notice: "no config — cannot dispatch", failed: true}
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
			return launchedMsg{feature: feature, notice: "repo not found: " + repoName, failed: true}
		}
		d, err := dispatchpkg.Launch(*found, feature, prompt, mode, mdl, fanOut)
		if err != nil {
			return launchedMsg{feature: feature, notice: "launch failed: " + err.Error(), failed: true}
		}
		return launchedMsg{feature: feature, notice: "dispatched \"" + d.Feature + "\" → " + d.RepoName}
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
