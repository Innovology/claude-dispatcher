package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatusPriorityOrdering(t *testing.T) {
	order := []Status{StatusBlocked, StatusNeedsInput, StatusLaunching,
		StatusWorking, StatusExited, StatusDone}
	for i := 1; i < len(order); i++ {
		if order[i-1].Priority() >= order[i].Priority() {
			t.Errorf("%s should outrank %s", order[i-1], order[i])
		}
	}
}

func TestSaveLoadAllSortsByUrgency(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	for _, d := range []*Dispatch{
		{ID: "w", Feature: "working", Status: StatusWorking},
		{ID: "b", Feature: "blocked", Status: StatusBlocked},
		{ID: "d", Feature: "done", Status: StatusDone},
		{ID: "n", Feature: "needs", Status: StatusNeedsInput},
	} {
		if err := Save(d); err != nil {
			t.Fatal(err)
		}
	}
	got := LoadAll()
	if len(got) != 4 {
		t.Fatalf("expected 4 records, got %d", len(got))
	}
	wantOrder := []string{"b", "n", "w", "d"}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Errorf("position %d: got %s, want %s", i, got[i].ID, id)
		}
	}
}

// The permission mode has to survive the round trip, and an unset one has to
// stay unset. Resume reads Mode off the record to reopen a dispatcher the way
// it went out; a dropped field would silently bring a manual or plan
// dispatcher back in auto, which is a permission change nobody asked for. A
// record from before the mode was a choice carries none, and inventing one
// here would be claiming a session ran in a mode nobody picked.
func TestSaveLoadKeepsTheMode(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	if err := Save(&Dispatch{ID: "m", Feature: "planned", Mode: "plan", Status: StatusWorking}); err != nil {
		t.Fatal(err)
	}
	if err := Save(&Dispatch{ID: "o", Feature: "older", Status: StatusWorking}); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range LoadAll() {
		got[d.ID] = d.Mode
	}
	if got["m"] != "plan" {
		t.Errorf("mode did not survive the round trip: %q", got["m"])
	}
	if got["o"] != "" {
		t.Errorf("a record that never chose a mode came back as %q", got["o"])
	}
}

func TestSaveStampsUpdatedAt(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	d := &Dispatch{ID: "x", Status: StatusWorking}
	before := time.Now()
	if err := Save(d); err != nil {
		t.Fatal(err)
	}
	if d.UpdatedAt.Before(before) {
		t.Error("UpdatedAt not stamped on save")
	}
}

func TestLoadAllSkipsCorruptRecords(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	if err := Save(&Dispatch{ID: "ok", Status: StatusWorking}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, DispatchesDir()+"/broken.json", "{not json")
	writeFile(t, DispatchesDir()+"/empty-id.json", "{}")
	got := LoadAll()
	if len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("expected only the valid record, got %d", len(got))
	}
}

// The shelf has to survive the round trip: the cockpit that reads the record
// back may be a different process days later, and Resume, the fleet and the
// hooks all read Parked() off what was persisted.
func TestSaveLoadKeepsThePark(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	at := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := Save(&Dispatch{ID: "p", Status: StatusNeedsInput,
		ParkedReason: "waiting on legal", ParkedAt: &at}); err != nil {
		t.Fatal(err)
	}
	if err := Save(&Dispatch{ID: "q", Status: StatusNeedsInput}); err != nil {
		t.Fatal(err)
	}
	got := map[string]*Dispatch{}
	for _, d := range LoadAll() {
		got[d.ID] = d
	}
	p := got["p"]
	if !p.Parked() || p.ParkedReason != "waiting on legal" || p.ParkedAt == nil || !p.ParkedAt.Equal(at) {
		t.Errorf("the park did not survive the round trip: %+v", p)
	}
	if got["q"].Parked() {
		t.Error("a record nobody parked came back parked")
	}
}

func TestFailureTransient(t *testing.T) {
	for e, want := range map[string]bool{
		"overloaded": true, "server_error": true, "unknown": true, "max_output_tokens": true,
		"rate_limit": false, "authentication_failed": false, "billing_error": false,
		"invalid_request": false, "model_not_found": false,
	} {
		if got := (&Failure{Error: e}).Transient(); got != want {
			t.Errorf("%s: got %v want %v", e, got, want)
		}
	}
	if (*Failure)(nil).Transient() {
		t.Error("no failure is not a transient one")
	}
}

// A long message keeps its end, because the ask is at the end.
func TestSetSaidKeepsTheEnd(t *testing.T) {
	var d Dispatch
	d.SetSaid(strings.Repeat("x", MaxSaid+10) + " Want me to merge it?")
	if !strings.HasSuffix(d.Said, "Want me to merge it?") || !strings.HasPrefix(d.Said, "…") {
		t.Fatalf("got …%q", d.Said[len(d.Said)-30:])
	}
	if n := len([]rune(d.Said)); n != MaxSaid+1 {
		t.Fatalf("len %d", n)
	}
}

// FinishedAt means "the moment it stopped", so only the transition stamps it:
// the tracker re-writing a merged PR's fields months later is not a second
// ending, and a record re-stamped by it would be held on the table again.
func TestStopStampsTheEndingOnceAndHoldsItUntilDismissed(t *testing.T) {
	start := time.Now()
	d := &Dispatch{Status: StatusWorking}
	if d.Held() {
		t.Fatal("a running dispatcher is not a held finish")
	}
	d.Stop(StatusExited, "session ended", start)
	if d.FinishedAt == nil || !d.FinishedAt.Equal(start) {
		t.Fatalf("the ending was not stamped at the transition: %v", d.FinishedAt)
	}
	if !d.Held() {
		t.Fatal("a dispatcher that finished under this build must hold its row")
	}
	d.Stop(StatusDone, "deployed", start.Add(time.Hour))
	if !d.FinishedAt.Equal(start) {
		t.Errorf("a second stop moved the ending to %v", d.FinishedAt)
	}
	if d.Status != StatusDone || d.StatusReason != "deployed" {
		t.Errorf("the second stop's status was not recorded: %s %q", d.Status, d.StatusReason)
	}
	d.Dismiss(start.Add(2 * time.Hour))
	if d.Held() {
		t.Error("a dismissed finish is still held")
	}
}

// A record that ended before FinishedAt existed carries no stamp, and shipping
// the hold must not flush a year of history onto the fleet.
func TestAFinishFromBeforeTheStampIsNotHeld(t *testing.T) {
	if (&Dispatch{Status: StatusDone}).Held() {
		t.Error("an unstamped finish was held")
	}
}

// A resumed dispatcher must come back clean: the next save of a live status
// drops the old ending and the human's reading of it, or the row would sit
// under "finished" while the session works.
func TestSavingALiveStatusClearsTheEnding(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", t.TempDir())
	now := time.Now()
	d := &Dispatch{ID: "r", Status: StatusWorking}
	d.Stop(StatusExited, "session ended", now)
	d.Dismiss(now)
	if err := Save(d); err != nil {
		t.Fatal(err)
	}
	if d.FinishedAt == nil || d.DismissedAt == nil {
		t.Fatal("saving a finished record dropped its ending")
	}
	d.Status = StatusWorking
	if err := Save(d); err != nil {
		t.Fatal(err)
	}
	if got := LoadAll()[0]; got.FinishedAt != nil || got.DismissedAt != nil {
		t.Errorf("a working record kept its ending: finished %v dismissed %v", got.FinishedAt, got.DismissedAt)
	}
}

// An adopted session's directory is the human's own checkout. Every caller that
// deletes a folder or offers a kill asks these first, so they must say no for
// an adopted record and yes only for a worktree this cockpit cut.
func TestAnAdoptedRecordOwnsNothing(t *testing.T) {
	now := time.Now()
	var none *Dispatch
	if none.Adopted() || none.OwnsWorktree() {
		t.Fatal("no record claims nothing")
	}
	cut := &Dispatch{WorktreePath: "/wt/feature"}
	if cut.Adopted() || !cut.OwnsWorktree() {
		t.Fatal("a dispatched worktree is ours to remove")
	}
	if (&Dispatch{}).OwnsWorktree() {
		t.Fatal("a record with no worktree owns none")
	}
	adopted := &Dispatch{WorktreePath: "/home/me/project", AdoptedAt: &now}
	if !adopted.Adopted() || adopted.OwnsWorktree() {
		t.Fatal("an adopted record must claim its session and not its directory")
	}
}

// Releasing gives the session back: the record stops answering its hooks, ends,
// and goes to history read — it does not sit on the table asking to be
// dismissed, and the directory stays the human's after it.
func TestReleasingGivesTheSessionBack(t *testing.T) {
	now := time.Now()
	d := &Dispatch{Status: StatusWorking, SessionID: "77ac6360", TranscriptPath: "/t.jsonl",
		WorktreePath: "/home/me/project", AdoptedAt: &now}
	d.Release(now)
	if d.Adopted() {
		t.Error("a released record still claims its session")
	}
	if d.SessionID != "" {
		t.Error("a released record still answers to its session's hooks")
	}
	if d.TranscriptPath != "/t.jsonl" {
		t.Error("the transcript is history and must stay")
	}
	if d.Status != StatusExited || !strings.Contains(d.StatusReason, "released") {
		t.Errorf("status = %s %q, want exited and saying it was released", d.Status, d.StatusReason)
	}
	if d.Held() {
		t.Error("a release is an ending the human asked for; it must not be held")
	}
	if d.OwnsWorktree() {
		t.Error("releasing must not hand the human's directory to the reclaimer")
	}
}

// A steward note and a reply only speak for the wait they were written in. One
// left over from an earlier wait would mark a new question handled and the
// steward would never wake for it.
func TestANoteOrAnAnswerOnlyCountsForTheCurrentWait(t *testing.T) {
	since := time.Now()
	before, after := since.Add(-time.Minute), since.Add(time.Minute)

	d := &Dispatch{Status: StatusNeedsInput, WaitingSince: &since}
	if d.Handled() {
		t.Fatal("a wait nobody touched is not handled")
	}
	d.StewardNote, d.StewardNoteAt = "yours: merge needs you", &after
	if d.Note() != "yours: merge needs you" {
		t.Fatalf("note = %q", d.Note())
	}
	if !d.Handled() {
		t.Fatal("a note written during the wait handles it")
	}
	d.StewardNoteAt = &before
	if d.Note() != "" || d.Handled() {
		t.Fatal("a note from an earlier wait handled this one")
	}

	d.Answer, d.AnsweredAt = "yes, merge it", &after
	if d.Answered() != "yes, merge it" || !d.Handled() {
		t.Fatalf("an answer typed into the wait did not handle it: %q", d.Answered())
	}
	d.AnsweredAt = &before
	if d.Answered() != "" || d.Handled() {
		t.Fatal("an answer to an earlier wait handled this one")
	}

	// A record that is not waiting has no current wait to speak for.
	d.AnsweredAt, d.StewardNoteAt = &after, &after
	d.Status = StatusWorking
	if d.Waiting() || d.Note() != "" || d.Answered() != "" || d.Handled() {
		t.Error("a working record reads as a handled wait")
	}
	d.Status = StatusBlocked
	if !d.Waiting() || !d.Handled() {
		t.Error("a permission prompt is a wait too")
	}
	// Without WaitingSince there is nothing to be earlier than.
	d.WaitingSince = nil
	if d.Note() == "" || d.Answered() == "" {
		t.Error("a note and an answer with no wait start to compare against were dropped")
	}
}

// The prompt file is the transport the launch command reads, so it must hold
// exactly what was dispatched, and a resume's opening message lives beside it
// under its own name rather than over it.
func TestAPromptIsWrittenWhereTheLaunchReadsIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	path, err := WritePrompt("abc123", "do the thing\n'quoted' $HOME")
	if err != nil {
		t.Fatal(err)
	}
	if path != PromptPath("abc123") || filepath.Dir(path) != PromptsDir() || !strings.HasPrefix(path, dir) {
		t.Fatalf("prompt written to %s, want %s under the state dir", path, PromptPath("abc123"))
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "do the thing\n'quoted' $HOME" {
		t.Fatalf("prompt file holds %q (%v)", b, err)
	}
	if PromptPath("abc123.resume") == path {
		t.Error("a resume's message would overwrite the launch prompt")
	}
	if WorktreesDir() != filepath.Join(dir, "worktrees") {
		t.Errorf("worktrees dir = %s", WorktreesDir())
	}
}

// With no override the state lives where the docs say it does.
func TestTheStateDirDefaultsUnderHome(t *testing.T) {
	t.Setenv("CLAUDE_DISPATCHER_STATE", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	want := filepath.Join(home, ".local", "state", "claude-dispatcher")
	if Dir() != want {
		t.Errorf("Dir() = %s, want %s", Dir(), want)
	}
}

// A state dir that cannot be made is an error for the writes a launch depends
// on, and silence for the event log, which must never disturb a live session.
func TestAnUnwritableStateDirFailsLoudlyExceptForTheLog(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	writeFile(t, blocker, "not a directory")
	t.Setenv("CLAUDE_DISPATCHER_STATE", filepath.Join(blocker, "state"))
	if err := Save(&Dispatch{ID: "x", Status: StatusWorking}); err == nil {
		t.Error("a record that could not be written was reported saved")
	}
	if _, err := WritePrompt("x", "go"); err == nil {
		t.Error("a prompt that could not be written was reported written")
	}
	AppendEvent(Event{Event: "Stop"}) // must return, not panic or fail
	if LoadEvents() != nil {
		t.Error("an unreadable log produced events")
	}
}

// The log is read back as written, oldest first, and a torn or blank line is a
// non-event: the writer swallows its failures, so a truncated last line is
// normal and must not cost the reader the rest of the log.
func TestTheEventLogSurvivesATornLine(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_DISPATCHER_STATE", dir)
	if LoadEvents() != nil {
		t.Fatal("a missing log produced events")
	}
	AppendEvent(Event{Event: EventDispatchAsked, Feature: "first", Repo: "acme"})
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("\n{\"event\":\"Sto\n")
	_ = f.Close()
	AppendEvent(Event{Event: EventDispatchFailed, Feature: "second", Reason: "empty slug"})

	got := LoadEvents()
	if len(got) != 2 {
		t.Fatalf("%d events read, want the two whole ones: %+v", len(got), got)
	}
	if got[0].Feature != "first" || got[1].Feature != "second" || got[1].Reason != "empty slug" {
		t.Errorf("events out of order or incomplete: %+v", got)
	}
	if got[0].Time.IsZero() {
		t.Error("an appended event was not stamped")
	}
}

// Ids name the record file and the session's join key, so two dispatches must
// not share one.
func TestNewIDsAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID()
		if len(id) != 12 || seen[id] {
			t.Fatalf("id %q is malformed or repeated", id)
		}
		seen[id] = true
	}
}
