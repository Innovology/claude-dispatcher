package fleetcmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/steward"
)

const head = "0123456789abcdef0123456789abcdef01234567"

// greenView is gh's account of a pull request on head with every check passed.
func greenView(head string) string {
	return `{"isDraft":false,"headRefOid":"` + head + `","statusCheckRollup":[` +
		`{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"SUCCESS"},` +
		`{"__typename":"CheckRun","name":"upload","status":"COMPLETED","conclusion":"SKIPPED"},` +
		`{"__typename":"StatusContext","context":"ci/legacy","state":"SUCCESS"}]}`
}

// withForge answers merge's reads from a table keyed by gh's first two words
// (and the api path for an api call).
func withForge(t *testing.T, answers map[string]string) {
	t.Helper()
	pr := ghRead
	t.Cleanup(func() { ghRead = pr })
	ghRead = func(_ string, args ...string) (string, error) {
		key := args[0] + " " + args[1]
		if out, ok := answers[key]; ok {
			return out, nil
		}
		return "gh: not stubbed: " + key, errors.New("exit 1")
	}
}

func withMerge(t *testing.T) *[]string {
	t.Helper()
	withForge(t, map[string]string{"pr view": greenView(head), "run list": "[]"})
	pd, pg := discoverRepos, ghMerge
	t.Cleanup(func() { discoverRepos, ghMerge = pd, pg })
	discoverRepos = func(*config.Config) []repos.Repo {
		return []repos.Repo{{Name: "player-app", Path: "/src/player-app"}, {Name: "design-system", Path: "/src/ds"}}
	}
	var calls []string
	ghMerge = func(dir string, args ...string) (string, error) {
		calls = append(calls, dir+": gh "+strings.Join(args, " "))
		return "", nil
	}
	return &calls
}

// The ruling verbs: added with an end, listed while they stand, ended early.
func TestRulingVerbs(t *testing.T) {
	withStore(t)
	var out, errOut bytes.Buffer
	if code := Run("ruling", []string{"add", "--product", "player", "--allow", "merge", "--pr", "any", "--until", "6h", "merge", "greens"}, &out, &errOut); code != 0 {
		t.Fatalf("add: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "allows merge of any pull request · merge greens") {
		t.Fatalf("add said %q", out.String())
	}
	id := strings.Fields(out.String())[1]
	out.Reset()
	if code := Run("ruling", []string{"list", "--product", "player"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), id) {
		t.Fatalf("list: %q %q", out.String(), errOut.String())
	}
	out.Reset()
	if code := Run("ruling", []string{"end", id}, &out, &errOut); code != 0 {
		t.Fatalf("end: %s", errOut.String())
	}
	out.Reset()
	_ = Run("ruling", []string{"list", "--product", "player"}, &out, &errOut)
	if !strings.Contains(out.String(), "no rulings stand") {
		t.Fatalf("an ended ruling is still listed as standing: %q", out.String())
	}
	if code := Run("ruling", []string{"add", "--product", "player", "--allow", "deploy", "ship it"}, &out, &errOut); code == 0 {
		t.Fatal("a ruling granting a hand there is none of was accepted")
	}
	if code := Run("ruling", []string{"add", "merge"}, &out, &errOut); code == 0 {
		t.Fatal("a ruling with no product was accepted")
	}
}

// merge refuses without a ruling that allows it there, and with one merges the
// exact head it was given, in the repo's own checkout, and says so in the log.
func TestMergeNeedsARuling(t *testing.T) {
	withStore(t)
	calls := withMerge(t)
	var out, errOut bytes.Buffer
	if code := Run("merge", []string{"player-app", "931", "--head", head}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "no ruling") {
		t.Fatalf("merged without a ruling: %q", errOut.String())
	}
	if _, err := steward.AddRuling("player", steward.Ruling{Text: "merge #931 in the app", Allow: []string{steward.AllowMerge}, PRs: []int{931}, Repo: "player-app"}); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := Run("merge", []string{"design-system", "12", "--head", head}, &out, &errOut); code == 0 {
		t.Fatal("a ruling for player-app merged in design-system")
	}
	// Found in review: a grant whose words said "only #931" merged #99.
	if code := Run("merge", []string{"player-app", "99", "--head", head}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "no ruling lets the steward merge #99") {
		t.Fatalf("a ruling for #931 merged #99: %q", errOut.String())
	}
	for _, bad := range [][]string{
		{"player-app", "931", "--head", "0123abc"},
		{"player-app", "nine", "--head", head},
		{"player-app", "931", "--head", head, "--method", "yolo"},
		{"player-app", "931"},
	} {
		if code := Run("merge", bad, &out, &errOut); code == 0 {
			t.Errorf("accepted %v", bad)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("gh was asked before every check passed: %v", *calls)
	}
	errOut.Reset()
	if code := Run("merge", []string{"player-app", "#931", "--head", head}, &out, &errOut); code != 0 {
		t.Fatalf("merge: %s", errOut.String())
	}
	if want := "/src/player-app: gh pr merge 931 --squash --match-head-commit " + head; len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("calls = %v", *calls)
	}
	var logged bool
	for _, ev := range state.LoadEvents() {
		if ev.Event == state.EventStewardMerged && ev.Repo == "player-app" && ev.Feature == "#931" && strings.Contains(ev.Reason, "merge #931") {
			logged = true
		}
	}
	if !logged {
		t.Fatal("the merge is not in the event log")
	}
}

// gh's refusal is the steward's to read, in gh's words.
func TestMergeSaysWhyGhRefused(t *testing.T) {
	withStore(t)
	withMerge(t)
	ghMerge = func(string, ...string) (string, error) {
		return "GraphQL: Head branch was modified. Review and try the merge again.\nmore", errors.New("exit 1")
	}
	if _, err := steward.AddRuling("player", steward.Ruling{Text: "merge greens", Allow: []string{steward.AllowMerge}, AnyPR: true}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run("merge", []string{"player-app", "931", "--head", head}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "Head branch was modified") {
		t.Fatalf("got %q", errOut.String())
	}
}

// Found in review, measured: an unquoted "--until 2026-11-16 09:00" stood
// until the end of the 16th with "09:00" leading its text.
func TestRulingAddReadsWhatWasMeant(t *testing.T) {
	withStore(t)
	// A day that is still ahead whenever this runs: a fixed date stops being one,
	// and a ruling that would already be over is refused.
	day := time.Now().AddDate(0, 0, 7)
	var out, errOut bytes.Buffer
	if code := Run("ruling", []string{"add", "--product", "player", "--allow", "merge", "--pr", "any", "--until", day.Format("2006-01-02"), "09:00", "merge", "greens"}, &out, &errOut); code != 0 {
		t.Fatalf("add: %s", errOut.String())
	}
	rs := steward.Rulings("player")
	if len(rs) != 1 || rs[0].Text != "merge greens" || !rs[0].Until.Equal(time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, time.Local)) {
		t.Fatalf("ruling = %+v", rs)
	}
}

// Found in review: flags were lifted out of a ruling's text wherever they sat,
// so "do not --allow merge yet" recorded a ruling that allows merge, with "do
// not yet" for its text. A flag after the text began is refused — neither read
// as a grant nor swallowed as words — unless `--` says the text means it.
func TestARulingsFlagsEndWhereItsTextBegins(t *testing.T) {
	withStore(t)
	var out, errOut bytes.Buffer
	for _, args := range [][]string{
		{"add", "--product", "tools", "--until", "1h", "do", "not", "--allow", "merge", "yet"},
		{"add", "--product", "tools", "merge", "greens", "--allow", "merge", "--until", "2h"},
		{"add", "--product", "tools", "merge", "greens", "-allow=merge"},
	} {
		errOut.Reset()
		if code := Run("ruling", args, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "put every flag before the text") {
			t.Errorf("%v: code %d, %q", args, code, errOut.String())
		}
	}
	if rs := steward.Rulings("tools"); len(rs) != 0 {
		t.Fatalf("a refused ruling was recorded: %+v", rs)
	}
	if code := Run("ruling", []string{"add", "--product", "tools", "--", "do", "not", "--allow", "merge", "yet"}, &out, &errOut); code != 0 {
		t.Fatalf("add after --: %s", errOut.String())
	}
	rs := steward.Rulings("tools")
	if len(rs) != 1 || rs[0].Text != "do not --allow merge yet" || len(rs[0].Allow) != 0 {
		t.Fatalf("after --, the words are text and grant nothing: %+v", rs)
	}
	// A word that only looks like a flag is text.
	if code := Run("ruling", []string{"add", "--product", "tools", "hold", "--force-pushes", "and", "-1", "days"}, &out, &errOut); code != 0 {
		t.Fatalf("add: %s", errOut.String())
	}
}

// Found in review: merge checked nothing about the pull request, so under a
// standing ruling a red one merged in a repo with no required checks. It is held
// to green now — and green is one complete run, not one stitched together from
// re-running only the failed jobs.
func TestMergeHoldsThePullRequestToGreen(t *testing.T) {
	withStore(t)
	calls := withMerge(t)
	if _, err := steward.AddRuling("player", steward.Ruling{Text: "merge greens", Allow: []string{steward.AllowMerge}, AnyPR: true}); err != nil {
		t.Fatal(err)
	}
	moved := strings.Repeat("f", 40)
	view := func(rollup string) string {
		return `{"isDraft":false,"headRefOid":"` + head + `","statusCheckRollup":[` + rollup + `]}`
	}
	const run = `[{"databaseId":7,"attempt":2,"workflowName":"CI"},{"databaseId":5,"attempt":1,"workflowName":"CodeQL"},{"databaseId":3,"attempt":1,"workflowName":"CI"}]`
	partial := map[string]string{"pr view": greenView(head), "run list": run,
		"api repos/{owner}/{repo}/actions/runs/7/attempts/2":                   "2026-09-17T13:50:18Z",
		"api repos/{owner}/{repo}/actions/runs/7/attempts/2/jobs?per_page=100": `{"jobs":[{"name":"test","started_at":"2026-09-17T13:50:22Z"},{"name":"lint","started_at":"2026-09-17T13:46:36Z"}]}`}
	for name, c := range map[string]struct {
		answers map[string]string
		says    string
	}{
		"draft":        {map[string]string{"pr view": `{"isDraft":true,"headRefOid":"` + head + `"}`}, "is a draft"},
		"head moved":   {map[string]string{"pr view": `{"isDraft":false,"headRefOid":"` + moved + `"}`}, "read them again"},
		"no checks":    {map[string]string{"pr view": view("")}, "no checks ran"},
		"failing":      {map[string]string{"pr view": view(`{"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE"}`)}, "test on #931 is failure"},
		"running":      {map[string]string{"pr view": view(`{"__typename":"CheckRun","name":"test","status":"IN_PROGRESS"}`)}, "has not finished (in_progress)"},
		"red status":   {map[string]string{"pr view": view(`{"__typename":"StatusContext","context":"ci/legacy","state":"PENDING"}`)}, "ci/legacy on #931 is pending"},
		"unreadable":   {map[string]string{}, "could not read #931"},
		"partial":      {partial, "re-ran only some jobs and kept lint"},
		"runs unknown": {map[string]string{"pr view": greenView(head)}, "could not read #931's workflow runs"},
	} {
		withForge(t, c.answers)
		var out, errOut bytes.Buffer
		if code := Run("merge", []string{"player-app", "931", "--head", head}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), c.says) {
			t.Errorf("%s: code %d, said %q", name, code, errOut.String())
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("gh merged what was not green: %v", *calls)
	}
	// The same workflow run again in full: every job started in the attempt
	// that passed, and an older partial run of it no longer decides anything.
	whole := map[string]string{"pr view": greenView(head), "run list": run,
		"api repos/{owner}/{repo}/actions/runs/7/attempts/2":                   "2026-09-17T13:50:18Z",
		"api repos/{owner}/{repo}/actions/runs/7/attempts/2/jobs?per_page=100": `{"jobs":[{"name":"test","started_at":"2026-09-17T13:50:22Z"},{"name":"lint","started_at":"2026-09-17T13:50:18Z"}]}`}
	withForge(t, whole)
	var out, errOut bytes.Buffer
	if code := Run("merge", []string{"player-app", "931", "--head", head}, &out, &errOut); code != 0 {
		t.Fatalf("a whole re-run that passed was refused: %s", errOut.String())
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v", *calls)
	}
}

// A merge grant says which pull requests it is for in the command the human
// confirms, and the line it prints back says the same.
func TestAMergeRulingNamesItsPullRequests(t *testing.T) {
	withStore(t)
	var out, errOut bytes.Buffer
	if code := Run("ruling", []string{"add", "--product", "player", "--allow", "merge", "merge", "ONLY", "#12"}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "--pr 12,14, or --pr any") {
		t.Fatalf("an unscoped merge grant was accepted: %q", errOut.String())
	}
	for _, bad := range [][]string{
		{"add", "--product", "player", "--allow", "merge", "--pr", "twelve", "merge"},
		{"add", "--product", "player", "--pr", "12", "#12 waits for goldens"},
	} {
		if code := Run("ruling", bad, &out, &errOut); code == 0 {
			t.Errorf("accepted %v", bad)
		}
	}
	out.Reset()
	if code := Run("ruling", []string{"add", "--product", "player", "--allow", "merge", "--pr", "#12,14", "--repo", "player-app", "merge", "these", "two"}, &out, &errOut); code != 0 {
		t.Fatalf("add: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "allows merge of #12, #14 in player-app · merge these two") {
		t.Fatalf("add said %q", out.String())
	}
}

// A ruling or a question for a product the config does not name is refused.
func TestUnknownProductRefusedByTheVerbs(t *testing.T) {
	withStore(t)
	var out, errOut bytes.Buffer
	if code := Run("ruling", []string{"add", "--product", "plyer", "--allow", "merge", "merge", "greens"}, &out, &errOut); code == 0 {
		t.Fatal("a ruling for an unknown product was recorded")
	}
	if code := Run("ask", []string{"--product", "plyer", "Merge?"}, &out, &errOut); code == 0 {
		t.Fatal("a question for an unknown product was saved")
	}
}
