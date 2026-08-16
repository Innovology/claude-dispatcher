package cockpit

// collect_decision_log.go is the DECISIONS lens's second source: decisions
// written as prose rather than as an adr-tools folder.
//
// The lens shipped reading only doc/adr-style directories. Across a real fleet
// that found nothing at all — not one repo keeps an ADR folder — while the
// decisions themselves were sitting in plain sight in each repo's CLAUDE.md
// under a "## Agreed decisions" heading. A lens that reports "no decision
// records found in any repo" over a repo whose decisions are written down is
// not an honest empty state, it is a scanner looking in one place.
//
// So: find any heading whose name says decisions, and read the records under
// it. Each top-level bullet (or each sub-heading, when the section is written
// that way) is one record. Nothing here writes; a repo with no such heading
// simply contributes nothing.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dcnLogFiles are the conventional markdown files a decision section lives in,
// relative to a repo root. Every one that exists is read — a repo may keep its
// agreed decisions in CLAUDE.md and its architecture rationale in
// docs/ARCHITECTURE.md, and both are records.
//
// The list is case-doubled on purpose: a case-insensitive filesystem hands back
// the same file for two spellings, which dcnScanLogs de-duplicates, and a
// case-sensitive one needs both spellings tried.
var dcnLogFiles = []string{
	"DECISIONS.md", "docs/DECISIONS.md", "docs/decisions.md",
	"CLAUDE.md", "AGENTS.md",
	"ARCHITECTURE.md", "docs/ARCHITECTURE.md", "docs/architecture.md",
	"README.md",
}

// dcnHeading matches a markdown ATX heading, capturing its level and its text.
var dcnHeading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

// dcnBullet matches a list marker at any indent, capturing the indent and the
// text after the marker.
var dcnBullet = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+(.*)$`)

// dcnBoldLead matches a bolded lead-in at the head of a bullet — the convention
// that names a decision before explaining it.
var dcnBoldLead = regexp.MustCompile(`^\*\*(.+?)\*\*`)

// dcnISODate matches an ISO date, used to date a section from its heading.
var dcnISODate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

var (
	// dcnParen strips a trailing parenthetical from a heading — a date stamp,
	// usually — so the phrase test sees the heading's actual name.
	dcnParen = regexp.MustCompile(`\s*\([^)]*\)\s*$`)
	// dcnWord tokenises a heading, keeping "&" as a word so a phrase that ends
	// on a conjunction can be told from one that runs on into another noun.
	dcnWord = regexp.MustCompile(`[a-z&]+`)

	// dcnRecordWords are the nouns that keep a heading a set of decisions: a
	// "decision log" is a log of them, a "decision history" a history of them.
	dcnRecordWords = map[string]bool{
		"log": true, "logs": true, "record": true, "records": true,
		"history": true, "register": true, "journal": true, "list": true,
		"index": true, "made": true, "taken": true, "notes": true,
		"and": true, "&": true, "or": true,
	}
)

// dcnIsDecisionHeading reports whether a heading names a set of decisions.
//
// Matching any heading with "decision" in it is far too loose, and the fleet
// proves it: "## Decision Graph Workflow" is a page about driving a tool, and
// reading it as a decision log turned 56 headings like "Available Slash
// Commands" into architecture decisions. Two things separate a log from a
// document about decisions — "decision" has to be near the front of the
// heading, so "How we make decisions" (a process) is not one, and it has to be
// the thing being listed rather than a modifier of something else, so
// "decision history" is a log and "decision graph" is not.
func dcnIsDecisionHeading(text string) bool {
	s := dcnParen.ReplaceAllString(strings.ToLower(dcnPlain(text)), "")
	words := dcnWord.FindAllString(s, -1)
	at := -1
	for i, w := range words {
		if w == "decision" || w == "decisions" {
			at = i
			break
		}
	}
	if at < 0 {
		return len(words) == 1 && (words[0] == "adr" || words[0] == "adrs")
	}
	if at > 2 {
		return false
	}
	return at == len(words)-1 || dcnRecordWords[words[at+1]]
}

// dcnScanLogs reads every decision section in a repo's conventional markdown
// and returns the records found, in file order. A repo with none returns nil.
func dcnScanLogs(repoPath string) []decision {
	var out []decision
	seen := map[string]bool{}
	for _, rel := range dcnLogFiles {
		key := strings.ToLower(rel)
		if seen[key] {
			continue
		}
		path := filepath.Join(repoPath, filepath.FromSlash(rel))
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		seen[key] = true
		fallback := ""
		if info, err := os.Stat(path); err == nil {
			fallback = dcnAge(info.ModTime())
		}
		out = append(out, dcnLogRecords(rel, string(raw), fallback)...)
	}
	return out
}

// dcnLogRecords parses one markdown file into decision records: every heading
// whose text mentions decisions opens a section, and the section's contents
// become the records. at is the age used for records whose section heading
// carries no date of its own.
func dcnLogRecords(rel, text, at string) []decision {
	lines := strings.Split(text, "\n")
	var out []decision
	fence := false

	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		h := dcnHeading.FindStringSubmatch(lines[i])
		if h == nil || !dcnIsDecisionHeading(h[2]) {
			continue
		}
		level := len(h[1])

		// The section runs to the next heading at the same level or higher.
		end := len(lines)
		inner := false
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
				inner = !inner
				continue
			}
			if inner {
				continue
			}
			if m := dcnHeading.FindStringSubmatch(lines[j]); m != nil && len(m[1]) <= level {
				end = j
				break
			}
		}

		when := at
		if d := dcnISODate.FindAllString(h[2], -1); len(d) > 0 {
			// The most recent date in the heading: "(2026-08-06, worktrees
			// added 2026-08-07)" is dated by the amendment, not the original.
			if t, err := time.Parse("2006-01-02", d[len(d)-1]); err == nil {
				when = dcnAge(t)
			}
		}
		src := rel + " · " + dcnPlain(h[2])
		out = append(out, dcnSectionRecords(lines[i+1:end], i+2, level, rel, src, when)...)
		i = end - 1
	}
	return out
}

// dcnSectionRecords turns the body of one decision section into records. A
// section written as sub-headings yields one record per sub-heading; otherwise
// each top-level bullet is a record, and the bullets nested under it are what
// follows from it — the nearest thing a prose log has to an ADR's
// consequences. The section's own preamble becomes every record's context.
func dcnSectionRecords(body []string, firstLine, level int, rel, src, at string) []decision {
	type block struct {
		line   int
		title  string // set when the block came from a sub-heading
		lead   string // the record's opening line, markdown intact
		prose  []string
		conseq []string
	}
	var blocks []block
	var preamble []string
	fence := false

	// A markdown bullet wraps onto following indented lines, so an unmarked
	// line belongs to whatever was last opened. Tracking which — the record's
	// own prose or its latest nested bullet — is the difference between
	// "three independent axes:" and that phrase with a nested bullet's second
	// line welded onto the end of it.
	const (
		intoNothing = iota
		intoProse
		intoConseq
	)
	into := intoNothing

	for n, ln := range body {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if h := dcnHeading.FindStringSubmatch(ln); h != nil && len(h[1]) > level {
			blocks = append(blocks, block{line: firstLine + n, title: dcnPlain(h[2])})
			into = intoProse
			continue
		}
		if b := dcnBullet.FindStringSubmatch(ln); b != nil {
			if len(b[1]) == 0 {
				blocks = append(blocks, block{
					line: firstLine + n, lead: b[2], prose: []string{dcnPlain(b[2])},
				})
				into = intoProse
				continue
			}
			if len(blocks) == 0 {
				into = intoNothing // a nested bullet with nothing to hang off
				continue
			}
			blocks[len(blocks)-1].conseq = append(blocks[len(blocks)-1].conseq, dcnPlain(b[2]))
			into = intoConseq
			continue
		}
		if t == "" {
			// A bullet's continuation ends at the blank line. A sub-heading's
			// body starts after one, so a heading block stays open.
			into = intoNothing
			if len(blocks) > 0 && blocks[len(blocks)-1].title != "" {
				into = intoProse
			}
			continue
		}
		if len(blocks) == 0 {
			preamble = append(preamble, dcnPlain(t))
			continue
		}
		last := &blocks[len(blocks)-1]
		switch into {
		case intoProse:
			last.prose = append(last.prose, dcnPlain(t))
		case intoConseq:
			c := len(last.conseq) - 1
			last.conseq[c] = strings.TrimSpace(last.conseq[c] + " " + dcnPlain(t))
		}
	}

	ctx := strings.Join(preamble, " ")
	if ctx == "" {
		ctx = "recorded in " + src
	}

	var out []decision
	for _, b := range blocks {
		prose := strings.TrimSpace(strings.Join(b.prose, " "))
		title := b.title
		if title == "" {
			title = dcnLogTitle(b.lead, prose)
		}
		if title == "" {
			continue // a bullet with nothing in it is not a decision
		}
		out = append(out, decision{
			id:           strconv.Itoa(len(out) + 1),
			title:        title,
			status:       dcnLogStatus(prose),
			at:           at,
			by:           rel + ":" + strconv.Itoa(b.line),
			context:      ctx,
			decision:     prose,
			consequences: strings.Join(b.conseq, " · "),
		})
	}
	return out
}

// dcnTitleMax is the widest a derived title may be before it is elided. Titles
// taken from a heading or a bold lead-in are left alone; only a title carved
// out of running prose can run away.
const dcnTitleMax = 72

// dcnTitleCuts are the punctuation marks a prose bullet's opening statement
// ends at, in the order they are searched for.
var dcnTitleCuts = []string{" — ", " – ", ": ", "; ", ". "}

// dcnLogTitle derives a record's one-line title: the bolded lead-in if the
// bullet has one, else its opening statement up to the first sentence break,
// elided if that still runs long. lead is the record's opening line with its
// markdown intact — the bold has to be read before dcnPlain strips it, or the
// author's own name for the decision is lost with the asterisks.
func dcnLogTitle(lead, prose string) string {
	if m := dcnBoldLead.FindStringSubmatch(strings.TrimSpace(lead)); m != nil {
		if t := strings.TrimRight(dcnPlain(m[1]), ".,;:· "); t != "" {
			return dcnElide(t, dcnTitleMax)
		}
	}
	plain := strings.TrimRight(dcnPlain(prose), ".,;:· ")
	// A record short enough to show whole is shown whole. Cutting at the first
	// break regardless is how `Upgraded from "low" to "high" for better detail`
	// became the record "Upgraded from" — a fragment, where the whole line
	// would have fitted.
	if len([]rune(plain)) <= dcnTitleMax {
		return plain
	}
	cut := len(plain)
	for _, sep := range dcnTitleCuts {
		if i := strings.Index(plain, sep); i > 0 && i < cut {
			// A break inside "e.g. x" or "vs. y" is not a sentence ending; a
			// title that short is a fragment, so keep looking.
			if i >= 12 {
				cut = i
			}
		}
	}
	return dcnElide(strings.TrimRight(plain[:cut], ".,;:· "), dcnTitleMax)
}

// dcnElide shortens s to at most max columns, breaking on a word where it can.
func dcnElide(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)[:max]
	if i := strings.LastIndex(string(r), " "); i > max/2 {
		r = []rune(string(r)[:i])
	}
	return strings.TrimRight(string(r), " ,;:·") + "…"
}

// dcnLogStatus reads a prose record's lifecycle state. A written-down decision
// is an agreed one, so the default is accepted and only an explicit marker
// moves it: dcnStatus's keyword sweep cannot be used here, because a record
// that says it *supersedes* an earlier call would be read as superseded itself.
func dcnLogStatus(prose string) string {
	low := strings.ToLower(prose)
	switch {
	case strings.HasPrefix(strings.TrimSpace(prose), "~~"),
		strings.Contains(low, "superseded by"), strings.Contains(low, "replaced by"),
		strings.Contains(low, "no longer holds"):
		return "superseded"
	case strings.Contains(low, "(proposed"), strings.HasPrefix(low, "proposed:"),
		strings.Contains(low, "to be decided"), strings.Contains(low, "not yet agreed"),
		strings.Contains(low, "still open"):
		return "proposed"
	}
	if s := dcnInlineStatus(prose); s != "" {
		return s
	}
	return "accepted"
}

// dcnPlain strips the inline markdown a prose log is written in. The lens has
// no markdown renderer, so emphasis markers and link syntax would otherwise
// reach the pane as literal asterisks and brackets.
func dcnPlain(s string) string {
	s = strings.TrimSpace(s)
	s = dcnLink.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "~~", "")
	s = dcnItalic.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

var (
	// dcnLink rewrites [text](href) to text.
	dcnLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	// dcnItalic strips single-asterisk emphasis around a word run. Underscore
	// emphasis is left alone on purpose: snake_case identifiers are far more
	// common in these files than _emphasis_ is.
	dcnItalic = regexp.MustCompile(`\*([^*]+)\*`)
)
