package fleetcmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"claude-dispatcher/internal/config"
	"claude-dispatcher/internal/repos"
	"claude-dispatcher/internal/state"
	"claude-dispatcher/internal/steward"
)

// runRuling is `ruling add|end|list`: the human's standing decisions for a
// product (steward.Ruling).
//
// `ruling add` is the one verb a steward has to ask permission for (its
// settings put it under "ask"): a ruling is the human's, so the human confirms
// each one the steward records, and the steward cannot widen its own hands.
// Ending one and reading them are free — narrowing authority is always safe.
func runRuling(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ruling add|end|list …")
	}
	switch args[0] {
	case "add":
		return rulingAdd(args[1:], out)
	case "end":
		if len(args) != 2 {
			return errors.New("usage: ruling end <id>")
		}
		product, r, err := steward.EndRuling(args[1])
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "ended %s's ruling %s · %s\n", product, r.ID, r.Text)
		return nil
	case "list":
		return rulingList(args[1:], out)
	}
	return fmt.Errorf("unknown argument %q (add | end | list)", args[0])
}

func rulingAdd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ruling add", flag.ContinueOnError)
	product := fs.String("product", "", "the product it is for (required)")
	allow := fs.String("allow", "", "what it lets the steward do itself: "+strings.Join(steward.Allows, ", "))
	repo := fs.String("repo", "", "narrow it to one repo of the product")
	until := fs.String("until", "", "when it ends: 6h, 3d, 2026-11-16T09:00 (default 24h)")
	// Flags may come after the text, as merge's may: unordered, the first word
	// of the text ended the flags and every later --allow became part of it.
	if err := fs.Parse(reorder(args)); err != nil {
		return err
	}
	words := fs.Args()
	// A day and a time unquoted are two words, and the flag takes only the
	// first: "--until 2026-11-16 09:00" stood until the END of the 16th — 15
	// hours past what was said, for a ruling that may grant merges — with
	// "09:00" as the first word of its text. The time belongs to the day.
	if dayOnly.MatchString(*until) && len(words) > 0 && clockTime.MatchString(words[0]) {
		*until += " " + words[0]
		words = words[1:]
	}
	if strings.TrimSpace(*product) == "" {
		return errors.New("name the product: --product <name>")
	}
	if cfg, err := loadConfig(); err == nil {
		if err := steward.CheckProduct(cfg, *product); err != nil {
			return err
		}
	}
	t := time.Now()
	end, err := steward.ParseUntil(*until, t)
	if err != nil {
		return err
	}
	r := steward.Ruling{Text: strings.Join(words, " "), Repo: *repo, GivenAt: t, Until: end}
	if *allow != "" {
		r.Allow = strings.Split(*allow, ",")
	}
	r, err = steward.AddRuling(steward.Key(*product), r)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "ruling %s for %s until %s · %s\n", r.ID, steward.Key(*product), r.Until.Local().Format("Mon 2 Jan 15:04"), describe(r))
	return nil
}

func rulingList(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ruling list", flag.ContinueOnError)
	product := fs.String("product", "", "the product (required)")
	all := fs.Bool("all", false, "include rulings that have ended")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*product) == "" {
		return errors.New("name the product: --product <name>")
	}
	t := time.Now()
	rs := steward.ActiveRulings(steward.Key(*product), t)
	if *all {
		rs = steward.Rulings(steward.Key(*product))
	}
	if len(rs) == 0 {
		_, _ = fmt.Fprintln(out, "no rulings stand · only the brief's defaults apply")
		return nil
	}
	for _, r := range rs {
		when := "until " + r.Until.Local().Format("Mon 2 Jan 15:04")
		if !r.Active(t) {
			when = "ended"
		}
		_, _ = fmt.Fprintf(out, "%s  %s  %s\n", r.ID, when, describe(r))
	}
	return nil
}

// describe is a ruling in one line: what it grants and where, then its text.
func describe(r steward.Ruling) string {
	var parts []string
	if len(r.Allow) > 0 {
		grant := "allows " + strings.Join(r.Allow, ", ")
		if r.Repo != "" {
			grant += " in " + r.Repo
		}
		parts = append(parts, grant)
	} else if r.Repo != "" {
		parts = append(parts, "in "+r.Repo)
	}
	parts = append(parts, r.Text)
	return strings.Join(parts, " · ")
}

// The merge's world, as seams for tests.
var (
	discoverRepos = repos.Discover
	ghMerge       = func(dir string, args ...string) (string, error) {
		cmd := exec.Command("gh", args...)
		cmd.Dir = dir
		b, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(b)), err
	}
)

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// dayOnly and clockTime are the two halves of an unquoted "2026-11-16 09:00".
var (
	dayOnly   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	clockTime = regexp.MustCompile(`^\d{1,2}:\d{2}$`)
)

// runMerge is a steward merging a pull request, which it may do only under a
// ruling that allows it there: the code checks the ruling, so the human's
// "merge greens while I'm out" is what grants the hand and its end is what
// takes it back — not the steward's reading of its own brief.
//
// The merge is pinned to the head the steward checked (--match-head-commit),
// because the checks it read are a claim about that commit and nothing else: a
// push landing between the look and the merge must fail the merge, not ride it.
func runMerge(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	head := fs.String("head", "", "the full head SHA whose checks you read (required)")
	method := fs.String("method", "squash", "squash, merge or rebase")
	if err := fs.Parse(reorder(args)); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: merge <repo> <pr> --head <full sha> [--method squash|merge|rebase]")
	}
	repoName := fs.Arg(0)
	pr, err := strconv.Atoi(strings.TrimPrefix(fs.Arg(1), "#"))
	if err != nil || pr <= 0 {
		return fmt.Errorf("%q is not a pull request number", fs.Arg(1))
	}
	if !fullSHA.MatchString(*head) {
		return errors.New("--head must be the full 40-character SHA the checks ran on (gh pr view <n> --json headRefOid)")
	}
	switch *method {
	case "squash", "merge", "rebase":
	default:
		return fmt.Errorf("--method %q: squash, merge or rebase", *method)
	}
	cfg, err := loadConfig()
	if err != nil || cfg == nil {
		cfg = &config.Config{}
	}
	product := steward.Key(cfg.ProductFor(repoName))
	ruling, ok := steward.MergeRuling(product, repoName, time.Now())
	if !ok {
		return fmt.Errorf("no ruling lets the steward merge in %s (%s) — note it for the human, or ask them for one", repoName, product)
	}
	dir := ""
	for _, r := range discoverRepos(cfg) {
		if r.Name == repoName {
			dir = r.Path
		}
	}
	if dir == "" {
		return fmt.Errorf("no repo %q under the configured roots", repoName)
	}
	msg, err := ghMerge(dir, "pr", "merge", strconv.Itoa(pr), "--"+*method, "--match-head-commit", *head)
	if err != nil {
		return fmt.Errorf("gh refused the merge: %s", firstLine(msg, err))
	}
	state.AppendEvent(state.Event{Event: state.EventStewardMerged, Repo: repoName,
		Feature: "#" + strconv.Itoa(pr), Reason: "ruling " + ruling.ID + " · " + ruling.Text})
	_, _ = fmt.Fprintf(out, "merged %s #%d at %s under ruling %s\n", repoName, pr, (*head)[:9], ruling.ID)
	return nil
}

// reorder puts flags ahead of the positional arguments, so `merge app 12
// --head …` parses the way it reads.
func reorder(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

func firstLine(msg string, err error) string {
	if msg == "" {
		return err.Error()
	}
	return strings.SplitN(msg, "\n", 2)[0]
}
