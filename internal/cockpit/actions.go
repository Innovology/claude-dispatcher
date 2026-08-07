package cockpit

// actions.go turns the cockpit's key actions into real effects on the selected
// dispatcher's live record: tmux attach/kill, a real squash-merge on ship, a
// send-keys reply, and a fresh dispatch. Every action resolves the selected
// view row to its record via recordFor; with no record (still on seed data, or
// a demo row) it degrades to an honest notice instead of touching anything.

import (
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
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
	supervisor.EnsureBackKey()
	supervisor.SetStatusHint(rec.TmuxSession)
	return m, tea.ExecProcess(supervisor.AttachCmd(rec.TmuxSession), func(err error) tea.Msg {
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
			if rec.Status != state.StatusDone {
				rec.Status = state.StatusExited
				rec.StatusReason = "killed from cockpit"
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

// launchCmd dispatches a new feature into repoName with prompt.
func launchCmd(cfg *config.Config, repoName, feature, prompt string) tea.Cmd {
	return func() tea.Msg {
		if cfg == nil {
			return actionMsg{notice: "no config — cannot dispatch"}
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
			return actionMsg{notice: "repo not found: " + repoName}
		}
		d, err := dispatchpkg.Launch(*found, feature, prompt)
		if err != nil {
			return actionMsg{notice: "launch failed: " + err.Error()}
		}
		return actionMsg{notice: "dispatched \"" + d.Feature + "\" → " + d.RepoName}
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
