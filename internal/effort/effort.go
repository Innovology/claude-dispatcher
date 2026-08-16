// Package effort answers one question about a diff: how long would a senior
// developer have taken to write this by hand?
//
// It is the only number in this repo that is a MODEL rather than a
// measurement, and it is labelled that way everywhere it is shown. Everything
// else the cockpit prints quotes something the user's own tooling produced; an
// estimate cannot, so the discipline here is different — state the model, keep
// its constants in one place, and never let a caller print the figure without
// the words that say what it is.
//
// What it is FOR: the cockpit's other figures count dispatchers, commits and
// deploys, none of which say how much work happened. A week of five one-line
// fixes and a week of a rewritten payments module both read as "5 features
// live". This converts a diff into the one unit a human already has intuition
// for — developer hours — so those two weeks stop looking the same.
//
// What it is NOT: a measure of value, of difficulty, or of what the dispatcher
// actually spent. Lines are a proxy for size, and size is a proxy for time. A
// four-line fix that took a day of reading is estimated at minutes, and it
// should be: the estimate answers "how big is this change", asked in hours.
//
// # The model
//
//	minutes(file) = orient(kind) + weight(kind) × 60 × chargeable / LinesPerHour
//	chargeable    = plus + PureDeleteWeight × max(0, minus−plus)
//
// Three deliberate choices, each fixing a way the naive `plus/rate` gets it
// wrong:
//
//   - Churn is not double-charged. `git diff --numstat` reports a MODIFIED
//     line as one added and one deleted, so a file that changed 200 lines in
//     place reads as +200 −200. Only the excess deletion — minus beyond plus —
//     is a removal in its own right, and only that excess is charged for.
//
//   - Deleting is cheaper than writing, but is not free. Removing a dead
//     module still means proving it is dead. PureDeleteWeight prices that at a
//     fraction of writing the same lines.
//
//   - Not every line was typed by a person. A regenerated lockfile is forty
//     thousand lines that cost a human one command; a fixture is bulk with
//     little thought in it; a test is real work but more repetitive than the
//     code it covers. Kinds carry a per-line weight and a per-file orientation
//     cost, and generated files carry a per-line weight of zero — the only
//     honest price for lines nobody wrote.
//
// # The limits, named
//
// The classifier reads paths, not contents: it cannot tell hand-written JSON
// from emitted JSON, or a genuinely hard fifty lines from an easy five
// hundred. A repo whose generated output is not in the known shapes below will
// be over-estimated, which is why Estimate carries Files and Lines — a caller
// can show what the figure was computed FROM, and a reader who thinks it is
// wrong can see why.
package effort

import (
	"path"
	"strings"
	"time"
)

// LinesPerHour is the rate the whole model hangs off: finished, working,
// self-reviewed lines a senior developer produces in an hour of writing code —
// thinking, typing, running it, and fixing what did not work.
//
// It is NOT the 10–50 lines/day figure from whole-project accounting (COCOMO
// and its descendants), which divides a shipped system by its entire calendar
// and staff, and so prices meetings, specifications, hiring and holidays into
// every line. That number answers "what does a line of code cost an
// organisation"; this one answers the question actually being asked here —
// "how long would someone have sat there writing it".
//
// One number, one place. Everything downstream scales linearly with it, so a
// reader who thinks the estimates run high can find the assumption in one line
// rather than inferring it from the outputs.
const LinesPerHour = 50.0

// PureDeleteWeight prices a removed line — one that is not paired with a
// written line in the same file — against writing one.
const PureDeleteWeight = 0.15

// File is one entry of a `git diff --numstat`: the path, and the lines added
// and deleted in it. Binary files, which numstat reports as "-", arrive as
// zeroes and so cost only their orientation.
type File struct {
	Path        string
	Plus, Minus int
}

// Estimate is a hand-coding time and the diff it was computed from. The inputs
// ride along because the figure is a model: a caller can say what it was
// derived from, and nothing has to re-walk the diff to find out.
type Estimate struct {
	// Dur is the estimated time at the keyboard.
	Dur time.Duration
	// Files is how many files the diff touched, Lines the chargeable lines
	// after churn pairing — NOT plus+minus, which would count a modified line
	// twice. Both are counts of the raw diff, unweighted by kind.
	Files, Lines int
}

// kind is a class of file, priced by how much of a person's time its lines
// really cost.
//
// The weights are ratios against writing source code, not independent
// measurements — the claim each makes is "a line here is about this much of a
// line of production code", which is a claim a reader can argue with, which is
// the point of writing them down.
type kind struct {
	name string
	// perLine scales the writing rate. Zero means the lines were emitted by a
	// tool, so no amount of them costs a person anything.
	perLine float64
	// orient is the fixed cost, in minutes, of touching this file at all:
	// finding it, reading enough of it to change it safely, and wiring up
	// whatever the change needs. It is charged once however small the edit,
	// because opening a file is not free, and a change spread over twenty files
	// is more work than the same lines in one.
	orient float64
}

var (
	kSource    = kind{"source", 1.00, 6}
	kTest      = kind{"test", 0.75, 4}
	kConfig    = kind{"config", 0.50, 4}
	kDocs      = kind{"docs", 0.35, 3}
	kData      = kind{"data", 0.15, 2}
	kGenerated = kind{"generated", 0.00, 2}
)

// Of estimates one diff.
//
// An empty diff is a zero estimate, not an absent one: "this branch changed
// nothing" is a real answer, and it is the caller's job to distinguish it from
// "the diff could not be read" — which is why the readable/unreadable
// distinction lives at the git call and never in here.
func Of(files []File) Estimate {
	var est Estimate
	var mins float64
	for _, f := range files {
		k := classify(f.Path)
		chargeable := chargeableLines(f.Plus, f.Minus)
		est.Files++
		est.Lines += chargeable
		mins += k.orient + k.perLine*60*float64(chargeable)/LinesPerHour
	}
	est.Dur = time.Duration(mins * float64(time.Minute))
	return est
}

// Add accumulates another estimate. Summing durations is exact — the model is
// linear in files and lines — so a total over many dispatches is the same
// figure as estimating their combined diff.
func (e Estimate) Add(o Estimate) Estimate {
	return Estimate{Dur: e.Dur + o.Dur, Files: e.Files + o.Files, Lines: e.Lines + o.Lines}
}

// chargeableLines applies the churn pairing: every deleted line up to the
// number added is the other half of a modification the addition already paid
// for, and only the surplus is a deletion in its own right.
func chargeableLines(plus, minus int) int {
	if plus < 0 {
		plus = 0
	}
	if minus < 0 {
		minus = 0
	}
	pure := minus - plus
	if pure < 0 {
		pure = 0
	}
	return plus + int(float64(pure)*PureDeleteWeight+0.5)
}

// Human formats an estimate for a status line: minutes below the hour, hours
// and minutes below ten hours, whole hours above it.
//
// Hours all the way up, never days. "3d" would have to mean a working day, and
// a working day is a second assumption — six focused hours? eight? — stacked
// on top of the first one. Hours are the unit the model is actually in, and a
// reader converts them to whatever their own week looks like.
func Human(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	mins := int(d.Minutes() + 0.5)
	if mins < 60 {
		if mins == 0 {
			// Rounded away, but something was there. A sub-minute change is
			// still a change, and "0m" reads as nothing happened.
			return "<1m"
		}
		return itoa(mins) + "m"
	}
	h, m := mins/60, mins%60
	if h < 10 {
		return itoa(h) + "h " + pad2(m) + "m"
	}
	return itoa(h) + "h"
}

// classify names what a path is. Order is significance, not preference: a
// generated test fixture is generated first and a fixture second, and the
// cheapest true classification is the right one.
func classify(p string) kind {
	lower := strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	base := path.Base(lower)
	ext := path.Ext(base)

	switch {
	case isGenerated(lower, base):
		return kGenerated
	case isTest(lower, base):
		return kTest
	case isData(lower, ext):
		return kData
	case isDocs(base, ext):
		return kDocs
	case isConfig(base, ext):
		return kConfig
	}
	return kSource
}

// lockfiles are the dependency manifests a package manager writes. They are
// matched by exact name because that is how they are exact: every one of these
// is a file no human edits by hand.
var lockfiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"npm-shrinkwrap.json": true, "bun.lockb": true, "deno.lock": true,
	"cargo.lock": true, "go.sum": true, "poetry.lock": true, "uv.lock": true,
	"pipfile.lock": true, "gemfile.lock": true, "composer.lock": true,
	"flake.lock": true, "gradle.lockfile": true, "packages.lock.json": true,
	"mix.lock": true, "pubspec.lock": true,
}

// generatedSuffixes are the filename endings that name their own producer:
// protoc, the dart build runner, a bundler's minifier, a source map.
var generatedSuffixes = []string{
	".pb.go", ".pb.gw.go", "_pb2.py", "_pb2_grpc.py", ".pb.cc", ".pb.h",
	".g.dart", ".freezed.dart", ".g.cs", ".designer.cs",
	".min.js", ".min.css", ".map", ".snap",
	"_generated.go", "_gen.go", ".generated.ts", ".generated.go",
}

// generatedDirs are the tree positions that mean "not written here": vendored
// copies of other people's code, and build output checked in.
//
// Deliberately not here: "build/", "out/" and "bin/". They are build output in
// several ecosystems and ordinary source directories in several others, and
// zeroing a real source tree understates the work — the one direction this
// figure must not fail in is the one that says a week of work was nothing.
var generatedDirs = []string{
	"/vendor/", "/node_modules/", "/dist/", "/.next/", "/__generated__/",
	"/generated/", "/.terraform/", "/target/debug/", "/target/release/",
}

func isGenerated(lower, base string) bool {
	if lockfiles[base] {
		return true
	}
	for _, s := range generatedSuffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	if strings.HasPrefix(base, "zz_generated") {
		return true
	}
	// A leading slash makes the directory tests work on the first segment too,
	// so "vendor/x/y.go" matches the same rule as "a/vendor/x/y.go".
	slashed := "/" + lower
	for _, d := range generatedDirs {
		if strings.Contains(slashed, d) {
			return true
		}
	}
	return false
}

// testSuffixes are the naming conventions that mark a file as a test across
// the ecosystems this repo's user is likely to have checked out.
var testSuffixes = []string{
	"_test.go", "_test.py", "_test.rb", "_test.exs", "_test.dart", "_test.ts",
	".test.js", ".test.jsx", ".test.ts", ".test.tsx",
	".spec.js", ".spec.jsx", ".spec.ts", ".spec.tsx", ".spec.rb",
	"test.java", "tests.cs", "test.cs", "_spec.rb", "_spec.lua",
}

func isTest(lower, base string) bool {
	for _, s := range testSuffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	if strings.HasPrefix(base, "test_") || strings.HasPrefix(base, "conftest.") {
		return true
	}
	slashed := "/" + lower
	for _, d := range []string{"/test/", "/tests/", "/spec/", "/__tests__/"} {
		if strings.Contains(slashed, d) {
			return true
		}
	}
	return false
}

// dataExts are bulk formats: serialised records, tables and vector art. Their
// lines are typed in the sense that somebody produced them, but they are not
// reasoned about a line at a time.
var dataExts = map[string]bool{
	".json": true, ".csv": true, ".tsv": true, ".ndjson": true, ".jsonl": true,
	".svg": true, ".golden": true, ".fixture": true, ".har": true, ".parquet": true,
}

func isData(lower, ext string) bool {
	if dataExts[ext] {
		return true
	}
	slashed := "/" + lower
	for _, d := range []string{"/testdata/", "/fixtures/", "/__snapshots__/", "/__fixtures__/"} {
		if strings.Contains(slashed, d) {
			return true
		}
	}
	return false
}

var docExts = map[string]bool{
	".md": true, ".mdx": true, ".markdown": true, ".txt": true,
	".rst": true, ".adoc": true, ".asciidoc": true, ".org": true,
}

// docNames are the extensionless files at a repo root that are plainly prose.
var docNames = map[string]bool{
	"license": true, "licence": true, "readme": true, "changelog": true,
	"authors": true, "contributors": true, "notice": true, "copying": true,
	"code_of_conduct": true, "contributing": true,
}

func isDocs(base, ext string) bool {
	if docExts[ext] {
		return true
	}
	return ext == "" && docNames[base]
}

var configExts = map[string]bool{
	".yml": true, ".yaml": true, ".toml": true, ".ini": true, ".cfg": true,
	".conf": true, ".properties": true, ".env": true, ".editorconfig": true,
	".plist": true, ".xml": true,
}

// configNames are the extensionless build and tooling files. Makefile sits
// here rather than under source: it is real work, but it is declaration far
// more than it is logic.
var configNames = map[string]bool{
	"dockerfile": true, "makefile": true, "justfile": true, "procfile": true,
	"vagrantfile": true, "caddyfile": true, "brewfile": true, "gemfile": true,
	"rakefile": true, ".gitignore": true, ".dockerignore": true,
	".gitattributes": true, ".npmrc": true, ".nvmrc": true,
}

func isConfig(base, ext string) bool {
	if configExts[ext] {
		return true
	}
	if configNames[base] {
		return true
	}
	// Dotfiles with no extension of their own — ".golangci.yml" is caught by
	// its extension above, ".prettierrc" and friends are caught here.
	return strings.HasPrefix(base, ".") && strings.HasSuffix(base, "rc")
}

// itoa and pad2 keep this package free of fmt for two format verbs.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}
