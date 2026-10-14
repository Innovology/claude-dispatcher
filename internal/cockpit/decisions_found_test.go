package cockpit

// decisions_found_test.go covers the shapes a decision is recognised by, each
// written the way a real repo was found writing it — the lens shipped finding
// one repo's decisions and none of the hundreds in the repos beside it — and
// the screen that lists them.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func parsed(t *testing.T, rel string, lines ...string) []decision {
	t.Helper()
	return dcnParse(rel, strings.Join(lines, "\n"), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
}

func summary(ds []decision) string {
	var out []string
	for _, d := range ds {
		out = append(out, d.status+" "+d.id+" "+d.title)
	}
	return strings.Join(out, "\n")
}

// A register: a file whose title names a set of decisions has one under each
// `##`, by its own id and with the id, numbering and date off its title. Where
// most entries carry an id the ones that do not are front matter; and a `##`
// that is itself a set of open questions lists those questions, open.
func TestARegisterHasADecisionUnderEachEntry(t *testing.T) {
	got := parsed(t, "_docs/spec/stack-decisions.md",
		"# Stack decisions",
		"",
		"## How to read this",
		"Dated, newest last.",
		"",
		"## D1 — Runtime: Node everywhere",
		"Node 24 on every service.",
		"",
		"## D2 — Persistence: MongoDB v8.3 (2026-07-02)",
		"Mongo locally and on Railway.",
		"",
		"## Open questions (still not decided)",
		"",
		"1. **Schema/migration tooling** for MongoDB.",
		"2. **Node framework** hosting the services.",
	)
	want := "decided D1 Runtime: Node everywhere\n" +
		"decided D2 Persistence: MongoDB v8.3 (2026-07-02)\n" +
		"open 1 Schema/migration tooling\n" +
		"open 2 Node framework"
	if s := summary(got); s != want {
		t.Errorf("got\n%s\nwant\n%s", s, want)
	}
	if got[1].when.Format("2006-01-02") != "2026-07-02" {
		t.Errorf("D2 is dated %v, want its heading's date", got[1].when)
	}
	if got[0].decision != "Node 24 on every service." || got[0].by != "_docs/spec/stack-decisions.md:6" {
		t.Errorf("D1 = %q at %s", got[0].decision, got[0].by)
	}
}

// A record of rulings says each one's state in capitals, and a pending table
// keeps its settled rows struck through: settled is decided, withdrawn is
// superseded, and only what is still pending is open.
func TestARecordOfRulingsReadsItsMarkers(t *testing.T) {
	got := parsed(t, "_docs/project-management/r2-decision-record.md",
		"# R2 Decision Record",
		"",
		"## 1. R2 runs on the cross-app sync foundation — RATIFIED (direction), 2026-07-28",
		"Body.",
		"",
		"## 9. ~~PROPOSAL~~ WITHDRAWN 2026-07-30: drop the coach home screen from R2",
		"Overtaken by the release cut.",
		"",
		"## 13. Pending decisions (owners, and what closes them)",
		"",
		"| # | Decision | Owner | Needed to close |",
		"| --- | --- | --- | --- |",
		"| 1 | ~~Drop coach home (§9)~~ — **CLOSED 2026-07-30**: withdrawn | — | — |",
		"| 2 | ~~**The cached value package**~~ — **CLOSED 2026-08-21.** Ruled and shipped. | — | — |",
		"| 3 | Chat vendor contract | PM | a signed order |",
	)
	want := "decided 1 R2 runs on the cross-app sync foundation\n" +
		"superseded 9 drop the coach home screen from R2\n" +
		"superseded 1 Drop coach home (§9)\n" +
		"decided 2 The cached value package\n" +
		"open 3 Chat vendor contract"
	if s := summary(got); s != want {
		t.Errorf("got\n%s\nwant\n%s", s, want)
	}
}

// A file named for one decision is that decision, its state read from its
// status line however it is dressed. A file named for a set of them is not.
func TestAFileNamedForOneDecisionIsThatDecision(t *testing.T) {
	got := parsed(t, "_docs/spec/sync-retention-decision.md",
		"# Sync transport retention — a decision point, not a change",
		"",
		"> **Status:** [PROPOSED] 2026-08-25 — a written decision point.",
		"",
		"## 1. Options",
		"- keep 7 days",
	)
	if s := summary(got); s != "open  Sync transport retention — a decision point, not a change" {
		t.Errorf("got %q", s)
	}
	if got := parsed(t, "DECISIONS.md", "# Decisions", "", "- **One** first", "- **Two** second"); len(got) != 2 {
		t.Errorf("DECISIONS.md read as %d decisions, want its two bullets", len(got))
	}
}

// Sections, in the shapes plans and specs write them: decided ones by any of
// their names, open ones by theirs, a sub-heading per decision with its own
// bullets and tables inside it rather than beside it, and a set of decisions
// holding a "Decided" sub-section read through it. A heading that only uses
// the word is not one.
func TestSectionsNamedForDecisionsWhereverTheyAre(t *testing.T) {
	got := parsed(t, "docs/sync-bridge-plans/07-e7-notifications.md",
		"# E7 — notifications",
		"",
		"## Decision Graph Workflow",
		"- not a decision",
		"",
		"## 4. Design decisions this plan makes, and why",
		"",
		"### 4.1 The gate: two keys, both required",
		"",
		"- Key A — the type is allowlisted",
		"- Key B — the recipient is bridgeable",
		"",
		"| reason | meaning |",
		"|---|---|",
		"| not_a_roster_player | refused |",
		"",
		"### 4.2 The context resolution rule",
		"Resolved at the edge.",
		"",
		"## 9. Decisions",
		"",
		"### 9.1 Decided",
		"- Player push is owned by the player backend.",
		"",
		"## Open decisions",
		"- Retention on sync_outbox",
		"",
		"## Things explicitly NOT decided",
		"- Backend framework beyond auth",
	)
	want := "decided 4.1 The gate: two keys, both required\n" +
		"decided 4.2 The context resolution rule\n" +
		"decided 1 Player push is owned by the player backend\n" +
		"open 1 Retention on sync_outbox\n" +
		"open 1 Backend framework beyond auth"
	if s := summary(got); s != want {
		t.Errorf("got\n%s\nwant\n%s", s, want)
	}
	if got[0].consequences != "Key A — the type is allowlisted · Key B — the recipient is bridgeable" {
		t.Errorf("the gate's own bullets = %q, want them as what follows from it", got[0].consequences)
	}
}

// A heading can be one ruling on its own, in a document that is not about
// decisions at all: numbered, marked, or both, the marker and its brackets
// taken off the title.
func TestARulingHeadingIsOneDecision(t *testing.T) {
	got := parsed(t, "_docs/spec/wearable-session-model-proposal.md",
		"# Wearable Session Model",
		"",
		"## 2. Decision 1 — the session mask collection name — **[RULED] 2026-08-20**",
		"session_masks.",
		"",
		"## 3. Migration",
		"",
		"### Connected organisms — presentational-half-only (RATIFIED 2026-08-11, Decision #2)",
		"Only the presentational half.",
		"",
		"### 11.3 [SUPERSEDED by D13] The three positions v0.1 costed",
		"Gone.",
		"",
		"### Core decision — Open WebUI shell behind a pipe",
		"",
		"## 4. What the session is",
		"Not a ruling.",
	)
	want := "decided 1 Decision 1 — the session mask collection name\n" +
		"decided 2 Connected organisms — presentational-half-only\n" +
		"superseded 11.3 The three positions v0.1 costed\n" +
		"decided  Core decision — Open WebUI shell behind a pipe"
	if s := summary(got); s != want {
		t.Errorf("got\n%s\nwant\n%s", s, want)
	}
	if got[0].when.Format("2006-01-02") != "2026-08-20" {
		t.Errorf("the ruling is dated %v, want its marker's date", got[0].when)
	}
}

// [NEEDS DECISION] marks an open question: titled by the item's lead where the
// marker follows it, by what it says next where the marker opens it or sits
// mid-paragraph, across the lines the sentence wraps onto. A line that only
// talks about the marker is not one.
func TestALineMarkedNeedsDecisionIsAnOpenQuestion(t *testing.T) {
	got := parsed(t, "docs/r2-coach-response.md",
		"# R2 — our response",
		"",
		"> **Status:** DRAFT — items marked **[NEEDS DECISION]** are not yet agreed on our",
		"> side.",
		"",
		"**[NEEDS DECISION — our side]** Whether we add a unique index on `User.email`. It would",
		"refuse duplicates.",
		"",
		"- **(ii) \"Keep `tpd` queryable\"** — **[NEEDS DECISION]**. As replicated today.",
		"",
		"The bind writes once and the coach app reads it as",
		"replicated. **[NEEDS DECISION]** — this needs a named owner on our side to make the",
		"introduction.",
		"",
		"See the open `[NEEDS DECISION]` items in that file.",
	)
	want := "open  Whether we add a unique index on User.email\n" +
		"open  (ii) \"Keep tpd queryable\"\n" +
		"open  this needs a named owner on our side to make the introduction"
	if s := summary(got); s != want {
		t.Errorf("got\n%s\nwant\n%s", s, want)
	}
}

// A group is named for the file a decision came from, its folder beside it —
// with the numbering and dates that say when or in what order taken off — and
// for the folder where the file's name says nothing, or where the file is one
// ADR of a set. A generic file at the root is the plain list.
func TestGroupsAreNamedForTheFileOrItsFolder(t *testing.T) {
	for _, c := range []struct {
		rel          string
		adr          bool
		group, where string
	}{
		{"_docs/spec/stack-decisions.md", false, "stack decisions", "_docs/spec"},
		{"_docs/project-management/r2-decision-record.md", false, "r2 decision record", "_docs/project-management"},
		{"docs/sync-bridge-plans/03-e3-team-snapshots.md", false, "e3 team snapshots", "docs/sync-bridge-plans"},
		{"_docs/plans/oracle-2026-07-17.md", false, "oracle", "_docs/plans"},
		{"docs/adr/0001-parking-is-an-annotation.md", true, "adr", "docs"},
		{"docs/licensing-and-approvals/README.md", false, "licensing and approvals", "docs"},
		{"CLAUDE.md", false, "", ""},
		{"MIGRATION-PLAN.md", false, "migration plan", ""},
	} {
		g, w := dcnGroup(c.rel, c.adr)
		if g != c.group || w != c.where {
			t.Errorf("%s: group %q in %q, want %q in %q", c.rel, g, w, c.group, c.where)
		}
	}
}

// One decision written down in two places — the ADR and the CLAUDE.md bullet
// that summarises it — is listed once, as the fuller of the two.
func TestADecisionWrittenTwiceIsListedOnce(t *testing.T) {
	list := []decision{
		{title: "Parking is an annotation, never a status.", decision: "short", by: "CLAUDE.md:308"},
		{title: "Open", decision: "a"},
		{title: "Parking is an annotation, never a status", context: "long context", decision: "long decision",
			consequences: "and more", by: "docs/adr/0001.md:1"},
		{title: "Open", decision: "b"},
	}
	got := dcnDedupe(list)
	if len(got) != 3 {
		t.Fatalf("got %d, want 3 — one parking, both short titles: %+v", len(got), got)
	}
	for _, d := range got {
		if strings.HasPrefix(d.title, "Parking") && d.by != "docs/adr/0001.md:1" {
			t.Errorf("kept the summary at %s, want the ADR", d.by)
		}
	}
}

// The list a reader moves through: the root's decisions with no header, then
// each group under its name and folder, the cursor's row always on screen and
// what is cut counted.
func TestTheListIsGroupedAndKeepsTheCursorInView(t *testing.T) {
	var list []decision
	list = append(list, decision{id: "1", title: "Backend framework", status: "open", by: "CLAUDE.md:271"})
	for i := 1; i <= 60; i++ {
		list = append(list, decision{id: "D" + itoa(i), title: "Stack choice " + itoa(i), status: "decided",
			by: "_docs/spec/stack-decisions.md:" + itoa(i), group: "stack decisions", where: "_docs/spec"})
	}
	list = append(list, decision{id: "1", title: "Product rules", status: "decided", group: "payments", where: "_docs/spec"})

	out := ansi.Strip(decMidBlock(70, 30, "player-app", list, 61))
	for _, want := range []string{"62 decisions · 1 open", "payments", "_docs/spec · 1", "▸", "Product rules", "more above"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list near its end does not show %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "\n") + 1; n > 30 {
		t.Errorf("the list is %d lines in a 30-line budget", n)
	}
	top := ansi.Strip(decMidBlock(70, 30, "player-app", list, 0))
	if !strings.Contains(top, "Backend framework") || !strings.Contains(top, "stack decisions") || !strings.Contains(top, "more below") {
		t.Errorf("the top of the list:\n%s", top)
	}
	if strings.Index(top, "Backend framework") > strings.Index(top, "stack decisions") {
		t.Error("the root's decisions must come before the first group")
	}
}

// [ and ] step a group at a time: forward to the next group's first decision,
// back to the start of this one and then to the previous group's.
func TestBracketsStepAGroupAtATime(t *testing.T) {
	list := []decision{{group: ""}, {group: "a"}, {group: "a"}, {group: "a"}, {group: "b"}}
	for _, c := range []struct {
		at      int
		forward bool
		want    int
	}{
		{0, true, 1}, {2, true, 4}, {4, true, 4},
		{3, false, 1}, {1, false, 0}, {4, false, 1}, {0, false, 0},
	} {
		if got := decGroupStep(list, c.at, c.forward); got != c.want {
			t.Errorf("from %d forward=%v: %d, want %d", c.at, c.forward, got, c.want)
		}
	}
}

// o hands the file to the human's editor at the decision's line, in the
// spelling that editor takes.
func TestOpenNamesTheLineTheWayTheEditorTakesIt(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"nvim", "code", "hx"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("VISUAL", "")
	for _, c := range []struct{ editor, want string }{
		{"nvim", "nvim +12 /r/x.md"},
		{"code --wait", "code --wait -g /r/x.md:12"},
		{"hx", "hx /r/x.md:12"},
	} {
		t.Setenv("EDITOR", c.editor)
		cmd := decEditorCommand("/r/x.md", 12)
		if cmd == nil {
			t.Fatalf("%s: no command", c.editor)
		}
		if got := filepath.Base(cmd.Args[0]) + " " + strings.Join(cmd.Args[1:], " "); got != c.want {
			t.Errorf("%s: %q, want %q", c.editor, got, c.want)
		}
	}
	t.Setenv("EDITOR", "")
	if decEditorCommand("/r/x.md", 12) != nil {
		t.Error("with no editor and no vi on PATH there is nothing to run")
	}
}

// The repos are listed under their products, the one with the most still
// open first, and the screen says how many were read and had none.
func TestTheReposAreGroupedByProductMostOpenFirst(t *testing.T) {
	saved := captureVars()
	defer restoreVars(saved)
	decisions = map[string][]decision{
		"design-system":     {{status: "decided"}},
		"player-app":        {{status: "open"}, {status: "open"}, {status: "decided"}},
		"claude-dispatcher": {{status: "decided"}},
	}
	decisionRepoOrder = []string{"player-app", "design-system", "claude-dispatcher"}
	decisionProduct = map[string]string{"player-app": "playerpulse", "design-system": "playerpulse"}
	decisionNone = 21

	out := ansi.Strip(newModel().decLeftBlock(40, "player-app"))
	for _, want := range []string{"playerpulse", "▸ player-app", "2◆", "unassigned", "claude-dispatcher", "21 repos with none found"} {
		if !strings.Contains(out, want) {
			t.Errorf("left column lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "playerpulse") > strings.Index(out, "player-app") ||
		strings.Index(out, "unassigned") > strings.Index(out, "claude-dispatcher") {
		t.Errorf("a product's label must come before its repos:\n%s", out)
	}
}

// A status is the decision's own: the first state word in its status line, and
// for a register entry a status line at its top. D46's body says, a page down,
// "Status: ON for the iOS lane, DARK everywhere else. (Superseded by …)" about
// a feature it describes, and the entry was listed as superseded for it.
func TestAStatusIsTheDecisionsOwnNotWhatItDescribes(t *testing.T) {
	for in, want := range map[string]string{
		"Accepted. Supersedes 0003":                        "decided",
		"Superseded by 0012":                               "superseded",
		"[PROPOSED] 2026-08-25 — a written decision point": "open",
		"ON for the iOS lane, DARK everywhere else. (Superseded by the frozen calendar)": "decided",
	} {
		if got := dcnStatus(in); got != want {
			t.Errorf("dcnStatus(%q) = %q, want %q", in, got, want)
		}
	}
	body := []string{"# Stack decisions", "", "## D46 — One clock seam per side", "Ruled 2026-09-16.", "", "", "", "", "",
		"**Status: retired for the iOS lane.** *(Superseded by the frozen calendar.)*", "", "## D47 — Next"}
	got := parsed(t, "stack-decisions.md", body...)
	if len(got) != 2 || got[0].status != "decided" {
		t.Errorf("D46 = %+v, want decided: a status line deep in its body is not its own", got)
	}
}
