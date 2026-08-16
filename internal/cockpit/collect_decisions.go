package cockpit

// collect_decisions.go fills the DECISIONS lens: s.decisions (records per repo),
// s.decisionRepoOrder (repos that have records, in discovery order) and
// s.plugins (which decision source renders each repo).
//
// There are two sources, and a repo may have both. An adr-tools folder is the
// formal one, parsed here. The other is a decision section written as prose in
// the repo's own markdown — collect_decision_log.go — which is where these
// repos actually keep their decisions. Everything is best-effort and guarded; a
// repo with neither simply does not appear.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dcnAdrDirs are the conventional locations an ADR set lives in, relative to a
// repo root. The first that exists and holds markdown wins for that repo.
var dcnAdrDirs = []string{"doc/adr", "docs/adr", "docs/decisions", "adr"}

// collectDecisions scans every repo for both kinds of decision record and
// builds the decisions map, the repo order and the plugin list.
func collectDecisions(ctx *collectCtx, s *snapshot) {
	decs := map[string][]decision{}
	order := []string{}
	var adrRepos []string // repos backed by adr-tools (an ADR dir with records)
	var logRepos []string // repos whose decisions are written as prose
	var otherRepos []string

	for _, r := range ctx.repos {
		var list []decision
		_, files := dcnFindAdrs(r.Path)
		for _, f := range files {
			if d, ok := dcnParseFile(r.Path, f); ok {
				list = append(list, d)
			}
		}
		if len(list) > 0 {
			adrRepos = append(adrRepos, r.Name)
		}
		if log := dcnScanLogs(r.Path); len(log) > 0 {
			logRepos = append(logRepos, r.Name)
			list = append(list, log...)
		}
		if len(list) == 0 {
			otherRepos = append(otherRepos, r.Name)
			continue
		}
		decs[r.Name] = list
		order = append(order, r.Name)
	}

	s.decisions = decs
	s.decisionRepoOrder = order

	// Only sources that found something are listed. A plugin wired to no repo
	// is a claim the pane cannot back up, and the left column would show it as
	// "off" beside the repos it does not explain.
	s.plugins = nil
	if len(adrRepos) > 0 {
		s.plugins = append(s.plugins, plugin{
			id: "adr-tools", name: "adr-tools", host: "github.com/npryce/adr-tools",
			kind: "numbered markdown records", repos: adrRepos,
			note: "doc/adr/NNNN-*.md · the cockpit reads the folder, it does not write records",
		})
	}
	if len(logRepos) > 0 {
		s.plugins = append(s.plugins, plugin{
			id: "decision-log", name: "decision log", host: "the repo's own markdown",
			kind: "prose decision sections", repos: logRepos,
			note: "a heading that says decisions in CLAUDE.md, DECISIONS.md or ARCHITECTURE.md · " +
				"each bullet under it is a record, read where it was written",
		})
	}
	s.plugins = append(s.plugins, dcnBuiltin(otherRepos))
}

// dcnBuiltin is the fallback listed against repos where neither source found
// anything. Its note used to promise records "kept in the state dir,
// exportable as markdown"; nothing in the cockpit has ever written one, so it
// now says what is actually true — there is nothing recorded to read.
func dcnBuiltin(repos []string) plugin {
	if repos == nil {
		repos = []string{}
	}
	return plugin{
		id: "builtin", name: "nothing recorded", host: "no source found", kind: "fallback",
		repos: repos,
		note: "no doc/adr folder and no decisions heading in CLAUDE.md, DECISIONS.md or " +
			"ARCHITECTURE.md · write one and it appears here on the next load",
	}
}

// dcnFindAdrs returns the first ADR directory that exists under repoPath along
// with its markdown files, or ("", nil) when none is found.
func dcnFindAdrs(repoPath string) (string, []string) {
	for _, rel := range dcnAdrDirs {
		dir := filepath.Join(repoPath, filepath.FromSlash(rel))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var files []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
		if len(files) > 0 {
			return dir, files
		}
	}
	return "", nil
}

var dcnLeadNum = regexp.MustCompile(`^(\d+)`)

// dcnParseFile reads and parses one ADR markdown file into a decision, with
// repoPath naming the root the record's provenance is written relative to. The
// bool is false only when the file cannot be read.
func dcnParseFile(repoPath, path string) (decision, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return decision{}, false
	}
	text := string(raw)

	// by: where the record lives. The body pane has always had a line for this
	// and the collector has never filled it, so it rendered blank.
	d := decision{status: "accepted", by: path}
	if rel, err := filepath.Rel(repoPath, path); err == nil {
		d.by = filepath.ToSlash(rel)
	}

	// id: the leading number of the file name (preserving any zero-padding),
	// else the base name without extension.
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if m := dcnLeadNum.FindString(base); m != "" {
		d.id = m
	} else {
		d.id = base
	}

	// title: the first "# " heading, else the file base.
	d.title = base
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "# ") {
			d.title = strings.TrimSpace(strings.TrimPrefix(t, "# "))
			break
		}
	}

	// sections: split on "## " headings.
	sections := dcnSections(text)
	if st, ok := sections["status"]; ok {
		d.status = dcnStatus(st)
	} else if inline := dcnInlineStatus(text); inline != "" {
		d.status = inline
	}
	d.context = sections["context"]
	d.decision = sections["decision"]
	d.consequences = sections["consequences"]

	// at: file modification time as a short relative age.
	if info, err := os.Stat(path); err == nil {
		d.at = dcnAge(info.ModTime())
	}

	return d, true
}

// dcnSections splits an ADR body into its "## Heading" sections, keyed by the
// lower-cased heading, with the section prose collapsed to a single line.
func dcnSections(text string) map[string]string {
	out := map[string]string{}
	cur := ""
	var buf []string
	flush := func() {
		if cur != "" {
			out[cur] = strings.TrimSpace(strings.Join(buf, " "))
		}
		buf = nil
	}
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "## ") {
			flush()
			cur = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "## ")))
			continue
		}
		if cur != "" && t != "" {
			buf = append(buf, t)
		}
	}
	flush()
	return out
}

// dcnInlineStatus finds a "Status: X" line anywhere in the body.
func dcnInlineStatus(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		low := strings.ToLower(t)
		if strings.HasPrefix(low, "status:") {
			return dcnStatus(strings.TrimSpace(t[len("status:"):]))
		}
	}
	return ""
}

// dcnStatus normalises a status blob to one of the lens's lifecycle states,
// defaulting to accepted.
func dcnStatus(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "supersed"), strings.Contains(low, "deprecat"),
		strings.Contains(low, "reject"):
		return "superseded"
	case strings.Contains(low, "propos"), strings.Contains(low, "draft"):
		return "proposed"
	case strings.Contains(low, "accept"):
		return "accepted"
	default:
		return "accepted"
	}
}

// dcnAge renders a timestamp as a short relative age like "4m", "2h", "3d ago".
func dcnAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h ago"
	default:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d ago"
	}
}
