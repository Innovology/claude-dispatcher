package gh

// throttle.go is the brake. cache.go bounds how often we ask GitHub anything;
// this bounds what happens when GitHub answers that we have asked too much.
//
// Every read in this package degrades to "no signal" on error, which is the
// right shape for a missing binary or an offline machine and exactly the wrong
// one for an exhausted quota: the collectors could not tell a repo with no
// checks from a client that had been locked out, so they kept a whole
// portfolio's worth of requests going at full rate against a quota that was
// already at zero — holding it there, and taking the human's own `gh` down with
// it for the rest of the hour. The one thing GitHub was actually telling us was
// the one thing nothing read.
//
// So the first refusal parks every read in this package until the window
// resets. Nothing retries, nothing probes, nothing spawns a process: the
// answer for the next few minutes is known, and it is not worth a request.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// ErrThrottled is returned instead of running a command while parked. Callers
// treat it like any other failure — they already degrade to no signal — so the
// only difference it makes is that no process was spawned to earn it.
var ErrThrottled = errors.New("gh: github api quota spent")

// blindPark is how long we stand down when GitHub refuses us but we cannot
// find out when the window resets — a secondary rate limit, or a rate_limit
// read that itself failed. Long enough to stop a spiral, short enough that a
// cockpit left open recovers on its own.
const blindPark = 5 * time.Minute

// GitHub phrases quota refusals several ways, and gh passes them through on
// stderr: primary limits ("API rate limit exceeded", "API rate limit already
// exceeded for user ID …"), the secondary limit, and the abuse-detection
// wording used when requests arrive too fast.
var rateLimitRe = regexp.MustCompile(
	`(?i)rate limit (already )?exceeded|secondary rate limit|too many requests|submitted too quickly`)

var (
	thMu    sync.Mutex
	thUntil time.Time
)

// Throttled reports whether GitHub has refused us for quota and when the window
// resets. The cockpit shows this rather than an empty forge: "—" in every check
// column is a claim about the repositories, and this is a fact about us.
func Throttled() (until time.Time, throttled bool) {
	thMu.Lock()
	defer thMu.Unlock()
	if time.Now().Before(thUntil) {
		return thUntil, true
	}
	return time.Time{}, false
}

// run executes a gh command unless we are parked, and parks if the answer is
// that the quota is gone. Every read in this package goes through it.
func run(cmd *exec.Cmd) ([]byte, error) {
	if _, parked := Throttled(); parked {
		return nil, ErrThrottled
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && rateLimitRe.Match(stderr.Bytes()) {
		park()
	}
	return out, err
}

// park stands every read down until the quota window resets.
//
// The provisional park goes in before the reset is looked up, because the
// refusal that got us here is one of a fan-out: a dozen collectors are in
// flight and each is about to be refused too. Parking first is what stops them
// all queueing behind one lookup and then all asking anyway.
func park() {
	thMu.Lock()
	if time.Now().Before(thUntil) {
		thMu.Unlock() // already parked by whichever request was refused first
		return
	}
	thUntil = time.Now().Add(blindPark)
	thMu.Unlock()

	if t, ok := resetAt(); ok {
		thMu.Lock()
		thUntil = t.Add(time.Second) // clocks differ; wake just after, never just before
		thMu.Unlock()
	}
}

// resetAt asks GitHub when the exhausted window ends. It bypasses run — we are
// parked by the time it is called, and this is the one endpoint worth asking
// anyway: /rate_limit does not itself count against the limit.
//
// The answer wanted is the reset of whatever ran out, not of everything: a
// spent GraphQL budget must not park us behind a search window that resets in
// an hour. Nothing at zero means the refusal was a secondary limit, which
// /rate_limit does not report — blindPark stands.
func resetAt() (time.Time, bool) {
	out, err := exec.Command("gh", "api", "rate_limit").Output()
	if err != nil {
		return time.Time{}, false
	}
	var body struct {
		Resources map[string]struct {
			Remaining int   `json:"remaining"`
			Reset     int64 `json:"reset"`
		} `json:"resources"`
	}
	if json.Unmarshal(out, &body) != nil {
		return time.Time{}, false
	}
	var latest int64
	for _, r := range body.Resources {
		if r.Remaining == 0 && r.Reset > latest {
			latest = r.Reset
		}
	}
	if latest == 0 {
		return time.Time{}, false
	}
	return time.Unix(latest, 0), true
}

// ThrottledFor renders how long the park has left, for the one line the cockpit
// shows about it.
func ThrottledFor(until time.Time) string {
	d := time.Until(until)
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())+1) + "s"
	}
	return strconv.Itoa(int(d.Minutes())+1) + "m"
}

// clearThrottle releases the park. Tests only — deliberately not wired to
// InvalidateCache, which is about how stale *our* answers are. The park is
// about GitHub refusing us, and a human coming back from a jump-in cannot
// change that. It expires on its own, at the reset it was told about.
func clearThrottle() {
	thMu.Lock()
	thUntil = time.Time{}
	thMu.Unlock()
}
