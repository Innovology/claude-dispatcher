package dispatch

// swap.go moves a dispatcher onto another Claude subscription without losing
// its conversation — the thing to do when the one it runs on has run out.
//
// A conversation belongs to a config directory (ADR 0022): its transcript is
// under that directory's projects/, and `claude --resume <id>` started under
// any other answers "No conversation found" — measured on 2.1.281, the same
// session id resuming fine there once its transcript had been copied across.
// So a swap is a copy and a resume: the conversation goes into the new
// account's directory under the same project folder (the folder is named for
// the working directory, which does not change), the old claude is ended, and
// Resume starts the same session id under the new login.
//
// It is a copy, never a move. The old directory's transcript is what that
// login actually spent, the human's own history in it, and the way back if the
// new account refuses too.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-dispatcher/internal/account"
	"claude-dispatcher/internal/state"
)

// SwapOffered reports, from the record alone, whether a swap could be asked
// of d: the screen offers the key where it is, and SwapAccount asks the
// session itself before acting. A turn in progress is not offered — working,
// launching, or on a menu, which is a turn waiting on a keypress — because
// ending claude there throws away the turn it is in the middle of.
func SwapOffered(d *state.Dispatch) bool {
	if d == nil || d.AdoptedAt != nil {
		return false
	}
	if SessionIDOf(d) == "" && strings.TrimSpace(d.Prompt) == "" {
		return false
	}
	switch d.Status {
	case state.StatusNeedsInput, state.StatusExited, state.StatusDone:
		return true
	}
	return false
}

// SwapAccount reopens d's conversation under to, and returns what Resume did
// and the session it is in. prompt is the first thing said to the resumed
// session; "" sends "continue" when the last turn was ended by an API error —
// which is how running out of credits arrives — and nothing otherwise, since
// a turn that ended on a question must not have it answered for the human.
//
// Refused, with nothing changed: an adopted session (it is the human's and
// not ours to end — ADR 0026), the account it is already on, an account that
// would sit unattended (account.Problems, the same refusal a launch makes),
// and a session whose claude is mid-turn.
func SwapAccount(d *state.Dispatch, to account.Account, prompt string) (ResumeMode, string, error) {
	if d == nil {
		return "", "", fmt.Errorf("no dispatch record")
	}
	if d.AdoptedAt != nil {
		return "", "", fmt.Errorf("%q is a session you adopted — it is not ours to restart; /login in it instead", d.Feature)
	}
	if filepath.Clean(to.Env()) == filepath.Clean(d.ConfigDir) {
		return "", "", fmt.Errorf("%q is already on %s", d.Feature, to.Name)
	}
	if problems := account.Problems(to, probeAccount(to)); len(problems) > 0 {
		return "", "", fmt.Errorf("the %s account cannot run it: %s", to.Name, strings.Join(problems, "; "))
	}
	if err := swapTurnOver(d); err != nil {
		return "", "", err
	}

	from := accountLabel(d.Account)
	sid := SessionIDOf(d)
	newTranscript := ""
	if sid != "" {
		p, err := copyConversation(d, sid, to.ConfigDir())
		if err != nil {
			return "", "", err
		}
		newTranscript = p
	}

	// The old claude goes now, and only now that the copy is made: it was at
	// its prompt (swapTurnOver), and a transcript it went on writing after the
	// copy would be a turn the new session never sees.
	if d.TmuxSession != "" && sessionAlive(SessionOf(d)) && holdsOwnSession(d) {
		_ = killSession(SessionOf(d))
	}

	if prompt == "" && d.Failure != nil && sid != "" {
		prompt = RetryText
	}
	d.Account = accountName(to)
	d.ConfigDir = to.Env()
	if newTranscript != "" {
		d.TranscriptPath = newTranscript
	}
	// Trust lives in each login's own .claude.json; without it the resumed
	// session opens on the trust prompt with nobody there to answer it.
	if dir := worktreeOrRepo(d); dir != "" {
		InheritTrustFor(d.ConfigDir, d.RepoPath, dir)
	}
	mode, name, err := Resume(d, prompt)
	if err != nil {
		return mode, name, err
	}
	state.AppendEvent(state.Event{
		Event: state.EventAccountSwapped, DispatcherID: d.ID, Feature: d.Feature,
		Repo: d.RepoName, Cwd: worktreeOrRepo(d), Reason: from + " → " + to.Name,
	})
	return mode, name, nil
}

// swapTurnOver is nil when ending d's claude loses nothing: there is no
// session, claude has exited in it, or the hooks say its turn has ended. A
// done record says nothing about the turn — the tracker marks a merged PR
// done while its session works on — so there the last message has to be on
// the record, which a Stop sets and the next prompt clears.
func swapTurnOver(d *state.Dispatch) error {
	if d.TmuxSession == "" || !sessionAlive(SessionOf(d)) || !holdsOwnSession(d) {
		return nil
	}
	if idle, known := sessionIdle(SessionOf(d)); known && idle {
		return nil
	}
	switch {
	case d.Status == state.StatusNeedsInput, d.Status == state.StatusExited:
		return nil
	case d.Status == state.StatusDone && d.Said != "":
		return nil
	case d.Status == state.StatusBlocked:
		return fmt.Errorf("%q is on a menu in its session — answer it, or attach and /exit, then switch", d.Feature)
	}
	return fmt.Errorf("%q is mid-turn — switch once it stops, or attach and /exit it first", d.Feature)
}

// copyConversation copies session sid's transcript, the folder of subagent
// transcripts and tool results beside it, and its file checkpoints, from the
// config directory it ran under into toDir, and returns the transcript's new
// path. The project folder keeps its name: Claude Code names it for the
// working directory, which a swap does not change.
func copyConversation(d *state.Dispatch, sid, toDir string) (string, error) {
	src := d.TranscriptPath
	if src == "" {
		src = findTranscript(configDirOf(d), sid)
	}
	if src == "" {
		return "", fmt.Errorf("no transcript for %q to take across", d.Feature)
	}
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("the transcript for %q is gone (%s)", d.Feature, src)
	}
	projDir := filepath.Dir(src)
	fromRoot := filepath.Dir(filepath.Dir(projDir))
	dst := filepath.Join(toDir, "projects", filepath.Base(projDir), filepath.Base(src))
	if filepath.Clean(dst) == filepath.Clean(src) {
		return src, nil
	}
	if err := copyFile(src, dst); err != nil {
		return "", fmt.Errorf("copy the transcript for %q: %w", d.Feature, err)
	}
	// Best effort past the transcript itself: --resume needs only that, and a
	// subagent's log or a rewind checkpoint that did not come across is a
	// detail lost, not a conversation.
	_ = copyTree(filepath.Join(projDir, sid), filepath.Join(filepath.Dir(dst), sid))
	_ = copyTree(filepath.Join(fromRoot, "file-history", sid), filepath.Join(toDir, "file-history", sid))
	return dst, nil
}

// configDirOf is the directory d's session reads: its account's, or the
// default login's.
func configDirOf(d *state.Dispatch) string {
	if d.ConfigDir != "" {
		return d.ConfigDir
	}
	return account.DefaultConfigDir()
}

// findTranscript is sid's transcript under dir, for a record that never had
// its path reported — the newest, should two project folders hold one.
func findTranscript(dir, sid string) string {
	hits, _ := filepath.Glob(filepath.Join(dir, "projects", "*", sid+".jsonl"))
	best, bestAt := "", time.Time{}
	for _, h := range hits {
		if fi, err := os.Stat(h); err == nil && fi.ModTime().After(bestAt) {
			best, bestAt = h, fi.ModTime()
		}
	}
	return best
}

// copyFile writes src over dst through a temporary file and a rename, so a
// session resuming from dst never reads half a transcript.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".swap-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// copyTree copies every regular file under src into dst. A missing src is
// nothing to copy.
func copyTree(src, dst string) error {
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return nil
	}
	return filepath.WalkDir(src, func(p string, e os.DirEntry, err error) error {
		if err != nil || !e.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		return copyFile(p, filepath.Join(dst, rel))
	})
}

func worktreeOrRepo(d *state.Dispatch) string {
	if d.WorktreePath != "" {
		return d.WorktreePath
	}
	return d.RepoPath
}

// accountLabel is a record's account as a person reads it.
func accountLabel(name string) string {
	if name == "" {
		return account.Default
	}
	return name
}
