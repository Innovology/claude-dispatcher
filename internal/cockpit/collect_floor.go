package cockpit

// collect_floor.go maps the user's real dispatch records into the floor
// (triage) lens's view model. It reads the dispatch records loaded by
// collectCtx, enriches each with best-effort git/gh/transcript signals, and
// fills the floor slice of the snapshot. Every external call degrades to an
// empty/zero value on error — nothing here is ever fatal.

import (
	"os/exec"
	"strconv"
	"strings"
	"time"

	"claude-dispatcher/internal/gh"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/transcript"
)

// collectFloor rebuilds the in-flight floor from the live dispatch records.
func collectFloor(ctx *collectCtx, s *snapshot) {
	s.dispatches = []dispatch{}
	s.records = map[string]*state.Dispatch{}
	s.saidBy = map[string]string{}
	s.tailLines = map[string][]string{}
	s.diffsBy = map[string]struct {
		files []diffFile
		hunk  []hunkLine
	}{}

	// Each record costs several git and gh round-trips, and they are
	// independent, so gather them concurrently and assemble in record order —
	// the floor's ordering must not depend on which request finished first.
	rows := make([]floorRow, len(ctx.records))
	forEach(ctx.records, func(i int, rec *state.Dispatch) {
		rows[i] = buildFloorRow(ctx, rec)
	})

	for _, r := range rows {
		if !r.ok {
			continue
		}
		rec := r.rec
		s.dispatches = append(s.dispatches, r.d)

		// Side tables keyed by feature.
		s.records[rec.Feature] = rec
		s.saidBy[rec.Feature] = r.said
		s.tailLines[rec.Feature] = r.tail
		s.diffsBy[rec.Feature] = struct {
			files []diffFile
			hunk  []hunkLine
		}{files: r.dfs, hunk: []hunkLine{}}
	}
}

// floorRow is one record's fully-resolved contribution to the floor, built off
// the assembly loop so the round-trips can overlap.
type floorRow struct {
	ok   bool
	rec  *state.Dispatch
	d    dispatch
	said string
	tail []string
	dfs  []diffFile
}

// buildFloorRow resolves one dispatch record into its view row. It is called
// concurrently, so it touches nothing but its own arguments.
func buildFloorRow(ctx *collectCtx, rec *state.Dispatch) floorRow {
	st := floorState(rec)
	if st == "" {
		return floorRow{} // exited with no shipped PR — not on the floor
	}
	forge := ctx.forge(rec.RepoPath)
	commits := len(rec.Commits)

	// Diff provenance: what BaseSHA..Branch actually changed.
	plus, minus, files, dfs := floorNumstat(rec.RepoPath, rec.BaseSHA, rec.Branch)

	// PR review + checks signals (gh only; ado degrades to zero).
	var checks gh.Checks
	var review gh.Review
	if forge == "gh" && rec.PRNumber > 0 {
		checks = gh.PRChecksFor(rec.RepoPath, rec.PRNumber)
		review = gh.PRReviewFor(rec.RepoPath, rec.PRNumber)
	}

	// Transcript preview: one read reused for said/tail/activity.
	tail := transcript.Tail(rec.TranscriptPath, 8)

	age := floorAge(rec.UpdatedAt)
	urgent := st == "blocked" || st == "needs"

	// PR reference + its short meta, reused by the stack view.
	var prs []prRef
	var prMeta string
	if rec.PRNumber > 0 {
		id := prID(forge, rec.PRNumber)
		var prColor string
		if rec.PRState == "MERGED" {
			prMeta = "merged"
			if rec.PRMergedAt != nil {
				prMeta = "merged " + floorAge(*rec.PRMergedAt) + " ago"
			}
			prColor = cMid
		} else {
			appr := "0 reviews"
			if review.Approvals > 0 {
				appr = floorApprovals(review.Approvals)
			}
			if cm, cc := floorChecksMeta(checks); cm != "" {
				prMeta = appr + " · " + cm
				prColor = cc
			} else {
				prMeta = appr
				prColor = cMid
			}
		}
		prs = []prRef{{id, rec.Feature, prMeta, prColor, forge}}
	}

	d := dispatch{
		feature: rec.Feature,
		repo:    rec.RepoName,
		product: ctx.productFor(rec),
		forge:   forge,
		state:   st,
		age:     age,
		branch:  rec.Branch,
		why:     rec.StatusReason,
		signal:  floorSignal(st, checks, review),
		urgent:  urgent,
		plus:    plus,
		minus:   minus,
		files:   files,
		commits: commits,
		prompt:  rec.Prompt,
		agents: []agent{{
			"", "main", modelForRecord(rec), rec.StatusReason,
			floorAgentState(st), age + " · " + strconv.Itoa(commits) + " commits",
		}},
		prs:   prs,
		runs:  floorRuns(rec),
		chain: floorChain(rec, forge, plus, minus, files, checks),
		ask:   floorAsk(rec, st, commits, age),
	}

	return floorRow{ok: true, rec: rec, d: d, said: floorSaid(tail), tail: tail, dfs: dfs}
}

// floorState maps a record's Status to the floor's view state, or "" to skip.
func floorState(rec *state.Dispatch) string {
	switch rec.Status {
	case state.StatusBlocked:
		return "blocked"
	case state.StatusNeedsInput:
		// A complete turn with an open, undeployed PR is a review row, not an
		// input request — the code exists but is not live.
		if rec.PRNumber > 0 && rec.PRState == "OPEN" && rec.DeployedAt == nil {
			return "review"
		}
		return "needs"
	case state.StatusWorking, state.StatusLaunching:
		return "working"
	case state.StatusDone:
		return "live"
	case state.StatusExited:
		if rec.PRState == "MERGED" || rec.PRMergedAt != nil {
			return "live"
		}
		return ""
	}
	return "working"
}

// modelForRecord reports the model a session runs. No model is recorded yet, so
// this is the design default; kept as a seam for when it is captured.
func modelForRecord(_ *state.Dispatch) string { return "sonnet" }

// floorAgentState maps a view state to the main agent's status glyph state.
func floorAgentState(st string) string {
	switch st {
	case "working":
		return "now"
	case "live":
		return "ok"
	default:
		return "idle"
	}
}

// floorSignal derives the short right-margin hint for a row.
func floorSignal(st string, checks gh.Checks, review gh.Review) string {
	switch st {
	case "blocked":
		return "approve"
	case "needs":
		return "needs you"
	case "review":
		switch {
		case checks.Failing > 0:
			return "checks ✗"
		case checks.Running > 0:
			return "ci running"
		case review.Approvals > 0:
			return "approved"
		default:
			return "mergeable"
		}
	case "working":
		return "working"
	case "live":
		return "live"
	}
	return ""
}

// floorAsk synthesises the blocked/needs decision block; nil for other states.
func floorAsk(rec *state.Dispatch, st string, commits int, age string) *askBlock {
	if st != "blocked" && st != "needs" {
		return nil
	}
	return &askBlock{
		kicker:   strings.ToUpper(stateMetaBy[st].label),
		headline: rec.RepoName + " · " + rec.StatusReason,
		evidence: strconv.Itoa(commits) + " commits · " + age,
		actions:  []action{{"y", "approve"}, {"n", "deny"}, {"r", "reply"}, {"enter", "attach"}},
	}
}

// floorRuns maps recent workflow runs on the branch to the runs strip.
func floorRuns(rec *state.Dispatch) []runRef {
	runs := gh.RunsForBranch(rec.RepoPath, rec.Branch)
	out := make([]runRef, 0, len(runs))
	for i, r := range runs {
		if i >= 3 {
			break
		}
		var label, color string
		if !strings.EqualFold(r.Status, "completed") {
			label, color = "● running", cBlue
		} else {
			switch r.Conclusion {
			case "success":
				label, color = "✓ passed", cGreen
			case "failure", "cancelled", "timed_out":
				label, color = "✗ "+r.Conclusion, cRed
			default:
				label, color = "· "+r.Conclusion, cFaint
			}
		}
		out = append(out, runRef{r.Name, label, color, floorAge(r.CreatedAt)})
	}
	return out
}

// floorChain builds the 5-node commits→PR→checks→merge→deploy chain.
func floorChain(rec *state.Dispatch, forge string, plus, minus, files int, checks gh.Checks) []chainStep {
	commits := len(rec.Commits)
	ch := make([]chainStep, 0, 5)

	// commits
	cst := "ok"
	if commits == 0 {
		cst = "idle"
	}
	ch = append(ch, chainStep{
		strconv.Itoa(commits) + " commits",
		"+" + strconv.Itoa(plus) + " −" + strconv.Itoa(minus) + " · " + strconv.Itoa(files) + " files",
		cst,
	})

	// PR
	if rec.PRNumber > 0 {
		word := "open"
		switch rec.PRState {
		case "MERGED":
			word = "merged"
		case "CLOSED":
			word = "closed"
		}
		ch = append(ch, chainStep{prID(forge, rec.PRNumber) + " " + word, forgeLabel(forge), "ok"})
	} else {
		ch = append(ch, chainStep{"no pr", "branch only", "idle"})
	}

	// checks
	switch {
	case checks.Total == 0:
		ch = append(ch, chainStep{"checks", "—", "idle"})
	case checks.Failing > 0:
		ch = append(ch, chainStep{"checks ✗ " + strconv.Itoa(checks.Failing), "ci failing", "bad"})
	case checks.Running > 0:
		ch = append(ch, chainStep{"checks " + strconv.Itoa(checks.Passed) + "/" + strconv.Itoa(checks.Total), "running", "now"})
	default:
		ch = append(ch, chainStep{"checks ✓ " + strconv.Itoa(checks.Passed) + "/" + strconv.Itoa(checks.Total), "ci green", "ok"})
	}

	// merge
	switch {
	case rec.PRState == "MERGED":
		meta := "on " + forgeLabel(forge)
		if rec.PRMergedAt != nil {
			meta = "merged " + floorAge(*rec.PRMergedAt) + " ago"
		}
		ch = append(ch, chainStep{"merged", meta, "ok"})
	case rec.PRNumber > 0 && rec.PRState == "OPEN":
		ch = append(ch, chainStep{"merge", "waiting on you", "now"})
	default:
		ch = append(ch, chainStep{"merge", "—", "idle"})
	}

	// deploy
	switch {
	case rec.DeployedAt != nil:
		ch = append(ch, chainStep{"deployed", "live " + floorAge(*rec.DeployedAt) + " ago", "ok"})
	case rec.PRState == "MERGED":
		ch = append(ch, chainStep{"deploy", "fires after merge", "idle"})
	default:
		ch = append(ch, chainStep{"deploy", "—", "idle"})
	}

	return ch
}

// floorSaid returns the last assistant text line in the tail (skipping tool
// uses), or "" when there is none.
func floorSaid(tail []string) string {
	for i := len(tail) - 1; i >= 0; i-- {
		if !strings.HasPrefix(tail[i], "⚙ ") {
			return tail[i]
		}
	}
	return ""
}

// floorNumstat sums `git diff --numstat BaseSHA..Branch` into totals plus a
// per-file list. Degrades to zero/empty on any error.
func floorNumstat(repoPath, base, branch string) (plus, minus, files int, dfs []diffFile) {
	dfs = []diffFile{}
	if repoPath == "" || base == "" || branch == "" {
		return
	}
	out, err := exec.Command("git", "-C", repoPath, "diff", "--numstat", base+".."+branch).Output()
	if err != nil {
		return
	}
	for _, ln := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if ln == "" {
			continue
		}
		parts := strings.Fields(ln)
		if len(parts) < 3 {
			continue
		}
		a, _ := strconv.Atoi(parts[0]) // "-" (binary) parses to 0
		d, _ := strconv.Atoi(parts[1])
		plus += a
		minus += d
		files++
		dfs = append(dfs, diffFile{path: strings.Join(parts[2:], " "), plus: a, minus: d})
	}
	return
}

// floorChecksMeta renders a PR's check rollup like "✓ 4/4" with its colour, or
// "" when there are no checks.
func floorChecksMeta(c gh.Checks) (label, color string) {
	if c.Total == 0 {
		return "", cFaint
	}
	switch {
	case c.Failing > 0:
		return "✗ " + strconv.Itoa(c.Passed) + "/" + strconv.Itoa(c.Total), cRed
	case c.Running > 0:
		return "● " + strconv.Itoa(c.Passed) + "/" + strconv.Itoa(c.Total), cBlue
	default:
		return "✓ " + strconv.Itoa(c.Passed) + "/" + strconv.Itoa(c.Total), cGreen
	}
}

func floorApprovals(n int) string {
	if n == 1 {
		return "1 approval"
	}
	return strconv.Itoa(n) + " approvals"
}

// prID renders a PR reference: "#123" on github, "!123" on azure devops.
func prID(forge string, n int) string {
	if forge == "ado" {
		return "!" + strconv.Itoa(n)
	}
	return "#" + strconv.Itoa(n)
}

func forgeLabel(forge string) string {
	if forge == "ado" {
		return "azure devops"
	}
	return "github"
}

// floorAge formats a timestamp as a compact relative age: "now","4m","2h","3d".
func floorAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}
