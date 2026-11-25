package fleetcmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/dispatch"
	"claude-dispatcher/internal/state"
)

// stubResumeMany answers for the batch: each record gets what answer says, in
// the order the records were handed over, and the records and the message are
// kept for the test to read.
func stubResumeMany(t *testing.T, answer func(d *state.Dispatch) dispatch.ResumeResult) (got *[]string, said *string) {
	t.Helper()
	prev := resumeMany
	t.Cleanup(func() { resumeMany = prev })
	var ids []string
	var msg string
	resumeMany = func(recs []*state.Dispatch, prompt string, report func(dispatch.ResumeResult)) {
		msg = prompt
		for _, d := range recs {
			ids = append(ids, d.ID)
			r := answer(d)
			r.ID, r.Feature = d.ID, d.Feature
			report(r)
		}
	}
	return &ids, &msg
}

func TestResumeArgsTakeTheFlagAnywhere(t *testing.T) {
	for _, args := range [][]string{
		{"a", "b", "--say", "continue"},
		{"--say", "continue", "a", "b"},
		{"a", "--say=continue", "b"},
	} {
		keys, say, err := resumeArgs(args)
		if err != nil || strings.Join(keys, ",") != "a,b" || say != "continue" {
			t.Errorf("%q → %v %q %v", args, keys, say, err)
		}
	}
	// A name that starts with a dash is still a name after --.
	if keys, _, err := resumeArgs([]string{"--", "-odd"}); err != nil || len(keys) != 1 || keys[0] != "-odd" {
		t.Errorf("after --: %v %v", keys, err)
	}
	for _, bad := range [][]string{{}, {"--say"}, {"a", "--loud"}} {
		if _, _, err := resumeArgs(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestResumeResolvesEveryNameBeforeResumingAny(t *testing.T) {
	withStore(t,
		&state.Dispatch{ID: "d1", Feature: "pay", Status: state.StatusExited, SessionID: "s1"},
		&state.Dispatch{ID: "d2", Feature: "auth", Status: state.StatusDone, SessionID: "s2"},
	)
	ids, _ := stubResumeMany(t, func(*state.Dispatch) dispatch.ResumeResult {
		return dispatch.ResumeResult{Mode: dispatch.ResumeStarted}
	})
	var out bytes.Buffer
	err := runResume([]string{"pay", "typo", "auth"}, &out)
	if err == nil || !strings.Contains(err.Error(), "nothing was resumed") || !strings.Contains(err.Error(), `"typo"`) {
		t.Fatalf("err = %v, want the typo named and nothing resumed", err)
	}
	if len(*ids) != 0 {
		t.Errorf("resumed %v though a name did not resolve", *ids)
	}
}

func TestResumeFindsFinishedRecordsAndRefusesAnAmbiguousName(t *testing.T) {
	older, newer := time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour)
	withStore(t,
		&state.Dispatch{ID: "old", Feature: "login", Status: state.StatusDone, SessionID: "s1"},
		&state.Dispatch{ID: "new", Feature: "login", Status: state.StatusExited, SessionID: "s2"},
		&state.Dispatch{ID: "d3", Feature: "pay", Status: state.StatusExited, SessionID: "s3"},
	)
	// Save stamps UpdatedAt, so the order is set on the loaded records.
	all := state.LoadAll()
	for _, d := range all {
		switch d.ID {
		case "old":
			d.UpdatedAt = older
		case "new":
			d.UpdatedAt = newer
		}
	}
	if d, err := findAny(all, "pay"); err != nil || d.ID != "d3" {
		t.Errorf("a finished record by its feature: %v %v", d, err)
	}
	if d, err := findAny(all, "old"); err != nil || d.ID != "old" {
		t.Errorf("by id: %v %v", d, err)
	}
	_, err := findAny(all, "login")
	if err == nil || !strings.Contains(err.Error(), "new (exited), old (done)") {
		t.Errorf("two records of one feature: %v, want both ids, newest first", err)
	}
}

func TestResumeSaysWhatHappenedToEach(t *testing.T) {
	withStore(t,
		&state.Dispatch{ID: "d1", Feature: "pay", Status: state.StatusExited, SessionID: "s1"},
		&state.Dispatch{ID: "d2", Feature: "auth", Status: state.StatusExited, SessionID: "s2"},
		&state.Dispatch{ID: "d3", Feature: "never", Status: state.StatusExited},
		&state.Dispatch{ID: "d4", Feature: "busy", Status: state.StatusWorking, SessionID: "s4"},
	)
	ids, said := stubResumeMany(t, func(d *state.Dispatch) dispatch.ResumeResult {
		switch d.ID {
		case "d2":
			return dispatch.ResumeResult{Err: errors.New("the transcript for \"auth\" is gone\nand more")}
		case "d3":
			return dispatch.ResumeResult{Mode: dispatch.ResumeRelaunched, Session: "disp-never"}
		case "d4":
			return dispatch.ResumeResult{Mode: dispatch.ResumeLive, Session: "disp-busy"}
		}
		return dispatch.ResumeResult{Mode: dispatch.ResumeStarted, Session: "disp-pay"}
	})
	var out bytes.Buffer
	err := runResume([]string{"pay", "d2", "never", "busy", "pay", "--say", " continue "}, &out)
	if err == nil || err.Error() != "1 of 4 did not resume" {
		t.Errorf("err = %v, want one of four counted as failed", err)
	}
	if strings.Join(*ids, ",") != "d1,d2,d3,d4" {
		t.Errorf("resumed %v, want each record once in the order named", *ids)
	}
	if *said != "continue" {
		t.Errorf("message = %q, want it trimmed and passed on", *said)
	}
	for _, want := range []string{
		`resumed "pay" · session disp-pay`,
		`"auth" did not resume: the transcript for "auth" is gone`,
		`started "never" again · it never ran · session disp-never`,
		`"busy" is still running · session disp-busy · the message was not sent to it`,
	} {
		if !strings.Contains(out.String(), want+"\n") {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "and more") {
		t.Errorf("a failure printed past its first line:\n%s", out.String())
	}
}
