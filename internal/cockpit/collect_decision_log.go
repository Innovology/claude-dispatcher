package cockpit

// collect_decision_log.go decides what in a markdown file is a decision, by its
// shape alone: collect_decisions.go hands it every file in a repo, wherever it
// sits. The shapes are the ones repos were found writing, and each line of a
// file is claimed by at most one of them, tried in this order:
//
//  1. an ADR — a file with Status / Context / Decision / Consequences
//     sections — is one decision;
//  2. a register — a file whose title names a set of decisions ("Stack
//     decisions", "R2 Decision Record", "Payments — decision record") — has a
//     decision under each of its `##` headings;
//  3. a file named for one decision (sync-retention-decision.md) is that one;
//  4. a section whose heading names decisions ("## Agreed decisions",
//     "Decided", "Design decisions this plan makes", "Open decisions", "Things
//     explicitly NOT decided") has one under each bullet or sub-heading;
//  5. a heading that is itself a ruling ("Decision 3 — …", "… — RATIFIED
//     2026-07-28", "[RULED]", "WITHDRAWN") is one;
//  6. a line marked [NEEDS DECISION] is an open one.
//
// The status is read from the repo's own words: RATIFIED, RULED and the like
// are decided; WITHDRAWN and SUPERSEDED, or a title struck through, are
// superseded; PROPOSED, an open or pending section and [NEEDS DECISION] are
// open. Anything written down without a marker is decided. Nothing here
// writes.

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dcnHeading matches a markdown ATX heading, capturing its level and its text.
var dcnHeading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

// dcnBullet matches a list marker at any indent, capturing the indent and the
// text after the marker.
var dcnBullet = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+(.*)$`)

// dcnBoldLead matches a bolded lead-in at the head of a bullet — the convention
// that names a decision before explaining it.
var dcnBoldLead = regexp.MustCompile(`^\*\*(.+?)\*\*`)

// dcnISODate matches an ISO date, used to date a record from its heading.
var dcnISODate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

var (
	// dcnParen strips a trailing parenthetical from a heading — a date stamp,
	// usually — so the phrase test sees the heading's actual name.
	dcnParen = regexp.MustCompile(`\s*\([^)]*\)\s*$`)
	// dcnWord tokenises a heading, keeping "&" as a word so a phrase that ends
	// on a conjunction can be told from one that runs on into another noun.
	dcnWord = regexp.MustCompile(`[a-z&]+`)

	// dcnFollowers are the words that may come after "decisions" in a heading
	// that names a set of them: the nouns of a log ("decision log", "decision
	// history"), and the words a sentence about them goes on with ("decisions
	// this plan makes", "the decision, and what it unblocks").
	dcnFollowers = map[string]bool{
		"log": true, "logs": true, "record": true, "records": true,
		"history": true, "register": true, "journal": true, "list": true,
		"index": true, "made": true, "taken": true, "notes": true,
		"and": true, "&": true, "or": true,
		"this": true, "these": true, "that": true, "we": true, "so": true,
		"to": true, "for": true, "in": true, "on": true, "about": true,
		"from": true, "by": true, "worth": true, "already": true, "here": true,
		"still": true, "agreed": true, "ruled": true,
	}
	// dcnOpenWords open a heading that names decisions still to be made.
	dcnOpenWords = map[string]bool{
		"open": true, "pending": true, "outstanding": true, "unresolved": true,
		"undecided": true, "remaining": true,
	}
	// dcnOpenNouns are what an open heading may list instead of decisions.
	dcnOpenNouns = map[string]bool{"questions": true, "question": true, "items": true, "issues": true, "points": true}
)

// dcnSectionKind reports whether a heading opens a set of decisions, and
// which: "decided", "open", or "" for neither.
//
// Matching any heading with "decision" in it is far too loose, and the fleet
// proves it: "## Decision Graph Workflow" is a page about driving a tool, and
// reading it as a decision log turned 56 headings like "Available Slash
// Commands" into architecture decisions. Two things separate a set of
// decisions from a document about them — "decision" has to be near the front
// of the heading, so "How we make decisions" (a process) is not one, and it
// has to be the thing listed rather than a modifier of something else, so
// "decision history" is a set and "decision graph" is not.
func dcnSectionKind(text string) string {
	low := strings.ToLower(dcnPlain(text))
	if strings.Contains(low, "not decided") || strings.Contains(low, "not yet decided") ||
		strings.Contains(low, "undecided") || strings.Contains(low, "needs a decision") {
		return "open"
	}
	words := dcnWord.FindAllString(dcnParen.ReplaceAllString(low, ""), -1)
	if len(words) == 1 && words[0] == "decided" {
		return "decided"
	}
	// "Open", "Open questions": what a spec has left to decide, by another name.
	if len(words) > 0 && dcnOpenWords[words[0]] &&
		(len(words) == 1 || len(words) == 2 && dcnOpenNouns[words[1]]) {
		return "open"
	}
	at := -1
	for i, w := range words {
		if w == "decision" || w == "decisions" {
			at = i
			break
		}
	}
	if at < 0 {
		if len(words) == 1 && (words[0] == "adr" || words[0] == "adrs") {
			return "decided"
		}
		return ""
	}
	if at > 2 || (at < len(words)-1 && !dcnFollowers[words[at+1]]) {
		return ""
	}
	for _, w := range words[:at] {
		if dcnOpenWords[w] {
			return "open"
		}
	}
	return "decided"
}

// dcnIsDecisionHeading reports whether a heading names a set of decisions.
func dcnIsDecisionHeading(text string) bool { return dcnSectionKind(text) != "" }

// dcnMarker is a status word a repo writes in capitals beside a ruling.
var dcnMarker = regexp.MustCompile(`\b(RATIFIED|RULED|DECIDED|ACCEPTED|CLOSED|RESOLVED|WITHDRAWN|SUPERSEDED|REJECTED|PROPOSED|PROPOSAL)\b`)

// dcnRulingHead matches a heading that names one decision rather than a set:
// "Decision 3 — …", "Decision #2", "Core decision — …", "Decision: …".
var dcnRulingHead = regexp.MustCompile(`(?i)\bdecision\s*(#?\d+\b|[—–:])`)

// dcnNeedsDecision is the inline marker for a question still open.
var dcnNeedsDecision = regexp.MustCompile(`(?i)\[needs decision[^\]]*\]`)

// dcnNeedsMention is a line that talks about the marker rather than carrying
// it: "items marked [NEEDS DECISION] are not yet agreed".
var dcnNeedsMention = regexp.MustCompile("(?i)marked\\s+[`*]*\\[needs decision|\\[needs decision[^\\]]*\\][`*]*\\s+items?\\b")

// dcnOneDecision is a file named for one decision — sync-retention-decision.md
// — and not for a set of them: decisions.md, decision-record.md and
// decision-log.md hold many.
var dcnOneDecision = regexp.MustCompile(`(?i)(^|[-_ ])decision(\.md)?$`)

// dcnLeadNum is the number an adr-tools file name starts with.
var dcnLeadNum = regexp.MustCompile(`^\d+`)

// dcnDecisionNum is the number in "Decision 3" or "Decision #2".
var dcnDecisionNum = regexp.MustCompile(`(?i)\bdecision\s*#?(\d+)`)

// dcnRecordID is a heading's own identifier: D12, 3, 9.1.
var dcnRecordID = regexp.MustCompile(`^(D\d+|\d+(?:\.\d+)*)\.?\s+(?:[—–:-]\s*)?`)

// dcnMarkerStatus reads the lens's status off a ruling's capitalised marker,
// "" where there is none. A withdrawn proposal is withdrawn.
func dcnMarkerStatus(text string) string {
	ms := dcnMarker.FindAllString(text, -1)
	if strings.HasPrefix(strings.TrimSpace(text), "~~") {
		return "superseded"
	}
	st := ""
	for _, m := range ms {
		switch m {
		case "WITHDRAWN", "SUPERSEDED", "REJECTED":
			return "superseded"
		case "PROPOSED", "PROPOSAL":
			st = "open"
		default:
			if st == "" {
				st = "decided"
			}
		}
	}
	return st
}

// dcnCleanTitle is a heading made into a record title: its own id, numbering
// and status marker taken off ("D1 — Runtime: Node everywhere" → "Runtime:
// Node everywhere"; "2. Decision 1 — the mask name — [RULED] 2026-08-20" →
// "Decision 1 — the mask name"). A marker that leads the heading
// ("WITHDRAWN 2026-07-30: drop the coach home screen") gives way to what
// follows it.
func dcnCleanTitle(text string) (id, title string) {
	t := dcnPlain(text)
	if m := dcnRecordID.FindStringSubmatch(t); m != nil {
		id = m[1]
		t = t[len(m[0]):]
	}
	return id, dcnStripMarker(t)
}

// dcnStripMarker takes a status marker off a title: cut there when the title
// comes before it ("Time-authority mechanics → RESOLVED (§8)"), or taken off
// the front with its date when it leads.
func dcnStripMarker(t string) string {
	if loc := dcnMarker.FindStringIndex(t); loc != nil {
		before := strings.Trim(t[:loc[0]], " —–-:([*~,→")
		before = strings.TrimSpace(dcnMarker.ReplaceAllString(before, ""))
		if before == "" {
			rest := t[loc[1]:]
			// "[SUPERSEDED by D13] The three positions": the bracket is the
			// marker's, all of it.
			if lead := strings.TrimRight(t[:loc[0]], " "); strings.HasSuffix(lead, "[") {
				if j := strings.Index(rest, "]"); j >= 0 {
					rest = rest[j+1:]
				}
			}
			rest = dcnMarker.ReplaceAllString(rest, "")
			rest = dcnISODate.ReplaceAllString(rest, "")
			t = strings.TrimLeft(rest, " ])—–-:,→")
		} else {
			t = strings.TrimRight(t[:loc[0]], " —–-:([*~,→")
		}
	}
	return strings.TrimSpace(t)
}

// dcnWhen dates a record from the last ISO date in its heading — "(2026-08-06,
// worktrees added 2026-08-07)" is dated by the amendment — else by its file.
func dcnWhen(text string, file time.Time) time.Time {
	if d := dcnISODate.FindAllString(text, -1); len(d) > 0 {
		if t, err := time.Parse("2006-01-02", d[len(d)-1]); err == nil {
			return t
		}
	}
	return file
}

// dcnHead is one heading outside a code fence.
type dcnHead struct {
	line, level int
	text        string
}

// dcnHeads lists a file's headings, skipping fenced code, where a "#" is a
// shell comment.
func dcnHeads(lines []string) []dcnHead {
	var out []dcnHead
	fence := false
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if h := dcnHeading.FindStringSubmatch(ln); h != nil {
			out = append(out, dcnHead{line: i, level: len(h[1]), text: strings.TrimSpace(h[2])})
		}
	}
	return out
}

// dcnEnd is where the section opened by heads[i] ends: the next heading at its
// level or above, else the end of the file.
func dcnEnd(heads []dcnHead, i, n int) int {
	for _, h := range heads[i+1:] {
		if h.level <= heads[i].level {
			return h.line
		}
	}
	return n
}

// dcnBodyMax caps the prose a record carries: the body pane wraps it, and a
// register entry can run to hundreds of lines.
const dcnBodyMax = 2400

// dcnBody is the prose under a heading, fenced code left out, as one
// paragraph.
func dcnBody(lines []string) string {
	var out []string
	fence := false
	n := 0
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			fence = !fence
			continue
		}
		if fence || t == "" || strings.HasPrefix(t, "|") || strings.Trim(t, "-*_ ") == "" {
			continue
		}
		t = strings.TrimLeft(t, "> ")
		if h := dcnHeading.FindStringSubmatch(t); h != nil {
			t = h[2]
		}
		// A list inside the prose reads as its points, not as dashes
		// in the middle of a sentence.
		if b := dcnBullet.FindStringSubmatch(t); b != nil {
			t = "· " + b[2]
		}
		p := dcnPlain(t)
		out = append(out, p)
		n += len(p) + 1
		if n > dcnBodyMax {
			break
		}
	}
	return dcnElide(strings.Join(out, " "), dcnBodyMax)
}

// dcnParse reads one file's decisions. rel is its path in the repo, mod its
// modification time, which dates the records whose headings carry no date.
func dcnParse(rel, text string, mod time.Time) []decision {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	heads := dcnHeads(lines)
	if d, ok := dcnADR(rel, lines, heads, mod); ok {
		return []decision{d}
	}
	used := make([]bool, len(lines)+1)
	claim := func(from, to int) {
		for i := from; i < to && i < len(used); i++ {
			used[i] = true
		}
	}
	var out []decision

	h1 := -1
	for i, h := range heads {
		if h.level == 1 {
			h1 = i
			break
		}
	}
	switch {
	case h1 >= 0 && dcnIsDecisionHeading(heads[h1].text) && dcnHasLevel(heads, 2):
		out = append(out, dcnRegister(rel, lines, heads, h1, mod, claim)...)
	case dcnOneDecision.MatchString(path.Base(rel)):
		return []decision{dcnSingle(rel, lines, heads, h1, mod)}
	}
	out = append(out, dcnSectionsIn(rel, lines, heads, mod, used, claim)...)
	out = append(out, dcnRulings(rel, lines, heads, mod, used, claim)...)
	out = append(out, dcnNeeds(rel, lines, mod, used)...)
	return out
}

func dcnHasLevel(heads []dcnHead, level int) bool {
	for _, h := range heads {
		if h.level == level {
			return true
		}
	}
	return false
}

// dcnADRSections are the headings an architecture decision record is made of,
// in Nygard's form and MADR's.
var dcnADRSections = map[string]bool{
	"status": true, "context": true, "decision": true, "consequences": true,
	"decision outcome": true, "context and problem statement": true,
	"considered options": true,
}

// dcnADR reads a file written as one ADR: two or more of its `##` headings are
// an ADR's sections. Its title is its first heading with the number adr-tools
// puts there ("9. A dispatch that did not happen") taken off.
func dcnADR(rel string, lines []string, heads []dcnHead, mod time.Time) (decision, bool) {
	sections := map[string]string{}
	for i, h := range heads {
		if h.level != 2 {
			continue
		}
		_, name := dcnCleanTitle(h.text)
		name = strings.ToLower(name)
		if dcnADRSections[name] {
			sections[name] = dcnBody(lines[h.line+1 : dcnEnd(heads, i, len(lines))])
		}
	}
	if len(sections) < 2 {
		return decision{}, false
	}
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	d := decision{status: "decided", by: rel + ":1", line: 1, when: mod, title: base, adr: true}
	if m := dcnLeadNum.FindString(base); m != "" {
		d.id = m
	}
	for _, h := range heads {
		if h.level == 1 {
			_, d.title = dcnCleanTitle(h.text)
			d.by, d.line = rel+":"+strconv.Itoa(h.line+1), h.line+1
			break
		}
	}
	if st, ok := sections["status"]; ok {
		d.status = dcnStatus(st)
	} else if st := dcnInlineStatus(lines); st != "" {
		d.status = st
	}
	d.context = sections["context"]
	if d.context == "" {
		d.context = sections["context and problem statement"]
	}
	d.decision = sections["decision"]
	if d.decision == "" {
		d.decision = sections["decision outcome"]
	}
	d.consequences = sections["consequences"]
	return d, true
}

// dcnRegister reads a file whose title names a set of decisions: each `##`
// under it is one. Where most of them carry an id (D12, 3.) the ones that do
// not are the register's own front matter and are left out. A `##` that is
// itself a set ("Open questions (still not decided)") is left to the section
// pass, which reads its items.
func dcnRegister(rel string, lines []string, heads []dcnHead, h1 int, mod time.Time, claim func(int, int)) []decision {
	_, regTitle := dcnCleanTitle(heads[h1].text)
	claim(heads[h1].line, heads[h1].line+1)
	var h2 []int
	withID := 0
	for i, h := range heads {
		if h.level == 2 && dcnSectionKind(h.text) == "" {
			h2 = append(h2, i)
			if dcnRecordID.MatchString(dcnPlain(h.text)) {
				withID++
			}
		}
	}
	onlyIDs := withID*2 >= len(h2)
	var out []decision
	for n, i := range h2 {
		h := heads[i]
		if onlyIDs && !dcnRecordID.MatchString(dcnPlain(h.text)) {
			continue
		}
		end := dcnEnd(heads, i, len(lines))
		id, title := dcnCleanTitle(h.text)
		if id == "" {
			id = strconv.Itoa(n + 1)
		}
		status := dcnMarkerStatus(h.text)
		if status == "" {
			// An entry's own status line is at its top; one deep in its body
			// is about something the entry describes.
			if status = dcnInlineStatus(lines[h.line+1 : mini(end, h.line+6)]); status == "" {
				status = "decided"
			}
		}
		out = append(out, decision{
			id: id, title: title, status: status,
			when: dcnWhen(h.text, mod), line: h.line + 1,
			by:       rel + ":" + strconv.Itoa(h.line+1),
			context:  "recorded in " + regTitle,
			decision: dcnBody(lines[h.line+1 : end]),
		})
		claim(h.line, end)
	}
	return out
}

// dcnSingle reads a file named for one decision as that decision: its title,
// its status line, and the prose before its first section.
func dcnSingle(rel string, lines []string, heads []dcnHead, h1 int, mod time.Time) decision {
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	d := decision{title: dcnHuman(base), when: mod, line: 1, by: rel + ":1"}
	start, end := 0, len(lines)
	if h1 >= 0 {
		_, d.title = dcnCleanTitle(heads[h1].text)
		d.line, d.by = heads[h1].line+1, rel+":"+strconv.Itoa(heads[h1].line+1)
		d.when = dcnWhen(heads[h1].text, mod)
		start = heads[h1].line + 1
		if h1+1 < len(heads) {
			end = heads[h1+1].line
		}
	}
	d.status = dcnInlineStatus(lines)
	if d.status == "" {
		if d.status = dcnMarkerStatus(d.title); d.status == "" {
			d.status = "decided"
		}
	}
	d.decision = dcnBody(lines[start:end])
	return d
}

// dcnSectionsIn reads every section whose heading names decisions. A section
// that holds another such section ("## 9. Decisions" over "### 9.1 Decided")
// is read through the inner one, whose items are the decisions.
func dcnSectionsIn(rel string, lines []string, heads []dcnHead, mod time.Time, used []bool, claim func(int, int)) []decision {
	var out []decision
	for i, h := range heads {
		kind := dcnSectionKind(h.text)
		if kind == "" || used[h.line] {
			continue
		}
		end := dcnEnd(heads, i, len(lines))
		nested := false
		for _, in := range heads[i+1:] {
			if in.line >= end {
				break
			}
			if dcnSectionKind(in.text) != "" {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		src := rel + " · " + dcnPlain(h.text)
		recs := dcnSectionRecords(lines[h.line+1:end], h.line+2, h.level, rel, src, dcnWhen(h.text, mod), kind == "open")
		out = append(out, recs...)
		claim(h.line, end)
	}
	return out
}

// dcnRulings reads headings that are each one decision, wherever they sit.
func dcnRulings(rel string, lines []string, heads []dcnHead, mod time.Time, used []bool, claim func(int, int)) []decision {
	var out []decision
	for i, h := range heads {
		if h.level == 1 || used[h.line] || dcnSectionKind(h.text) != "" {
			continue
		}
		plain := dcnPlain(h.text)
		status := dcnMarkerStatus(h.text)
		if status == "" && !dcnRulingHead.MatchString(plain) {
			continue
		}
		if status == "" {
			status = "decided"
		}
		end := dcnEnd(heads, i, len(lines))
		id, title := dcnCleanTitle(h.text)
		if m := dcnDecisionNum.FindStringSubmatch(plain); m != nil {
			id = m[1]
		}
		out = append(out, decision{
			id: id, title: title, status: status,
			when: dcnWhen(h.text, mod), line: h.line + 1,
			by:       rel + ":" + strconv.Itoa(h.line+1),
			context:  "recorded in " + rel,
			decision: dcnBody(lines[h.line+1 : end]),
		})
		claim(h.line, end)
	}
	return out
}

// dcnNeeds reads each line marked [NEEDS DECISION] outside what the passes
// before it claimed: an open question, titled by what the line asks.
func dcnNeeds(rel string, lines []string, mod time.Time, used []bool) []decision {
	var out []decision
	fence := false
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			fence = !fence
			continue
		}
		if fence || used[i] || !dcnNeedsDecision.MatchString(ln) || dcnNeedsMention.MatchString(ln) {
			continue
		}
		// The item is the marker's line and the lines that continue it. Where
		// the marker follows the item's lead ("(ii) Keep tpd queryable —
		// [NEEDS DECISION]") the lead is the question; where it opens the item,
		// or sits mid-paragraph, the question is what follows it.
		loc := dcnNeedsDecision.FindStringIndex(ln)
		before := strings.Trim(dcnPlain(strings.TrimLeft(ln[:loc[0]], " >")), " —–-:.*")
		if b := dcnBullet.FindStringSubmatch(ln[:loc[0]]); b != nil {
			before = strings.Trim(dcnPlain(b[2]), " —–-:.*")
		}
		starts := i == 0 || strings.TrimSpace(lines[i-1]) == "" || dcnBullet.MatchString(ln)
		after := []string{ln[loc[1]:]}
		for j := i + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" || dcnBullet.MatchString(lines[j]) || dcnHeading.MatchString(t) || used[j] {
				break
			}
			after = append(after, t)
		}
		rest := strings.Trim(dcnPlain(strings.Join(after, " ")), " —–-:.*")
		text, title := rest, ""
		if starts && len(before) >= 8 {
			text, title = before+" — "+rest, dcnLogTitle("", before)
		} else {
			title = dcnLogTitle("", dcnFirstSentence(rest))
		}
		if title == "" {
			continue
		}
		out = append(out, decision{
			title: title, status: "open",
			when: mod, line: i + 1, by: rel + ":" + strconv.Itoa(i+1),
			context: "marked [NEEDS DECISION] in " + rel, decision: text,
		})
	}
	return out
}

// dcnInlineStatus finds a "Status: X" line — "> **Status:** [PROPOSED]" as
// much as "Status: accepted" — and reads it; "" when there is none.
func dcnInlineStatus(lines []string) string {
	for _, ln := range lines {
		t := strings.ToLower(dcnPlain(strings.TrimLeft(ln, " >")))
		if strings.HasPrefix(t, "status:") {
			return dcnStatus(strings.TrimSpace(t[len("status:"):]))
		}
	}
	return ""
}

// dcnSectionRecords turns the body of one decision section into records. A
// section written as sub-headings yields one record per sub-heading; otherwise
// each top-level bullet is a record, and the bullets nested under it are what
// follows from it — the nearest thing a prose log has to an ADR's
// consequences. The section's own preamble becomes every record's context.
func dcnSectionRecords(body []string, firstLine, level int, rel, src string, when time.Time, open bool) []decision {
	type block struct {
		line   int
		title  string // set when the block came from a sub-heading
		lead   string // the record's opening line, markdown intact
		prose  []string
		conseq []string
		row    bool   // a table row: lead is its title cell, prose the whole row
		marks  string // a row's marker cells — `[RULED]`, `[OPEN]` — which say its state
	}
	var blocks []block
	var preamble []string
	fence := false
	var table []string // the header of the table being read, nil outside one
	titleCol := 0

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
		// A table is a set of decisions written as rows: the header names the
		// columns, the separator under it is not a row, and each row after is
		// one — titled by its Decision (or Title, What, Question) column.
		if strings.HasPrefix(t, "|") {
			// A table under a decision's own sub-heading, or inside its
			// bullet, is part of that decision, not a set of its own.
			if len(blocks) > 0 && !blocks[len(blocks)-1].row {
				into = intoNothing
				continue
			}
			cells := dcnCells(t)
			switch {
			case table == nil:
				table, titleCol = cells, dcnTitleColumn(cells)
			case dcnSeparatorRow(cells):
			default:
				lead, marks := dcnRowTitle(cells, titleCol)
				blocks = append(blocks, block{
					line: firstLine + n, lead: lead, marks: marks, row: true,
					prose: []string{dcnPlain(strings.Join(cells, " · "))},
				})
			}
			into = intoNothing
			continue
		}
		table = nil
		if h := dcnHeading.FindStringSubmatch(ln); h != nil && len(h[1]) > level {
			blocks = append(blocks, block{line: firstLine + n, title: h[2]})
			into = intoProse
			continue
		}
		if b := dcnBullet.FindStringSubmatch(ln); b != nil {
			// A section written as sub-headings has one decision per
			// sub-heading; the bullets under one are what follows from it.
			underHeading := len(blocks) > 0 && blocks[len(blocks)-1].title != ""
			if len(b[1]) == 0 && !underHeading {
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
		id, title, status := "", "", ""
		raw := b.lead
		switch {
		case b.title != "":
			id, title = dcnCleanTitle(b.title)
			status = dcnMarkerStatus(b.title)
			raw = b.title
		case b.row:
			_, cell := dcnCleanTitle(b.lead)
			title = dcnLogTitle("", cell)
		default:
			title = dcnStripMarker(dcnLogTitle(b.lead, prose))
		}
		if title == "" {
			continue // a bullet with nothing in it is not a decision
		}
		if id == "" {
			id = strconv.Itoa(len(out) + 1)
		}
		struck := strings.HasPrefix(strings.TrimLeft(raw, " *"), "~~")
		if status == "" {
			status = dcnLogStatus(prose)
			if struck {
				status = "superseded"
			}
		}
		// Under an open heading an item is open until it says otherwise: a
		// pending table keeps its settled rows, struck through and marked
		// CLOSED, and those are decided — or withdrawn, if that is what
		// closed them.
		if open && status == "decided" {
			switch low := strings.ToLower(prose); {
			case strings.Contains(low, "withdrawn"):
				status = "superseded"
			case struck || dcnSettled.MatchString(title+" "+b.marks):
			default:
				status = "open"
			}
		} else if open && struck && status == "superseded" && !strings.Contains(strings.ToLower(prose), "withdrawn") {
			status = "decided"
		}
		out = append(out, decision{
			id:           id,
			title:        title,
			status:       status,
			when:         when,
			line:         b.line,
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

// dcnLogStatus reads a prose record's state. A written-down decision is an
// agreed one, so the default is decided and only an explicit marker moves it:
// dcnStatus's keyword sweep cannot be used here, because a record that says it
// *supersedes* an earlier call would be read as superseded itself.
func dcnLogStatus(prose string) string {
	low := strings.ToLower(prose)
	switch {
	case strings.HasPrefix(strings.TrimSpace(prose), "~~"),
		strings.Contains(low, "superseded by"), strings.Contains(low, "replaced by"),
		strings.Contains(low, "no longer holds"):
		return "superseded"
	case strings.Contains(low, "(proposed"), strings.HasPrefix(low, "proposed:"),
		strings.Contains(low, "to be decided"), strings.Contains(low, "not yet agreed"),
		strings.Contains(low, "still open"), dcnNeedsDecision.MatchString(prose):
		return "open"
	}
	if s := dcnInlineStatus([]string{prose}); s != "" {
		return s
	}
	if s := dcnMarkerStatus(prose); s == "superseded" {
		return s
	}
	return "decided"
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

// dcnSettled is the word a row or bullet under an open heading says it has
// been settled with.
var dcnSettled = regexp.MustCompile(`\b(CLOSED|RULED|RATIFIED|DECIDED|RESOLVED)\b`)

// dcnCells splits a markdown table row into its trimmed cells.
func dcnCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	parts := strings.Split(row, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// dcnSeparatorRow is the |---|:--:| line under a table's header.
func dcnSeparatorRow(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return len(cells) > 0
}

// dcnTitleColumn picks the column a table's rows are titled by: the one its
// header calls the decision (or title, question, what), else the first that
// is not a number or an id.
func dcnTitleColumn(header []string) int {
	for i, h := range header {
		switch strings.ToLower(dcnPlain(h)) {
		case "decision", "title", "question", "what", "ruling", "item", "topic":
			return i
		}
	}
	for i, h := range header {
		switch strings.ToLower(dcnPlain(h)) {
		case "#", "id", "no", "no.", "n", "":
			continue
		}
		return i
	}
	return 0
}

// dcnMarkCell is a table cell that holds only a status marker: `[RULED]`,
// **OPEN**, ✓.
var dcnMarkCell = regexp.MustCompile(`^\W*[A-Z][A-Z ]{1,18}\W*$|^\W{1,3}$`)

// dcnRowTitle is a row's title cell and its marker cells. A row whose chosen
// column holds only a marker ("| `[RULED]` | Where past injuries are written
// … |") is titled by the first cell after it that says something.
func dcnRowTitle(cells []string, col int) (title, marks string) {
	var ms []string
	for _, c := range cells {
		if dcnMarkCell.MatchString(dcnPlain(c)) {
			ms = append(ms, c)
		}
	}
	marks = strings.Join(ms, " ")
	for i := col; i < len(cells); i++ {
		if p := dcnPlain(cells[i]); p != "" && !dcnMarkCell.MatchString(p) {
			return cells[i], marks
		}
	}
	if col < len(cells) {
		return cells[col], marks
	}
	return "", marks
}

// dcnFirstSentence is s up to its first full stop, where that leaves enough
// to read: a question marked open is titled by the question, not by the
// sentence after it that happens to fit beside it.
func dcnFirstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 12 {
		return s[:i]
	}
	return s
}
