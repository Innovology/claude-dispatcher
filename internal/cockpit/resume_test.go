package cockpit

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"claude-dispatcher/internal/config"
	dispatchpkg "claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/supervisor"
)

// stubResume stands in for dispatch.Resume and records the copy of the record
// each resume was handed.
func stubResume(t *testing.T, mode dispatchpkg.ResumeMode, session string, err error) *[]state.Dispatch {
	t.Helper()
	var handed []state.Dispatch
	prev := resumeDispatch
	resumeDispatch = func(d *state.Dispatch, prompt string) (dispatchpkg.ResumeMode, string, error) {
		handed = append(handed, *d)
		return mode, session, err
	}
	t.Cleanup(func() { resumeDispatch = prev })
	return &handed
}

// The report, from the cockpit's side: the snapshot still held the record from
// before the first resume, and the second ⏎ resumed that copy. A resume is
// handed the record as it is on disk.
func TestAResumeIsHandedTheRecordOnDiskNotTheScreens(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	onDisk := &state.Dispatch{ID: "074a9f870c8e", Feature: "one button production publish",
		TmuxSession: "disp-one-button-production-publish", TmuxSocket: "player-app", Status: state.StatusWorking}
	if err := state.Save(onDisk); err != nil {
		t.Fatal(err)
	}
	stale := *onDisk
	stale.TmuxSession, stale.Status = "disp-old", state.StatusExited
	prev := liveByID
	liveByID = map[string]*state.Dispatch{stale.ID: &stale}
	t.Cleanup(func() { liveByID = prev })
	handed := stubResume(t, dispatchpkg.ResumeStarted, "disp-one-button-production-publish", nil)

	msg := resumeCmd(onDisk.ID, "")().(resumedMsg)
	if len(*handed) != 1 || (*handed)[0].TmuxSession != onDisk.TmuxSession || (*handed)[0].Status != state.StatusWorking {
		t.Fatalf("Resume was handed %+v, want the record on disk", *handed)
	}
	if msg.id != onDisk.ID || msg.session != (supervisor.Session{Name: "disp-one-button-production-publish", Socket: "player-app"}) {
		t.Errorf("resumedMsg = %+v", msg)
	}

	// A record that is not on disk is not resumed from a copy that is.
	if msg := resumeCmd("gone", "")().(resumedMsg); msg.id != "gone" || msg.notice != "no dispatch record to resume" {
		t.Errorf("resumedMsg = %+v", msg)
	}
	if len(*handed) != 1 {
		t.Error("resumed a record that is not on disk")
	}
}

// ⏎ twice on a row whose resume is still starting fires one resume, and the
// second says the first is coming. Its answer lets go of the row, success or
// failure, so a later ⏎ resumes (or attaches) again.
func TestASecondEnterWhileResumingStartsNothing(t *testing.T) {
	m := newModel()
	m.cfg = &config.Config{}
	m, first := m.startResume("abc", "retry", "")
	if first == nil || !m.resuming["abc"] || m.notice != "resuming \"retry\"…" {
		t.Fatalf("first ⏎: cmd %v, resuming %v, notice %q", first != nil, m.resuming["abc"], m.notice)
	}
	m, second := m.startResume("abc", "retry", "")
	if second != nil {
		t.Error("the second ⏎ fired another resume")
	}
	if !strings.HasPrefix(m.notice, "still resuming \"retry\"") {
		t.Errorf("notice = %q, want it to say the first is still coming", m.notice)
	}
	if _, other := m.startResume("def", "other", ""); other == nil {
		t.Error("a resume of another record was held back")
	}

	next, _ := m.Update(resumedMsg{id: "abc", notice: "resume failed: boom"})
	m = next.(model)
	if m.resuming["abc"] {
		t.Error("the row is still marked resuming after its answer came back")
	}
	if m.notice != "resume failed: boom" {
		t.Errorf("notice = %q, want the resume's own", m.notice)
	}
}

// Refused or failed, the answer names the record so the row is let go of, and
// says what happened in the resume's words.
func TestAResumeThatDidNotStartSaysWhy(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if err := state.Save(&state.Dispatch{ID: "abc", Feature: "retry"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: elsewhere", dispatchpkg.ErrResuming), "\"retry\" is already being resumed"},
		{errors.New("the transcript is gone"), "resume failed: the transcript is gone"},
	} {
		stubResume(t, "", "", tc.err)
		msg := resumeCmd("abc", "")().(resumedMsg)
		if msg.id != "abc" || !strings.HasPrefix(msg.notice, tc.want) || msg.session.Name != "" {
			t.Errorf("resumedMsg = %+v, want %q and nothing to attach", msg, tc.want)
		}
	}
}
