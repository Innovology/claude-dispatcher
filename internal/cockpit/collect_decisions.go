package cockpit

// collect_decisions.go fills the DECISIONS lens: s.decisions (records per repo),
// s.decisionRepoOrder (repos that have ADRs, in discovery order) and s.plugins
// (which decision tool renders each repo). It scans each repo for architecture
// decision records under the conventional folders and parses the markdown into
// the view's decision struct. Everything is best-effort and guarded; a repo
// with no ADRs simply does not appear.

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

// collectDecisions scans every repo for ADR markdown and builds the decisions
// map, the repo order and the plugin list.
func collectDecisions(ctx *collectCtx, s *snapshot) {
	decs := map[string][]decision{}
	var order []string
	var adrRepos []string // repos backed by adr-tools (have an ADR dir)
	var otherRepos []string

	for _, r := range ctx.repos {
		dir, files := dcnFindAdrs(r.Path)
		if dir == "" || len(files) == 0 {
			otherRepos = append(otherRepos, r.Name)
			continue
		}
		var list []decision
		for _, f := range files {
			if d, ok := dcnParseFile(f); ok {
				list = append(list, d)
			}
		}
		if len(list) == 0 {
			otherRepos = append(otherRepos, r.Name)
			continue
		}
		decs[r.Name] = list
		order = append(order, r.Name)
		adrRepos = append(adrRepos, r.Name)
	}

	// No repo has ADRs: honest empty state, a single builtin plugin.
	if len(order) == 0 {
		s.decisions = map[string][]decision{}
		s.decisionRepoOrder = []string{}
		s.plugins = []plugin{dcnBuiltin(otherRepos)}
		return
	}

	s.decisions = decs
	s.decisionRepoOrder = order
	s.plugins = []plugin{
		{
			id: "adr-tools", name: "adr-tools", host: "github.com/npryce/adr-tools",
			kind: "numbered markdown records", repos: adrRepos,
			note: "doc/adr/NNNN-*.md · the cockpit reads the folder and writes new records as proposed",
		},
		dcnBuiltin(otherRepos),
	}
}

// dcnBuiltin is the fallback plugin used for repos that keep no ADR tool of
// their own; their decisions live in the cockpit's own state.
func dcnBuiltin(repos []string) plugin {
	if repos == nil {
		repos = []string{}
	}
	return plugin{
		id: "builtin", name: "cockpit records", host: "local state", kind: "fallback",
		repos: repos,
		note:  "used where a repo has no decision tool of its own · kept in the state dir, exportable as markdown",
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

// dcnParseFile reads and parses one ADR markdown file into a decision. The
// bool is false only when the file cannot be read.
func dcnParseFile(path string) (decision, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return decision{}, false
	}
	text := string(raw)

	d := decision{status: "accepted"}

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
