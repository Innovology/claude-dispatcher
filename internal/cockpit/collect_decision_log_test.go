package cockpit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDcnIsDecisionHeading(t *testing.T) {
	cases := map[string]bool{
		"Decisions":                     true,
		"Agreed decisions":              true,
		"Agreed decisions (2026-08-06)": true,
		"Architecture Decision Records": true,
		"Key architectural decisions":   true,
		"Decision log":                  true,
		"Decision History & Evolution":  true,
		"Design decisions & rationale":  true,
		"Decisions made":                true,
		"ADRs":                          true,
		"**Decisions**":                 true,
		// A page about a decision tool is not a log of decisions. This is the
		// case that turned one repo's CLAUDE.md into 56 "records".
		"Decision Graph Workflow":                  false,
		"Decision Graph Management":                false,
		"Decision tree":                            false,
		"Decision matrix":                          false,
		"How we make decisions":                    false,
		"Technical findings that affect decisions": false,
		"Conventions":                              false,
		"Architecture map":                         false,
	}
	for in, want := range cases {
		if got := dcnIsDecisionHeading(in); got != want {
			t.Errorf("dcnIsDecisionHeading(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDcnPlain(t *testing.T) {
	cases := map[string]string{
		"**bold** and `code`":      "bold and code",
		"*Repo* is the primitive":  "Repo is the primitive",
		"see [the docs](http://x)": "see the docs",
		"~~dropped~~":              "dropped",
		"snake_case_name stays":    "snake_case_name stays",
	}
	for in, want := range cases {
		if got := dcnPlain(in); got != want {
			t.Errorf("dcnPlain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDcnLogTitle(t *testing.T) {
	cases := []struct{ lead, prose, want string }{
		// The author's own name for the decision wins.
		{"**Done means live** — a feature stays open", "Done means live — a feature stays open", "Done means live"},
		// Short enough to show whole: no cut, even though it has a colon.
		{"", `Upgraded from quality: "low" to "high" for better detail`, `Upgraded from quality: "low" to "high" for better detail`},
		// Too long: cut at the first sentence break.
		{"", `"Done means live": a feature stays open until deployed, unless explicitly stated otherwise and then some more`, `"Done means live"`},
		// Too long with no break at all: elided on a word boundary.
		{"", strings.Repeat("word ", 30), "word word word word word word word word word word word word word word…"},
	}
	for _, c := range cases {
		if got := dcnLogTitle(c.lead, c.prose); got != c.want {
			t.Errorf("dcnLogTitle(%q) = %q, want %q", c.prose, got, c.want)
		}
	}
}

func TestDcnLogStatus(t *testing.T) {
	cases := map[string]string{
		"we use postgres":                           "decided",
		"~~we used mysql~~ moved on":                "superseded",
		"the old rule, superseded by the one below": "superseded",
		"(proposed) move the queue out of process":  "open",
		"still open: which broker":                  "open",
		"Status: Draft":                             "open",
		// A record that supersedes an earlier call is itself decided — the
		// keyword sweep the ADR parser uses would read this as superseded.
		"Multi-worktree. (Supersedes the original single-checkout call.)": "decided",
	}
	for in, want := range cases {
		if got := dcnLogStatus(in); got != want {
			t.Errorf("dcnLogStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDcnLogRecordsBullets covers the shape this repo's own CLAUDE.md is in:
// one dated heading, bullets as records, nested bullets as consequences, and
// wrapped lines that must land on whichever bullet opened them.
func TestDcnLogRecordsBullets(t *testing.T) {
	md := strings.Join([]string{
		"# Project", // 1
		"",          // 2
		"## Agreed decisions (2026-01-05, amended 2026-02-09)", // 3
		"",                                    // 4
		"How this project settled its shape.", // 5
		"",                                    // 6
		"- **Multi-worktree** — three axes:",  // 7
		"  - *Repo* is the organising primitive;",             // 8
		"    discovery is via configured roots.",              // 9
		"  - *Worktree* is per-dispatch isolation",            // 10
		"- Sessions run under tmux; it is a hard dependency.", // 11
		"  The cockpit is a stateless viewer.",                // 12
		"",                                                    // 13
		"## Build",                                            // 14
		"- make build",                                        // 15
	}, "\n")

	recs := dcnParse("CLAUDE.md", md, time.Now().Add(-72*time.Hour))
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(recs), recs)
	}

	first := recs[0]
	if first.title != "Multi-worktree" {
		t.Errorf("title = %q, want Multi-worktree", first.title)
	}
	if first.by != "CLAUDE.md:7" {
		t.Errorf("by = %q, want CLAUDE.md:7", first.by)
	}
	if first.context != "How this project settled its shape." {
		t.Errorf("context = %q, want the section preamble", first.context)
	}
	// The heading's later date dates the section, not the file's mtime.
	if got := first.when.Format("2006-01-02"); got != "2026-02-09" {
		t.Errorf("dated %s, want 2026-02-09", got)
	}
	// A nested bullet's wrapped second line belongs to that bullet, not to the
	// record's own prose — the difference between "three axes:" and that
	// phrase with a stray half-sentence welded on.
	if first.decision != "Multi-worktree — three axes:" {
		t.Errorf("decision = %q", first.decision)
	}
	want := "Repo is the organising primitive; discovery is via configured roots. · Worktree is per-dispatch isolation"
	if first.consequences != want {
		t.Errorf("consequences = %q, want %q", first.consequences, want)
	}

	second := recs[1]
	if second.id != "2" || second.by != "CLAUDE.md:11" {
		t.Errorf("second record id=%q by=%q", second.id, second.by)
	}
	// An unindented wrapped line continues the record it follows.
	if !strings.HasSuffix(second.decision, "The cockpit is a stateless viewer.") {
		t.Errorf("second decision = %q", second.decision)
	}
	// "## Build" closes the section: its bullet is not a decision.
	for _, r := range recs {
		if strings.Contains(r.decision, "make build") {
			t.Errorf("section ran past its heading: %+v", r)
		}
	}
}

// TestDcnLogRecordsHeadings covers a section written as sub-headings, and the
// code fence that must not be mistaken for one.
func TestDcnLogRecordsHeadings(t *testing.T) {
	md := strings.Join([]string{
		"## Decisions",
		"",
		"### Postgres over DynamoDB",
		"",
		"Relational access patterns won.",
		"",
		"```bash",
		"# Not a heading",
		"- not a record",
		"```",
		"",
		"### Queue is SQS",
		"",
		"Status: proposed",
	}, "\n")

	mod := time.Now().Add(-24 * time.Hour)
	recs := dcnParse("DECISIONS.md", md, mod)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(recs), recs)
	}
	if recs[0].title != "Postgres over DynamoDB" || recs[0].decision != "Relational access patterns won." {
		t.Errorf("first = %+v", recs[0])
	}
	if !recs[0].when.Equal(mod) {
		t.Errorf("dated %v, want the file's time when the heading carries no date", recs[0].when)
	}
	// The section has no preamble, so the context names where it was read.
	if !strings.Contains(recs[0].context, "DECISIONS.md · Decisions") {
		t.Errorf("context = %q", recs[0].context)
	}
	if recs[1].status != "open" {
		t.Errorf("second status = %q, want open", recs[1].status)
	}
}

func TestDecisionsAreReadFromEveryFileThatHasThem(t *testing.T) {
	if got := dcnMarkdown(t.TempDir()); len(got) != 0 {
		t.Errorf("empty repo: found %v", got)
	}
	repo := t.TempDir()

	md := "## Decisions\n\n- We ship on Fridays.\n"
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs", "ops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "ops", "release-notes.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	var got []decision
	for _, rel := range dcnMarkdown(repo) {
		got = append(got, dcnFileRecords(repo, rel)...)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want one from each file: %+v", len(got), got)
	}
	if got[0].title != "We ship on Fridays" {
		t.Errorf("title = %q", got[0].title)
	}
	// The root file is in no group; the other is named for itself, with its
	// folder beside it.
	if got[0].group != "" || got[1].group != "release notes" || got[1].where != "docs/ops" {
		t.Errorf("groups = %q, %q (%q)", got[0].group, got[1].group, got[1].where)
	}
}
