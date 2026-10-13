package gh

import "testing"

// An umbrella workflow's aggregator is the only check that answers "is this
// mergeable", and it is one entry among dozens in the rollup the repo read has
// already paid for. Measured on Player-Pulse/player-app, whose `All gates
// green` job is documented in its own workflow as "the aggregator is the ONLY
// required check": 7 open pull requests, 1 with the gate SUCCESS, 2 FAILURE and
// 4 carrying no gate entry at all, beside lanes reporting CANCELLED and SKIPPED
// that say nothing about whether the thing can ship.
//
// The failing case this guards: reading those lanes instead made the repo's row
// say "✗ failing" while its trunk was green and a pull request sat ready to
// merge.
func TestGateStateReadsTheNamedCheckOffTheBatchedRollup(t *testing.T) {
	calls := fakeGH(t, `case "$*" in
  *"pr list --state open"*) echo '[
    {"number":670,"statusCheckRollup":[
      {"__typename":"CheckRun","name":"Lint","status":"COMPLETED","conclusion":"CANCELLED"},
      {"__typename":"CheckRun","name":"Android build","status":"COMPLETED","conclusion":"SKIPPED"},
      {"__typename":"CheckRun","name":"All gates green","status":"COMPLETED","conclusion":"FAILURE"}]},
    {"number":665,"statusCheckRollup":[
      {"__typename":"CheckRun","name":"Lint","status":"COMPLETED","conclusion":"FAILURE"},
      {"__typename":"CheckRun","name":"All gates green","status":"COMPLETED","conclusion":"SUCCESS"}]},
    {"number":669,"statusCheckRollup":[
      {"__typename":"CheckRun","name":"Unit tests","status":"COMPLETED","conclusion":"SUCCESS"}]},
    {"number":654,"statusCheckRollup":[
      {"__typename":"StatusContext","context":"All gates green","state":"SUCCESS"}]},
    {"number":600,"statusCheckRollup":[
      {"__typename":"CheckRun","name":"All gates green","status":"IN_PROGRESS","conclusion":""}]},
    {"number":246,"statusCheckRollup":[
      {"__typename":"CheckRun","name":"All gates green","status":"COMPLETED","conclusion":"SUCCESS"},
      {"__typename":"CheckRun","name":"All gates green","status":"COMPLETED","conclusion":"FAILURE"}]}
  ]' ;;
  *) echo '[]' ;;
esac`)

	open, asked := RepoPRs(t.TempDir())
	if !asked {
		t.Fatal("the repo read failed")
	}
	want := map[int]string{
		670: "FAILURE", // the gate is red; the cancelled lanes beside it are noise
		665: "SUCCESS", // a lane failed and the gate still passed — the gate decides
		669: "",        // no gate entry: the umbrella skipped its aggregator
		654: "SUCCESS", // the older status-context shape names itself `context`
		600: "PENDING", // still running, which is not a verdict either way
		246: "FAILURE", // a re-run left two entries; red beats green for one gate
	}
	for num, w := range want {
		if got := open[num].GateState("All gates green"); got != w {
			t.Errorf("PR %d gate = %q, want %q", num, got, w)
		}
	}
	// A gate nobody named, and a name no rollup carries, are both silence.
	if got := open[670].GateState(""); got != "" {
		t.Errorf("unnamed gate = %q, want silence", got)
	}
	if got := open[670].GateState("Required checks"); got != "" {
		t.Errorf("absent gate = %q, want silence", got)
	}
	// The hard requirement: the gate rides along on the read the cockpit was
	// making anyway. One extra API call here is a quota this cockpit has
	// already exhausted once.
	if got := calls(); len(got) != 1 {
		t.Errorf("the gate cost %d calls, want the one repo read: %v", len(got), got)
	}
}
