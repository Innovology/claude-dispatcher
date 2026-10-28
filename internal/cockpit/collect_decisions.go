package cockpit

// collect_decisions.go fills the DECISIONS lens: s.decisions (records per repo,
// grouped), s.decisionRepoOrder (repos that have records, by product) and the
// count of repos that were read and had none.
//
// Decisions are found where they were written, which is anywhere. The lens
// shipped reading a fixed list of places — an adr-tools folder, and a heading
// in CLAUDE.md, DECISIONS.md, ARCHITECTURE.md or README.md at the root — and
// on the machine it was reported from that found one repo, its own, while the
// repos the human works in daily kept hundreds: player-app a numbered register
// (`_docs/spec/stack-decisions.md`, D1–D48) and a record of rulings marked
// RATIFIED, playerpulse its open questions in "Open decisions" sections and
// `[NEEDS DECISION]` markers throughout `docs/`, the design system two rulings
// marked "RATIFIED … Decision #2". None of it was in a place the lens looked.
// So every markdown file in a checkout is read, and what counts as a decision
// is decided by its shape (collect_decision_log.go), not its location.
//
// Reading every file on every load would be a disk scan per poll, so the walk
// is cached per checkout for dcnRewalk and each file's records for as long as
// its size and modification time hold. Nothing here writes.

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// collectDecisions reads every repo's markdown for decisions and builds the
// decisions map and the repo order.
func collectDecisions(ctx *collectCtx, s *snapshot) {
	decs := map[string][]decision{}
	product := map[string]string{}
	var order []string
	none := 0
	for _, r := range ctx.repos {
		var list []decision
		for _, rel := range dcnMarkdown(r.Path) {
			list = append(list, dcnFileRecords(r.Path, rel)...)
		}
		list = dcnDedupe(list)
		if len(list) == 0 {
			none++
			continue
		}
		for i := range list {
			list[i].at = dcnAge(list[i].when)
		}
		decs[r.Name] = dcnGrouped(list)
		order = append(order, r.Name)
		if ctx.cfg != nil {
			product[r.Name] = ctx.cfg.ProductFor(r.Name)
		}
	}
	// By product, as every other lens groups, unassigned last; inside one, the
	// repo with the most still open first — what is waiting on someone is what
	// the screen leads with — then the most written down, then by name.
	openIn := func(r string) int {
		n := 0
		for _, d := range decs[r] {
			if d.status == "open" {
				n++
			}
		}
		return n
	}
	sort.SliceStable(order, func(i, j int) bool {
		pi, pj := product[order[i]], product[order[j]]
		if (pi == "") != (pj == "") {
			return pj == ""
		}
		if pi != pj {
			return pi < pj
		}
		if oi, oj := openIn(order[i]), openIn(order[j]); oi != oj {
			return oi > oj
		}
		if ni, nj := len(decs[order[i]]), len(decs[order[j]]); ni != nj {
			return ni > nj
		}
		return order[i] < order[j]
	})
	s.decisions = decs
	s.decisionRepoOrder = order
	s.decisionProduct = product
	s.decisionNone = none
	// Cut up for `/` here, in the load, so a search never waits on it: on a
	// large portfolio it is the most expensive thing the search does.
	s.decisionIndex = buildDecIndex(order, decs)
}

// ---- finding the markdown -----------------------------------------------------

// dcnRewalk is how long one walk of a checkout stands. A decision written a
// minute ago can wait a minute; a walk of a large repo on every poll cannot.
const dcnRewalk = 2 * time.Minute

// dcnMaxFiles and dcnMaxBytes bound what one checkout can cost: a repo of
// generated markdown is not a decision log, and a 2 MB file is not prose.
const (
	dcnMaxFiles = 3000
	dcnMaxBytes = 2 << 20
)

// dcnSkipDirs are folders that hold other people's markdown or build output.
// Hidden folders are skipped as well (dcnSkip), which takes .git, .claude and
// its agent worktrees, .expo and the like.
var dcnSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"coverage": true, "target": true, "out": true, "Pods": true,
	"DerivedData": true, "bower_components": true, "site-packages": true,
}

var dcnCache = struct {
	sync.Mutex
	walks map[string]dcnWalked
	files map[string]dcnParsed
}{walks: map[string]dcnWalked{}, files: map[string]dcnParsed{}}

type dcnWalked struct {
	at    time.Time
	files []string // relative, slash-separated, in walk order
}

type dcnParsed struct {
	size int64
	mod  time.Time
	recs []decision
}

// dcnMarkdown lists a checkout's markdown files, relative to it, in walk order
// (lexical, so a folder's files stay together).
func dcnMarkdown(root string) []string {
	dcnCache.Lock()
	w, ok := dcnCache.walks[root]
	dcnCache.Unlock()
	if ok && time.Since(w.at) < dcnRewalk {
		return w.files
	}
	var files []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != root && dcnSkip(p, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if len(files) >= dcnMaxFiles {
			return fs.SkipAll
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			if rel, err := filepath.Rel(root, p); err == nil {
				files = append(files, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	dcnCache.Lock()
	dcnCache.walks[root] = dcnWalked{at: time.Now(), files: files}
	dcnCache.Unlock()
	return files
}

// dcnSkip reports whether a folder is left out of the walk: hidden, build
// output or dependencies, or another checkout — a folder with a .git of its
// own is a nested repo or a worktree, whose decisions are its own repo's.
func dcnSkip(p, name string) bool {
	if strings.HasPrefix(name, ".") || dcnSkipDirs[name] {
		return true
	}
	_, err := os.Lstat(filepath.Join(p, ".git"))
	return err == nil
}

// dcnFileRecords is one file's decisions, read again only when the file has
// changed. Records come back without their age: that is taken at each load
// from when, so a cached record does not go on saying "2m ago" for an hour.
func dcnFileRecords(root, rel string) []decision {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	// A regular file only: a .md that is a link to a FIFO or a device stats
	// at size 0 and then blocks the read, or never ends it, and loads run one
	// at a time, so one such file would hold up every load after it.
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() || info.Size() > dcnMaxBytes {
		return nil
	}
	dcnCache.Lock()
	c, ok := dcnCache.files[abs]
	dcnCache.Unlock()
	if ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return c.recs
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	recs := dcnParse(rel, string(raw), info.ModTime())
	group, where := dcnGroup(rel, len(recs) == 1 && recs[0].adr)
	for i := range recs {
		recs[i].file = abs
		recs[i].group, recs[i].where = group, where
	}
	dcnCache.Lock()
	dcnCache.files[abs] = dcnParsed{size: info.Size(), mod: info.ModTime(), recs: recs}
	dcnCache.Unlock()
	return recs
}

// ---- grouping -------------------------------------------------------------------

// dcnGeneric are file names that say nothing about what is in them, so the
// folder names the group instead — and at the root, nothing does.
var dcnGeneric = map[string]bool{
	"readme": true, "index": true, "claude": true, "agents": true,
	"decisions": true, "decision-log": true, "decision_log": true,
	"adr": true, "adrs": true, "architecture": true, "changelog": true,
	"notes": true, "overview": true,
}

// dcnNumbered matches a numbered record's file name — 0001-parking-is…,
// adr-012-… — which names the record, not the set it belongs to.
var dcnNumbered = regexp.MustCompile(`(?i)^(adr[-_]?)?\d+[-_.]`)

// dcnDated is a date in a file name, which says when, not what.
var dcnDated = regexp.MustCompile(`[-_ ]?\d{4}[-_]\d{2}[-_]\d{2}`)

// dcnGroup names the group a file's records sit under, and the folder shown
// beside it. A file named for what it holds is its own group ("stack
// decisions", "payments", "e3 team snapshots" for 03-e3-team-snapshots.md,
// "oracle" for oracle-2026-07-17.md); a generic name, or an ADR — whose
// name is the record's, not the set's — takes its folder's ("adr" for
// docs/adr/0001-….md); a generic file at the root is in no group, and its
// records are the plain list at the top.
func dcnGroup(rel string, adr bool) (group, where string) {
	dir, file := path.Split(rel)
	dir = strings.TrimSuffix(dir, "/")
	base := strings.TrimSuffix(file, path.Ext(file))
	if !dcnGeneric[strings.ToLower(base)] && !adr {
		name := dcnNumbered.ReplaceAllString(base, "")
		if name = strings.Trim(dcnDated.ReplaceAllString(name, ""), "-_. "); name != "" {
			return dcnHuman(name), dir
		}
	}
	if dir == "" {
		return "", ""
	}
	return dcnHuman(path.Base(dir)), path.Dir(dir)
}

// dcnHuman turns a file or folder name into a group label: separators become
// spaces, a leading underscore goes, and it is lower-cased as the cockpit is.
func dcnHuman(s string) string {
	s = strings.TrimLeft(s, "_.")
	s = strings.NewReplacer("-", " ", "_", " ").Replace(s)
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// dcnGrouped orders a repo's records for the list: the ungrouped ones first,
// then each group in the order its first file was walked, each keeping its
// files' order.
func dcnGrouped(list []decision) []decision {
	rank := map[string]int{"": -1}
	for _, d := range list {
		if _, ok := rank[d.group]; !ok {
			rank[d.group] = len(rank)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return rank[list[i].group] < rank[list[j].group] })
	return list
}

// dcnTitleKey is a title reduced to its letters and digits, for spotting one
// decision written down twice.
func dcnTitleKey(t string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(t) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dcnDedupe keeps one copy of a decision recorded in two places — an ADR and
// the CLAUDE.md bullet that summarises it — and keeps the fuller one. Short
// titles are left alone: two sections called "Open" are not one decision.
func dcnDedupe(list []decision) []decision {
	best := map[string]int{}
	for i, d := range list {
		k := dcnTitleKey(d.title)
		if len(k) < 16 {
			continue
		}
		if j, ok := best[k]; !ok || dcnWeight(d) > dcnWeight(list[j]) {
			best[k] = i
		}
	}
	out := list[:0:0]
	for i, d := range list {
		k := dcnTitleKey(d.title)
		if len(k) >= 16 && best[k] != i {
			continue
		}
		out = append(out, d)
	}
	return out
}

func dcnWeight(d decision) int { return len(d.context) + len(d.decision) + len(d.consequences) }

// ---- shared helpers ---------------------------------------------------------------

// dcnStatus reads a status blob as one of the lens's states, defaulting to
// decided: a written-down decision is an agreed one unless it says not. The
// first status word in its opening words decides — "Accepted. Supersedes
// 0003" is accepted, and "ON for the iOS lane … (Superseded by the …)" is a
// feature's state, not the decision's.
func dcnStatus(s string) string {
	low := strings.ToLower(s)
	if r := []rune(low); len(r) > 40 {
		low = string(r[:40])
	}
	best, at := "decided", len(low)
	for word, st := range dcnStatusWords {
		if i := strings.Index(low, word); i >= 0 && i < at {
			best, at = st, i
		}
	}
	return best
}

// dcnStatusWords are the stems a status line names a state with.
var dcnStatusWords = map[string]string{
	"supersed": "superseded", "deprecat": "superseded", "reject": "superseded",
	"withdrawn": "superseded", "propos": "open", "draft": "open", "open": "open",
	"pending": "open", "accept": "decided", "ratif": "decided", "ruled": "decided",
	"decided": "decided",
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
